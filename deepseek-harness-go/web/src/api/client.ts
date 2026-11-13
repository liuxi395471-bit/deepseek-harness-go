/**
 * 后端 API 客户端。
 *
 * 认证：除 /healthz 外，所有端点都要求 `Authorization: Bearer <token>`。
 * token 由用户在登录框输入，保存在 localStorage。
 *
 * SSE 说明：浏览器原生 EventSource 不支持自定义请求头，无法携带
 * Bearer token，因此这里用 fetch + ReadableStream 手动解析 SSE 帧。
 */

import type {
  MessageResponse,
  SessionDetail,
  SessionItem,
  SseEventName,
  SseEvents,
} from './types'

const TOKEN_KEY = 'dsh.auth.token'

/** 读取本地保存的 token。 */
export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

/** 保存 token；传空串表示登出。 */
export function setToken(token: string): void {
  try {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  } catch {
    /* localStorage 不可用（隐私模式）：token 仅存在于内存，刷新后需重填。 */
  }
}

/** 从后端的纯文本错误响应中提取可读信息。 */
async function readError(res: Response): Promise<string> {
  const text = await res.text().catch(() => '')
  if (!text) return `HTTP ${res.status}`
  // 后端用 http.Error 返回纯文本；去掉尾部换行。
  return text.trim() || `HTTP ${res.status}`
}

/**
 * ApiError 携带 HTTP 状态码的服务端错误。
 *
 * 后端的错误响应体是纯文本（如 "unauthorized"），**不含**状态码，
 * 因此不能靠 message 判断 401。必须显式保留 status，调用方
 * （如登录页区分"令牌无效"与其他故障）才能正确分支。
 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/** 统一的 fetch 包装：注入鉴权头、解析 JSON、归一化错误。 */
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken()
  const headers = new Headers(init.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  let res: Response
  try {
    res = await fetch(path, { ...init, headers })
  } catch (e) {
    throw new Error(`网络请求失败：${e instanceof Error ? e.message : String(e)}`)
  }

  if (!res.ok) {
    throw new ApiError(res.status, await readError(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

/** 健康检查，无需认证。 */
export async function health(): Promise<boolean> {
  try {
    const res = await fetch('/healthz')
    return res.ok
  } catch {
    return false
  }
}

/** 列出会话元数据。 */
export function listSessions(): Promise<SessionItem[]> {
  return request<SessionItem[]>('/api/sessions')
}

/**
 * 用指定 token 验证它是否被后端接受，**不**写入 localStorage。
 *
 * 登录页用它做"先验证、后提交"：如果在验证通过之前就调用
 * setAuthToken()，全局的 token ref 会立刻变化，App.vue 的 loggedIn
 * 计算属性随之为真，整个主界面（侧边栏 + 输入框）会先闪一下再退回
 * 登录页。先验证再写入可以避免这个闪烁。
 */
export async function verifyToken(token: string): Promise<void> {
  const headers = new Headers({ Authorization: `Bearer ${token}` })
  let res: Response
  try {
    res = await fetch('/api/sessions', { headers })
  } catch (e) {
    throw new ApiError(0, `网络请求失败：${e instanceof Error ? e.message : String(e)}`)
  }
  if (!res.ok) {
    throw new ApiError(res.status, await readError(res))
  }
  // 消费掉响应体，避免连接悬挂。
  await res.text().catch(() => '')
}

/** 获取完整会话记录（含全部消息）。 */
export function getSession(id: string): Promise<SessionDetail> {
  return request<SessionDetail>(`/api/sessions/${encodeURIComponent(id)}`)
}

/** 阻塞式执行一次 prompt，返回最终结果。 */
export function postMessage(prompt: string, sessionId?: string): Promise<MessageResponse> {
  return request<MessageResponse>('/api/agent/message', {
    method: 'POST',
    body: JSON.stringify({ prompt, session_id: sessionId }),
  })
}

/**
 * SSE 事件回调。
 *
 * onEvent 收到每个事件的负载；onError 在连接或解析出错时调用；
 * onClose 在流正常结束或被取消时调用。
 */
export interface StreamHandlers {
  onEvent: (name: SseEventName, data: SseEvents[SseEventName]) => void
  onError: (err: Error) => void
  onClose: () => void
}

/**
 * 打开一条 SSE 流（GET /api/agent/stream）。
 *
 * 返回一个 abort 函数，用于中断正在进行的流式请求。
 */
export function openStream(
  prompt: string,
  sessionId: string | undefined,
  handlers: StreamHandlers,
): () => void {
  const controller = new AbortController()

  const params = new URLSearchParams({ prompt })
  if (sessionId) params.set('session_id', sessionId)

  const run = async () => {
    const token = getToken()
    const headers = new Headers({ Accept: 'text/event-stream' })
    if (token) headers.set('Authorization', `Bearer ${token}`)

    try {
      const res = await fetch(`/api/agent/stream?${params.toString()}`, {
        headers,
        signal: controller.signal,
      })

      if (!res.ok) {
        throw new ApiError(res.status, await readError(res))
      }
      if (!res.body) {
        throw new Error('浏览器不支持流式响应')
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      // SSE 帧以空行分隔；一帧可能是多行 data:。
      const dispatch = (raw: string) => {
        let name = 'message'
        const dataLines: string[] = []
        for (const line of raw.split('\n')) {
          if (line.startsWith('event:')) {
            name = line.slice(6).trim()
          } else if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).replace(/^ /, ''))
          }
        }
        if (dataLines.length === 0) return
        const payload = dataLines.join('\n')
        let parsed: unknown
        try {
          parsed = JSON.parse(payload)
        } catch {
          // 非 JSON 的负载：按原始字符串回填，避免整条流中断。
          parsed = payload
        }
        handlers.onEvent(name as SseEventName, parsed as SseEvents[SseEventName])
      }

      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        let idx: number
        while ((idx = buffer.indexOf('\n\n')) !== -1) {
          const frame = buffer.slice(0, idx)
          buffer = buffer.slice(idx + 2)
          if (frame.trim()) dispatch(frame)
        }
      }

      // 流结束时可能还残留半帧。
      if (buffer.trim()) dispatch(buffer)
    } catch (e) {
      // 用户主动中断不算错误。
      if (controller.signal.aborted) {
        handlers.onClose()
        return
      }
      handlers.onError(e instanceof Error ? e : new Error(String(e)))
    } finally {
      handlers.onClose()
    }
  }

  void run()

  return () => controller.abort()
}
