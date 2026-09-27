<script setup lang="ts">
// SessionDetailView.vue
//
// 单个会话详情 + 输入框 + 流式 SSE 渲染。
// 消息结构（前端动态拼接）：
//   - 用户消息 → 用户气泡
//   - assistant 流式累积（onFrame event === 'delta'）→ 助手气泡
//   - tool_call → 折叠块
//   - usage 在最后一条消息后显示

import { useQuery } from '@tanstack/vue-query'
import { computed, ref, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Send, ArrowLeft, Wrench } from 'lucide-vue-next'
import { sessionsApi } from '@/api/sessions'
import { formatTime } from '@/utils/format'
import MarkdownView from '@/components/MarkdownView.vue'

interface UiMessage {
  id: string
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  toolCalls?: Array<{ id: string; name: string; args: string }>
  toolCallId?: string
  pending?: boolean
  ts: number
}

const route = useRoute()
const router = useRouter()

const sid = computed(() => decodeURIComponent(String(route.params.sid ?? '')))

const detail = useQuery({
  queryKey: ['session', sid],
  queryFn: () => sessionsApi.get(sid.value),
  enabled: computed(() => !!sid.value),
})

// 前端累积的"实时消息"列表（API 返回的是历史，pending 的是正在流式输出）
const live = ref<UiMessage[]>([])
const draft = ref('')
const sending = ref(false)
const abortCtrl = ref<AbortController | null>(null)
const scrollEl = ref<HTMLElement | null>(null)

const allMessages = computed<UiMessage[]>(() => {
  const historical: UiMessage[] = (detail.data.value?.messages ?? []).map((m, i) => ({
    id: `hist-${i}`,
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

async function send() {
  const text = draft.value.trim()
  if (!text || sending.value) return
  draft.value = ''

  // 把用户消息加到 live
  const userMsg: UiMessage = {
    id: `u-${Date.now()}`,
    role: 'user',
    content: text,
    ts: Date.now(),
  }
  // assistant 占位
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
        const d = frame.data
        if (ev === 'delta') {
          aMsg.content += String(d.text ?? '')
        } else if (ev === 'tool_call') {
          aMsg.toolCalls = [
            ...(aMsg.toolCalls ?? []),
            {
              id: String(d.id ?? ''),
              name: String(d.name ?? ''),
              args: JSON.stringify(d.args ?? {}, null, 2),
            },
          ]
        } else if (ev === 'done' || ev === 'finish') {
          aMsg.pending = false
          detail.refetch()
        } else if (ev === 'error') {
          aMsg.content += `\n\n[error] ${String(d.message ?? '')}`
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
</script>

<template>
  <div class="session-detail">
    <div class="header">
      <button class="btn" @click="router.push('/sessions')">
        <ArrowLeft :size="14" />
        返回
      </button>
      <h2 class="text-lg font-bold">{{ detail.data.value?.title || '会话详情' }}</h2>
      <div class="text-sm text-muted">
        {{ detail.data.value?.model }} · {{ allMessages.length }} 条消息 · {{ totalTokens }} tokens
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
          <span class="ts">#{{ m.id.slice(-6) }}</span>
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
        <button v-if="sending" class="btn btn-danger" @click="abort">停止</button>
        <button class="btn btn-primary" :disabled="sending || !draft.trim()" @click="send">
          <Send :size="14" />
          {{ sending ? '生成中…' : '发送' }}
        </button>
      </div>
    </div>
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
  background: #f7fafc;
  border: 1px solid var(--border);
}
.msg.role-user {
  align-self: flex-end;
  background: #ebf4ff;
  border-color: #bee3f8;
}
.msg.role-assistant {
  align-self: flex-start;
}
.msg.role-tool {
  align-self: flex-start;
  background: #fefcbf;
  border-color: #f6e05e;
}
.msg.pending {
  opacity: 0.7;
}
.meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--text-muted);
  margin-bottom: 6px;
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
