<script setup lang="ts">
// SessionDetailView.vue
//
// 单个会话详情 + 输入框 + 流式 SSE 渲染 + 消息编辑/删除 + Token 用量明细。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, nextTick, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Send, ArrowLeft, Wrench, Pencil, Trash2, Save, X, RotateCw } from 'lucide-vue-next'
import { sessionsApi, type SessionEvent } from '@/api/sessions'
import { openSSE, type SSEHandle } from '@/utils/sse'
import { formatTime } from '@/utils/format'
import { useUIStore } from '@/stores/ui'
import Modal from '@/components/Modal.vue'
import MarkdownView from '@/components/MarkdownView.vue'

interface UiMessage {
  id: string
  seq?: number
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  toolCalls?: Array<{ id: string; name: string; args: string }>
  toolCallId?: string
  pending?: boolean
  ts: number
}

const route = useRoute()
const router = useRouter()
const qc = useQueryClient()
const ui = useUIStore()

const sid = computed(() => decodeURIComponent(String(route.params.sid ?? '')))

const detail = useQuery({
  queryKey: ['session', sid],
  queryFn: () => sessionsApi.get(sid.value),
  enabled: computed(() => !!sid.value),
})

const live = ref<UiMessage[]>([])
const draft = ref('')
const sending = ref(false)
const abortCtrl = ref<AbortController | null>(null)
const scrollEl = ref<HTMLElement | null>(null)

// v8.1 Spill：断点续传状态
const lastSpillSeq = ref<number>(-1)
const spillConnected = ref(false)
const spillReconnects = ref(0)
let spillHandle: SSEHandle | null = null

onMounted(() => {
  startSpillResume()
})

onUnmounted(() => {
  spillHandle?.close()
})

async function startSpillResume() {
  // 先拉 since=-1 一次性拿到当前 lastSeq（实际是 max(seq)）
  const resp = await sessionsApi.eventsSince(sid.value, -1)
  if (resp === null) {
    // backend 不支持 Spill；保持不连接
    return
  }
  lastSpillSeq.value = resp.lastSeq
  // 启动 SSE 长连接，since=lastSeq
  spillHandle = openSSE({
    url: `/api/v1/console/sessions/${encodeURIComponent(sid.value)}/events`,
    sinceSeq: resp.lastSeq,
    onOpen: () => {
      spillConnected.value = true
      spillReconnects.value = 0
    },
    onError: () => {
      spillConnected.value = false
    },
    onReconnect: (n) => {
      spillReconnects.value = n
      spillConnected.value = false
    },
    onMessage: (data) => {
      try {
        const ev = JSON.parse(data) as SessionEvent
        applySpillEvent(ev)
        lastSpillSeq.value = ev.seq
      } catch {
        /* ignore non-JSON */
      }
    },
  })
}

// applySpillEvent 把后端 push 来的 event 转成 UI 消息（仅 assistant_delta / tool_result 可见）。
function applySpillEvent(ev: SessionEvent) {
  const t = ev.type
  // 简化：只识别常见的 few types（其余忽略）。
  const p = (ev.payload ?? {}) as Record<string, unknown>
  if (t === 2 /* assistant_delta */) {
    const text = String(p.text ?? '')
    const last = live.value[live.value.length - 1]
    if (last && last.role === 'assistant' && last.pending) {
      last.content += text
    } else {
      live.value.push({
        id: `spill-${ev.seq}`,
        seq: ev.seq,
        role: 'assistant',
        content: text,
        ts: new Date(ev.ts).getTime(),
      })
    }
  } else if (t === 6 /* tool_result */) {
    live.value.push({
      id: `spill-tool-${ev.seq}`,
      seq: ev.seq,
      role: 'tool',
      content: String(p.content ?? ''),
      toolCallId: String(p.tool_call_id ?? ''),
      ts: new Date(ev.ts).getTime(),
    })
  }
}

const editOpen = ref(false)
const editSeq = ref<number | null>(null)
const editContent = ref('')

const deleteMut = useMutation({
  mutationFn: ({ seq }: { seq: number }) => sessionsApi.deleteMessage(sid.value, seq),
  onSuccess: () => {
    detail.refetch()
    ui.pushToast('success', '消息已删除')
  },
  onError: (e: Error) => ui.reportError(e, '删除失败'),
})

const editMut = useMutation({
  mutationFn: ({ seq, content }: { seq: number; content: string }) =>
    sessionsApi.editMessage(sid.value, seq, content),
  onSuccess: () => {
    editOpen.value = false
    detail.refetch()
    qc.invalidateQueries({ queryKey: ['session', sid] })
    ui.pushToast('success', '消息已更新')
  },
  onError: (e: Error) => ui.reportError(e, '编辑失败'),
})

const allMessages = computed<UiMessage[]>(() => {
  const historical: UiMessage[] = (detail.data.value?.messages ?? []).map((m, i) => ({
    id: `hist-${i}`,
    seq: (m as any).seq ?? i,
    role: m.role,
    content: m.content ?? '',
    toolCalls: m.tool_calls?.map((tc) => ({
      id: tc.id,
      name: tc.function.name,
      args: tc.function.arguments,
    })),
    toolCallId: m.tool_call_id,
    ts: i,
  }))
  return [...historical, ...live.value]
})

const usage = computed(() => detail.data.value?.usage)
const totalTokens = computed(() => usage.value?.totalTokens ?? 0)
const promptTokens = computed(() => usage.value?.promptTokens ?? 0)
const completionTokens = computed(() => usage.value?.completionTokens ?? 0)
// ds-java 控制台还会把"已缓存" / "reasoning" 拆出；ds-go 当前 Usage
// 只暴露 prompt + completion + total。计算 cacheTokens = total - 其他。
const cacheTokens = computed(() =>
  Math.max(0, totalTokens.value - promptTokens.value - completionTokens.value),
)

const rounds = computed(() => detail.data.value?.rounds ?? 0)

async function send() {
  const text = draft.value.trim()
  if (!text || sending.value) return
  draft.value = ''

  const userMsg: UiMessage = {
    id: `u-${Date.now()}`,
    role: 'user',
    content: text,
    ts: Date.now(),
  }
  const aMsg: UiMessage = {
    id: `a-${Date.now()}`,
    role: 'assistant',
    content: '',
    pending: true,
    ts: Date.now(),
  }
  live.value = [...live.value, userMsg, aMsg]
  await nextTick()
  scrollToBottom()

  sending.value = true
  abortCtrl.value = new AbortController()
  try {
    await sessionsApi.sendStream(
      sid.value,
      text,
      (frame) => {
        const ev = frame.event
        const d = frame.data as Record<string, unknown>
        if (ev === 'assistant_delta') {
          aMsg.content += String(d.text ?? '')
        } else if (ev === 'tool_call_start') {
          const call = (d.call as any) ?? {}
          aMsg.toolCalls = [
            ...(aMsg.toolCalls ?? []),
            {
              id: String(call.id ?? ''),
              name: String(call.function?.name ?? ''),
              args: JSON.stringify(call.function?.arguments ?? {}, null, 2),
            },
          ]
        } else if (ev === 'assistant_message') {
          aMsg.content += ''
        } else if (ev === 'tool_result') {
          const name = String(d.name ?? 'tool')
          const content = String(d.content ?? '')
          const isErr = Boolean(d.isError)
          live.value = [
            ...live.value,
            {
              id: `t-${Date.now()}`,
              role: 'tool',
              content,
              ts: Date.now(),
              toolCallId: String(d.callId ?? ''),
              toolCalls: undefined,
            },
          ]
        } else if (ev === 'loop_done') {
          aMsg.pending = false
          detail.refetch()
        } else if (ev === 'loop_error') {
          aMsg.content += `\n\n[error] ${String(d.err ?? '')}`
          aMsg.pending = false
        }
      },
      abortCtrl.value.signal,
    )
  } catch (e) {
    aMsg.content += `\n\n[error] ${(e as Error).message}`
  } finally {
    aMsg.pending = false
    sending.value = false
    abortCtrl.value = null
    await nextTick()
    scrollToBottom()
    detail.refetch()
  }
}

function abort() {
  abortCtrl.value?.abort()
}

function scrollToBottom() {
  if (!scrollEl.value) return
  scrollEl.value.scrollTop = scrollEl.value.scrollHeight
}

function openEdit(msg: UiMessage) {
  if (msg.role !== 'user' && msg.role !== 'system') {
    ui.pushToast('error', '助手 / 工具消息不可编辑')
    return
  }
  editSeq.value = msg.seq ?? null
  editContent.value = msg.content
  editOpen.value = true
}

function confirmEdit() {
  if (editSeq.value == null || !editContent.value.trim()) return
  editMut.mutate({ seq: editSeq.value, content: editContent.value.trim() })
}

function confirmDelete(msg: UiMessage) {
  if (msg.seq == null) {
    ui.pushToast('error', '本条消息无 seq（流式中）')
    return
  }
  if (!window.confirm('删除这条消息？')) return
  deleteMut.mutate({ seq: msg.seq })
}
</script>

<template>
  <div class="session-detail">
    <div class="header">
      <button class="btn" @click="router.push('/sessions')">
        <ArrowLeft :size="14" />
        返回
      </button>
      <div style="flex: 1; min-width: 0;">
        <h2 class="text-lg font-bold">{{ detail.data.value?.title || '会话详情' }}</h2>
        <div class="text-sm text-muted">
          模型 {{ detail.data.value?.model || '—' }} · {{ allMessages.length }} 条消息 · {{ rounds }} 轮
        </div>
      </div>
      <span v-if="spillConnected" class="spill-badge" :title="`Spill: 已连接 (lastSeq=${lastSpillSeq})`">
        <RotateCw :size="12" />
        spill
      </span>
      <span v-else-if="spillReconnects > 0" class="spill-badge spill-reconnecting" :title="`Spill: 重连中 (尝试 #${spillReconnects})`">
        <RotateCw :size="12" />
        重连 {{ spillReconnects }}
      </span>
      <div class="usage-card">
        <div class="usage-title">Token 用量</div>
        <div class="usage-row">
          <span class="usage-label">prompt</span>
          <span class="usage-bar"><span :style="{ width: totalTokens ? (promptTokens / totalTokens) * 100 + '%' : '0%' }" /></span>
          <span class="usage-value">{{ promptTokens }}</span>
        </div>
        <div class="usage-row">
          <span class="usage-label">completion</span>
          <span class="usage-bar"><span :style="{ width: totalTokens ? (completionTokens / totalTokens) * 100 + '%' : '0%' }" /></span>
          <span class="usage-value">{{ completionTokens }}</span>
        </div>
        <div v-if="cacheTokens" class="usage-row">
          <span class="usage-label">cache</span>
          <span class="usage-bar"><span :style="{ width: totalTokens ? (cacheTokens / totalTokens) * 100 + '%' : '0%' }" /></span>
          <span class="usage-value">{{ cacheTokens }}</span>
        </div>
        <div class="usage-row usage-total">
          <span class="usage-label">total</span>
          <span class="usage-value">{{ totalTokens }}</span>
        </div>
      </div>
    </div>

    <div v-if="detail.error.value" class="error-banner">
      {{ (detail.error.value as Error).message }}
    </div>

    <div ref="scrollEl" class="messages">
      <div v-if="detail.isLoading.value" class="empty">加载中…</div>

      <div
        v-for="m in allMessages"
        :key="m.id"
        :class="['msg', `role-${m.role}`, { pending: m.pending }]"
      >
        <div class="meta">
          <span class="role">{{ m.role }}</span>
          <span class="ts">#{{ m.seq ?? m.id.slice(-6) }} · {{ formatTime(new Date(m.ts)) }}</span>
          <div class="msg-actions">
            <button v-if="m.role === 'user' || m.role === 'system'" class="icon-btn" title="编辑" @click="openEdit(m)">
              <Pencil :size="12" />
            </button>
            <button v-if="!m.pending" class="icon-btn" title="删除" @click="confirmDelete(m)">
              <Trash2 :size="12" />
            </button>
          </div>
        </div>
        <MarkdownView v-if="m.role === 'assistant' || m.role === 'system'" :source="m.content || (m.pending ? '…' : '')" />
        <div v-else class="content">{{ m.content }}</div>

        <details v-if="m.toolCalls && m.toolCalls.length" class="tool-calls">
          <summary>
            <Wrench :size="12" />
            {{ m.toolCalls.length }} 个工具调用
          </summary>
          <div v-for="tc in m.toolCalls" :key="tc.id" class="tool-call">
            <div class="tool-name">{{ tc.name }}</div>
            <pre>{{ tc.args }}</pre>
          </div>
        </details>
      </div>
    </div>

    <div class="composer">
      <textarea
        v-model="draft"
        rows="2"
        placeholder="发送消息…（Enter 发送，Shift+Enter 换行）"
        @keydown.enter.exact.prevent="send"
      />
      <div class="composer-actions">
        <button v-if="sending" class="btn btn-danger" @click="abort">
          <X :size="14" />
          停止
        </button>
        <button class="btn btn-primary" :disabled="sending || !draft.trim()" @click="send">
          <Send :size="14" />
          {{ sending ? '生成中…' : '发送' }}
        </button>
      </div>
    </div>

    <Modal :open="editOpen" title="编辑消息" @close="editOpen = false" @confirm="confirmEdit">
      <label>
        <span>新内容</span>
        <textarea v-model="editContent" rows="6" />
      </label>
      <template #footer>
        <button class="btn" @click="editOpen = false">取消</button>
        <button class="btn btn-primary" :disabled="editMut.isPending.value || !editContent.trim()" @click="confirmEdit">
          <Save :size="14" />
          {{ editMut.isPending.value ? '保存中…' : '保存' }}
        </button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.session-detail {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 84px);
}
.header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 12px;
}
.spill-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  background: rgba(59, 130, 246, 0.12);
  color: rgb(59, 130, 246);
}
.spill-reconnecting {
  background: rgba(234, 179, 8, 0.12);
  color: rgb(234, 179, 8);
}
.usage-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 12px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  min-width: 220px;
  font-size: 11px;
}
.usage-title {
  font-weight: 600;
  font-size: 12px;
  margin-bottom: 4px;
}
.usage-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.usage-label {
  width: 70px;
  color: var(--text-muted);
}
.usage-bar {
  flex: 1;
  height: 6px;
  background: var(--border);
  border-radius: 3px;
  overflow: hidden;
}
.usage-bar span {
  display: block;
  height: 100%;
  background: var(--accent);
}
.usage-value {
  font-variant-numeric: tabular-nums;
  min-width: 50px;
  text-align: right;
}
.usage-total .usage-value {
  font-weight: 600;
}
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.msg {
  border-radius: 8px;
  padding: 10px 14px;
  max-width: 90%;
  background: var(--bg-card);
  border: 1px solid var(--border);
}
.msg.role-user {
  align-self: flex-end;
  background: var(--bg);
  border-color: var(--accent);
}
.msg.role-assistant {
  align-self: flex-start;
}
.msg.role-tool {
  align-self: flex-start;
  background: var(--warning-bg);
  border-color: var(--warning);
}
:global([data-theme="dark"]) .msg.role-user {
  background: #1e3a5f;
  border-color: var(--accent);
}
.msg.pending {
  opacity: 0.7;
}
.meta {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 11px;
  color: var(--text-muted);
  margin-bottom: 6px;
}
.role {
  font-weight: 600;
  text-transform: uppercase;
}
.msg-actions {
  margin-left: auto;
  display: flex;
  gap: 4px;
}
.icon-btn {
  background: transparent;
  border: 1px solid transparent;
  color: var(--text-muted);
  border-radius: 4px;
  padding: 2px 4px;
  cursor: pointer;
}
.icon-btn:hover {
  color: var(--accent);
  border-color: var(--accent);
}
.content {
  white-space: pre-wrap;
  word-wrap: break-word;
}
.tool-calls {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed var(--border);
  font-size: 12px;
}
.tool-calls summary {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.tool-call {
  margin-top: 6px;
  padding: 8px;
  background: #1a202c;
  color: #edf2f7;
  border-radius: 4px;
}
.tool-name {
  font-size: 11px;
  color: #fbd38d;
  margin-bottom: 4px;
}
.tool-call pre {
  margin: 0;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-all;
}

.composer {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.composer textarea {
  resize: vertical;
  min-height: 60px;
}
.composer-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
