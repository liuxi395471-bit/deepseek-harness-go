/**
 * 应用状态（组合式 API，无需引入 Pinia）。
 *
 * 职责：
 * - 认证 token 的读取与保存。
 * - 会话列表的加载、切换、删除。
 * - 当前会话的消息列表与流式渲染状态。
 * - 消费后端 SSE 事件，把 agent 的运行过程映射为 UI 状态。
 *
 * 事件到 UI 的映射（对应 internal/server/util.go 的 eventToFrame）：
 *
 *   assistant_delta   -> 追加到"正在流式生成的助手消息"
 *   assistant_message -> 用最终内容替换流式内容，并展开工具调用卡片
 *   tool_call_start   -> 在当前助手消息下插入"工具执行中"占位
 *   tool_result       -> 用结果替换占位
 *   loop_error        -> 记录错误
 *   phase_change      -> 更新状态指示器文案
 *   loop_done         -> 结束流式状态，写入 session_id 与 usage
 */

import { computed, reactive, ref } from 'vue'
import * as api from '@/api/client'
import type { Message, SessionItem, ToolCall } from '@/api/types'

/** 一条工具调用在 UI 上的状态。 */
export interface ToolInvocation {
  callId: string
  name: string
  args: string
  /** 工具执行结果；未完成时为 undefined。 */
  result?: string
  isError?: boolean
  /** 耗时（毫秒）。 */
  tookMs?: number
  /** 是否仍在执行中。 */
  running: boolean
}

/** 会话中的一条消息（UI 层模型）。 */
export interface UiMessage {
  /** 本地唯一 id（用于 v-for key 与 DOM 定位）。 */
  key: string
  role: Message['role']
  content: string
  /** assistant 消息的工具调用列表。 */
  tools: ToolInvocation[]
  /** 是否是正在流式生成中的占位消息。 */
  streaming?: boolean
  /** tool 角色消息对应的工具名。 */
  toolName?: string
}

/** 流式运行的状态机。 */
export type RunPhase =
  | 'idle'
  | 'connecting'
  | 'llm_call'
  | 'tool_exec'
  | 'stopped'
  | 'error'

const PHASE_LABEL: Record<RunPhase, string> = {
  idle: '',
  connecting: '正在连接…',
  llm_call: '正在思考…',
  tool_exec: '正在执行工具…',
  stopped: '已停止',
  error: '出错',
}

// ---- 认证 ----

export const token = ref(api.getToken())
export const authError = ref('')

export function setAuthToken(t: string): void {
  api.setToken(t)
  token.value = api.getToken()
}

export function clearAuthToken(): void {
  api.setToken('')
  token.value = ''
}

// ---- 会话列表 ----

export const sessions = ref<SessionItem[]>([])
export const sessionsLoading = ref(false)
export const sessionsError = ref('')

/** 当前会话 id；空串表示"新对话"（尚未落库）。 */
export const currentSessionId = ref('')

/** 当前会话的消息列表。 */
export const messages = ref<UiMessage[]>([])

/** 流式状态。 */
export const phase = ref<RunPhase>('idle')
export const phaseLabel = computed(() => PHASE_LABEL[phase.value])

/** 本轮运行的 usage（来自 loop_done 帧）。 */
export const lastUsage = ref<{ prompt: number; completion: number; total: number } | null>(null)

/** 全局错误提示。 */
export const appError = ref('')

/** 是否正在流式运行。 */
export const isRunning = computed(() => phase.value !== 'idle' && phase.value !== 'error')

// 内部引用
let abortStream: (() => void) | null = null
let keySeq = 0

/** 生成一个本地消息 key。 */
function nextKey(): string {
  keySeq += 1
  return `m${keySeq}`
}

/** 报告一个错误。 */
function reportError(e: unknown): void {
  appError.value = e instanceof Error ? e.message : String(e)
  phase.value = 'error'
}

/** 加载会话列表。 */
export async function loadSessions(): Promise<void> {
  sessionsLoading.value = true
  sessionsError.value = ''
  try {
    sessions.value = await api.listSessions()
  } catch (e) {
    sessionsError.value = e instanceof Error ? e.message : String(e)
  } finally {
    sessionsLoading.value = false
  }
}

/** 切换到指定会话并加载其消息。 */
export async function selectSession(id: string): Promise<void> {
  if (isRunning.value) {
    appError.value = '当前有正在运行的请求，请先停止再切换会话。'
    return
  }
  currentSessionId.value = id
  messages.value = []
  lastUsage.value = null
  appError.value = ''

  try {
    const detail = await api.getSession(id)
    messages.value = toUiMessages(detail.messages)
    if (detail.usage) {
      lastUsage.value = {
        prompt: detail.usage.prompt_tokens ?? 0,
        completion: detail.usage.completion_tokens ?? 0,
        total: detail.usage.total_tokens ?? 0,
      }
    }
  } catch (e) {
    reportError(e)
  }
}

/** 开始一个新对话（清空当前视图）。 */
export function newConversation(): void {
  if (isRunning.value) {
    appError.value = '当前有正在运行的请求，请先停止再新建会话。'
    return
  }
  currentSessionId.value = ''
  messages.value = []
  lastUsage.value = null
  appError.value = ''
  phase.value = 'idle'
}

/**
 * 删除会话。
 *
 * 后端 v3 尚未提供删除端点，这里仅从列表中移除（磁盘数据仍保留）。
 */
export async function deleteSession(id: string): Promise<void> {
  sessions.value = sessions.value.filter((s) => s.id !== id)
  if (currentSessionId.value === id) {
    newConversation()
  }
}

/**
 * 把后端 Message 序列转为 UI 消息列表。
 *
 * 关键处理：role=tool 的消息是某次工具调用的**结果**，它属于前面那条
 * assistant 消息的工具卡片，不应单独渲染成一条消息（否则历史回放时
 * 会看到重复的内容）。这里把它折叠进对应的 ToolInvocation。
 */
function toUiMessages(list: Message[]): UiMessage[] {
  const out: UiMessage[] = []

  for (const m of list) {
    if (m.role === 'tool') {
      // 找到 tool_call_id 对应的调用，回填结果。
      const target = out.find((ui) =>
        ui.tools.some((t) => t.callId === m.tool_call_id),
      )
      const inv = target?.tools.find((t) => t.callId === m.tool_call_id)
      if (inv) {
        inv.result = m.content ?? ''
        inv.isError = (m.content ?? '').startsWith('[ERROR]')
        inv.running = false
      }
      continue
    }

    if (m.role === 'system') {
      // system 提示不在对话中展示。
      continue
    }

    const tools: ToolInvocation[] = []
    if (m.role === 'assistant' && m.tool_calls?.length) {
      for (const tc of m.tool_calls) {
        tools.push({
          callId: tc.id,
          name: tc.function.name,
          args: tc.function.arguments,
          running: false,
        })
      }
    }

    out.push({
      key: nextKey(),
      role: m.role,
      content: m.content ?? '',
      tools,
      toolName: m.name,
    })
  }

  return out
}

/** 停止当前正在进行的流式请求。 */
export function stopRun(): void {
  if (abortStream) {
    abortStream()
    abortStream = null
  }
  phase.value = 'stopped'
}

/** 关闭错误提示。 */
export function dismissError(): void {
  appError.value = ''
}

/**
 * 发送一条用户消息并流式接收结果。
 *
 * @param text 用户输入
 */
export async function sendMessage(text: string): Promise<void> {
  const prompt = text.trim()
  if (!prompt || isRunning.value) return

  appError.value = ''

  // 立即把用户消息推入视图（乐观更新）。
  messages.value = [
    ...messages.value,
    { key: nextKey(), role: 'user', content: prompt, tools: [] },
  ]

  // 占位的助手消息：流式内容会先写在这里。
  //
  // 必须用 reactive()：Vue 的 ref 数组在**读取**元素时才套上响应式
  // 代理，直接改这个原始对象不会触发任何重新渲染——流式文本会因此
  // 完全不显示。用 reactive() 让后续的 content/tools 变更都能被追踪。
  const assistantMsg = reactive<UiMessage>({
    key: nextKey(),
    role: 'assistant',
    content: '',
    tools: [],
    streaming: true,
  })
  messages.value = [...messages.value, assistantMsg]

  phase.value = 'connecting'
  lastUsage.value = null

  // 新会话在拿到 session_id 之前用空 sid；拿到后后续轮次续用它。
  let sid = currentSessionId.value || undefined

  abortStream = api.openStream(
    prompt,
    sid,
    {
      onEvent(name, data) {
        switch (name) {
          case 'phase_change': {
            const p = (data as { phase: string }).phase
            if (p === 'llm_call') phase.value = 'llm_call'
            else if (p === 'tool_exec') phase.value = 'tool_exec'
            else if (p === 'stopped') phase.value = 'stopped'
            else if (p === 'init') phase.value = 'connecting'
            break
          }

          case 'assistant_delta': {
            const d = data as { text: string }
            assistantMsg.content += d.text
            messages.value = [...messages.value]
            break
          }

          case 'tool_call_start': {
            const call = (data as { call: ToolCall }).call
            // 同一 call_id 只保留一张卡片：服务端在多轮里可能重复
            // 推送同一次调用的开始事件。
            if (!assistantMsg.tools.some((t) => t.callId === call.id)) {
              assistantMsg.tools.push({
                callId: call.id,
                name: call.function.name,
                args: call.function.arguments,
                running: true,
              })
            }
            phase.value = 'tool_exec'
            messages.value = [...messages.value]
            break
          }

          case 'tool_result': {
            const r = data as {
              call_id: string
              name: string
              content: string
              is_error: boolean
              took_ms: number
            }
            // 优先匹配正在运行的调用；找不到则按 call_id 兜底。
            let inv = assistantMsg.tools.find((t) => t.callId === r.call_id)
            if (!inv) {
              inv = {
                callId: r.call_id,
                name: r.name,
                args: '',
                running: true,
              }
              assistantMsg.tools.push(inv)
            }
            inv.result = r.content
            inv.isError = r.is_error
            inv.tookMs = r.took_ms
            inv.running = false
            messages.value = [...messages.value]
            break
          }

          case 'assistant_message': {
            const m = data as { content: string; tool_calls: ToolCall[] }
            // 用最终内容替换流式内容（流式可能被截断）。
            if (m.content) {
              assistantMsg.content = m.content
            }
            // 后端补发的工具调用（流式过程中可能未收到 tool_call_start）。
            for (const tc of m.tool_calls ?? []) {
              if (!assistantMsg.tools.some((t) => t.callId === tc.id)) {
                assistantMsg.tools.push({
                  callId: tc.id,
                  name: tc.function.name,
                  args: tc.function.arguments,
                  running: false,
                })
              }
            }
            messages.value = [...messages.value]
            break
          }

          case 'loop_error': {
            const e = data as { err: string }
            appError.value = e.err
            phase.value = 'error'
            break
          }

          case 'loop_done': {
            const d = data as {
              rounds: number
              stop_reason: string
              session_id: string
              error?: string
              usage?: {
                prompt_tokens: number
                completion_tokens: number
                total_tokens: number
              }
            }
            if (d.session_id) {
              // 新会话在此时才拿到 id：记下来，后续轮次续用。
              sid = d.session_id
              currentSessionId.value = d.session_id
              void loadSessions()
            }
            if (d.usage) {
              lastUsage.value = {
                prompt: d.usage.prompt_tokens,
                completion: d.usage.completion_tokens,
                total: d.usage.total_tokens,
              }
            }
            // 运行失败时把原因呈现出来，而不是静默结束。
            // 注意：loop_error 事件已经设置过 appError，这里不覆盖它
            // （避免用更笼统的 loop_done.error 冲掉更精确的 loop_error.err）。
            if (d.stop_reason === 'error') {
              if (d.error && !appError.value) {
                appError.value = d.error
              }
              phase.value = 'error'
            } else {
              phase.value = 'idle'
            }
            break
          }

          default:
            break
        }
      },

      onError(e) {
        reportError(e)
      },

      onClose() {
        abortStream = null
        assistantMsg.streaming = false
        messages.value = [...messages.value]
        if (phase.value !== 'error') {
          phase.value = 'idle'
        }
      },
    },
  )
}
