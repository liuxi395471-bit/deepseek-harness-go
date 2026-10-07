<script setup lang="ts">
// SessionDetailView.vue — dsh 桌面端聊天主界面
//
// 严格按 dsh 原生 web 视觉反推（参考 packages/client/ui-chat/* / SidebarRoot.module.css）。
// 设计目标：和 dsh 桌面端一摸一样，不添加任何 ds-h 原本没有的元素。
//
// 关键视觉锚点（dsh 真实）：
//   - topbar 38px（dsh 顶栏原始高度）
//   - messages max-width 748px（dsh --dsh-chat-content-width）
//   - user bubble 20px 圆角、82% max-width、右对齐（dsh 原生 userStack）
//   - assistant 左对齐无 bubble、26×26 Sparkles avatar
//   - 工具调用：collapsed <details>，圆点 + 命令名
//   - composer 圆角 card：textarea + mid tool row + foot [+ workspace / speed model / send]
//   - composer statusbar：6 个维度（轮 / 步 / tok/s / total / 缓存 / %）

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  RefreshCw,
  Copy,
  Check,
  ChevronDown,
  Cpu,
  Zap,
  Brain,
  ArrowUp,
  ArrowDown,
  Database,
  Paperclip,
  Mic,
  History,
  Sparkles,
  PanelRight,
  PanelRightClose,
  FileText,
  FileEdit,
  Terminal as TerminalIcon,
  ListChecks,
  Activity,
  ChevronRight,
  Plus,
  AtSign,
  Globe,
  CheckCircle2,
  FolderTree,
  Hourglass,
  CircleDot,
  GitBranch,
  Clock,
  Gauge,
  Search,
  Pin,
  ThumbsUp,
  ThumbsDown,
  RotateCw,
  Download,
  Pencil,
  X as XIcon,
} from 'lucide-vue-next'
import { sessionsApi, type SessionEvent } from '@/api/sessions'
import { modelsApi } from '@/api/models'
import { openSSE, type SSEHandle } from '@/utils/sse'
import { copyToClipboard } from '@/utils/format'
import { useUIStore } from '@/stores/ui'
import MarkdownView from '@/components/MarkdownView.vue'
import StepChain from '@/components/StepChain.vue'
import FileCard from '@/components/FileCard.vue'

interface UiMessage {
  id: string
  seq?: number
  role: 'user' | 'assistant' | 'tool' | 'system'
  content: string
  toolCalls?: Array<{ id: string; name: string; args: string }>
  toolName?: string
  isError?: boolean
  pending?: boolean
  ts: number
  usage?: TurnUsage
  steps?: number
  stepsData?: Array<{
    id: string
    icon: 'edit' | 'terminal' | 'web' | 'search' | 'tool' | 'file'
    title: string
    detail?: string
    preview?: string
    previewKind?: 'code' | 'text'
    done?: boolean
  }>
  fileCard?: {
    kind: 'edit' | 'create'
    path: string
    lineCol?: string
    description?: string
    preview?: string
    diff?: string
  }
  startedAt?: number
  finishedAt?: number
  model?: string
}

interface TurnUsage {
  promptTokens: number
  completionTokens: number
  totalTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  reasoningTokens: number
  cacheHitRate: number
  finishedAt?: number
  startedAt?: number
  tokPerSec?: number
}

const route = useRoute()
const router = useRouter()
const qc = useQueryClient()
const ui = useUIStore()

const sid = computed(() => decodeURIComponent(String(route.params.sid ?? '')))
const isDemo = computed(() => sid.value === 'demo')

const detail = useQuery({
  queryKey: ['session', sid],
  queryFn: () => sessionsApi.get(sid.value),
  enabled: computed(() => !!sid.value && !isDemo.value),
  retry: false,
})

const models = useQuery({
  queryKey: ['models'],
  queryFn: modelsApi.list,
  refetchInterval: 30_000,
  retry: false,
})

const sidePanelOpen = ref(true)

function emptyTurnUsage(): TurnUsage {
  return {
    promptTokens: 0,
    completionTokens: 0,
    totalTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    reasoningTokens: 0,
    cacheHitRate: 0,
    tokPerSec: 0,
  }
}

function cacheHitRate(u: TurnUsage): number {
  const billed =
    u.cacheReadTokens +
    u.cacheWriteTokens +
    Math.max(0, u.promptTokens - u.cacheReadTokens - u.cacheWriteTokens)
  if (billed <= 0) return 0
  return u.cacheReadTokens / billed
}

function shortNum(n: number): string {
  if (!n) return '0'
  if (n < 1000) return String(n)
  if (n < 10_000) return (n / 1000).toFixed(1) + 'k'
  if (n < 1_000_000) return Math.round(n / 1000) + 'k'
  return (n / 1_000_000).toFixed(1) + 'M'
}

function formatTime2(ms: number): string {
  if (ms < 1000) return ms + 'ms'
  if (ms < 60_000) return (ms / 1000).toFixed(1) + 's'
  return Math.round(ms / 1000) + 's'
}

const live = ref<UiMessage[]>([])
const draft = ref('')
const sending = ref(false)
const abortCtrl = ref<AbortController | null>(null)
const scrollEl = ref<HTMLElement | null>(null)
const composerEl = ref<HTMLTextAreaElement | null>(null)

// v8.1 P3: Composer attachment list（DESKTOP-FRONTEND §4.3 draft attachment rail）
// attachments 是 composable 内的临时附件列表，发送时随 prompt 一起提交；
// 后端 attachment 工具未实现 → 仅本地维护显示。
interface ComposerAttachment {
  id: string
  name: string
  kind: 'file' | 'image' | 'reference'
  size?: number
}
const attachments = ref<ComposerAttachment[]>([])

function addAttachment() {
  const id = 'att_' + Math.random().toString(36).slice(2, 9)
  attachments.value.push({ id, name: 'untitled.txt', kind: 'file' })
}
function removeAttachment(id: string) {
  attachments.value = attachments.value.filter((a) => a.id !== id)
}

const stickToBottom = ref(true)
const caretVisible = ref(true)
let caretTimer: number | null = null

const selectedModel = ref<string>('')
// v8.1 P3: effort（DESKTOP-FRONTEND §8.3）—— 由 model 决定可选项；这里先 3 档
const selectedEffort = ref<'low' | 'medium' | 'high'>('medium')
const modelSpeed = ref<'low' | 'medium' | 'high'>('high')

watch(models, (m) => {
  if (!selectedModel.value && m.data.value?.[0]) {
    selectedModel.value = m.data.value[0].channel
  }
}, { immediate: true })

const lastSpillSeq = ref<number>(-1)
const spillConnected = ref(false)
const spillReconnects = ref(0)
let spillHandle: SSEHandle | null = null

onMounted(() => {
  if (isDemo.value) return
  startSpillResume()
  startCaretBlink()
})

onUnmounted(() => {
  spillHandle?.close()
  if (caretTimer) clearInterval(caretTimer)
})

function startCaretBlink() {
  caretTimer = window.setInterval(() => {
    caretVisible.value = !caretVisible.value
  }, 530) as unknown as number
}

async function startSpillResume() {
  try {
    const resp = await sessionsApi.eventsSince(sid.value, -1)
    if (resp === null) return
    lastSpillSeq.value = resp.lastSeq
  } catch {
    /* ignore */
  }
  spillHandle = openSSE({
    url: `/api/v1/console/sessions/${encodeURIComponent(sid.value)}/events`,
    sinceSeq: lastSpillSeq.value,
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
        /* ignore */
      }
    },
  })
}

function applySpillEvent(ev: SessionEvent) {
  const t = ev.type
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
        pending: true,
        ts: new Date(ev.ts).getTime(),
      })
    }
    if (stickToBottom.value) scrollToBottom()
  } else if (t === 6 /* tool_result */) {
    live.value.push({
      id: `spill-tool-${ev.seq}`,
      seq: ev.seq,
      role: 'tool',
      content: String(p.content ?? ''),
      toolName: String(p.name ?? 'tool'),
      isError: Boolean(p.is_error ?? p.isError),
      ts: new Date(ev.ts).getTime(),
    })
    if (stickToBottom.value) scrollToBottom()
  }
}

const liveUsage = ref<TurnUsage>(emptyTurnUsage())
const storedUsage = computed(() => detail.data.value?.usage)
const ratings = ref<Record<number, number>>({})
watch(detail, (d) => {
  if (d.data.value && (d.data.value as any).ratings) {
    ratings.value = { ...(d.data.value as any).ratings }
  }
})

// v8.1 P1：feedback / regenerate / export
const feedbackMut = useMutation({
  mutationFn: ({ seq, rating }: { seq: number; rating: -1 | 0 | 1 }) =>
    sessionsApi.setFeedback(sid.value, seq, rating, ''),
  onSuccess: (_d, v) => {
    if (v.rating === 0) delete ratings.value[v.seq]
    else ratings.value[v.seq] = v.rating
    ratings.value = { ...ratings.value }
    detail.refetch()
  },
  onError: (e: Error) => ui.reportError(e, '评分失败'),
})

const exportMd = async () => {
  if (isDemo.value) {
    ui.pushToast('error', 'Demo 会话不可导出')
    return
  }
  try {
    const blob = await sessionsApi.exportMd(sid.value)
    downloadBlob(blob, `${sid.value}.md`)
    ui.pushToast('success', 'Markdown 已导出')
  } catch (e) {
    ui.reportError(e as Error, '导出失败')
  }
}

const exportJsonl = async () => {
  if (isDemo.value) {
    ui.pushToast('error', 'Demo 会话不可导出')
    return
  }
  try {
    const blob = await sessionsApi.exportJsonl(sid.value)
    downloadBlob(blob, `${sid.value}.jsonl`)
    ui.pushToast('success', 'JSONL 已导出')
  } catch (e) {
    ui.reportError(e as Error, '导出失败')
  }
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

const exportMenuOpen = ref(false)
function toggleExportMenu() {
  exportMenuOpen.value = !exportMenuOpen.value
}
function closeExportMenu() {
  setTimeout(() => (exportMenuOpen.value = false), 150)
}
function closeExport() {
  exportMenuOpen.value = false
}

async function regenerate(seq: number) {
  if (isDemo.value) {
    ui.pushToast('error', 'Demo 会话不可重生成')
    return
  }
  if (sending.value) return
  sending.value = true
  const aMsg: UiMessage = {
    id: `regen-${Date.now()}`,
    role: 'assistant',
    content: '',
    pending: true,
    ts: Date.now(),
    startedAt: Date.now(),
    steps: 0,
  }
  live.value = [...live.value, aMsg]
  await nextTick()
  scrollToBottom()
  abortCtrl.value = new AbortController()
  try {
    await sessionsApi.regenerate(
      sid.value,
      seq,
      (frame) => {
        const ev = frame.event
        const d = frame.data as Record<string, unknown>
        if (ev === 'assistant_delta') {
          aMsg.content += String(d.text ?? '')
          if (stickToBottom.value) scrollToBottom()
        } else if (ev === 'loop_done') {
          aMsg.pending = false
          aMsg.finishedAt = Date.now()
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
const displayUsage = computed<TurnUsage>(() => {
  if (liveUsage.value.totalTokens > 0) return liveUsage.value
  if (isDemo.value) {
    return {
      promptTokens: 8_762,
      completionTokens: 1_493,
      totalTokens: 56_312,
      cacheReadTokens: 47_857,
      cacheWriteTokens: 0,
      reasoningTokens: 0,
      cacheHitRate: 0.85,
      tokPerSec: 258,
    }
  }
  const s = storedUsage.value
  if (!s) return emptyTurnUsage()
  const u: TurnUsage = {
    promptTokens: s.promptTokens ?? 0,
    completionTokens: s.completionTokens ?? 0,
    totalTokens: s.totalTokens ?? 0,
    cacheReadTokens: s.cacheReadTokens ?? 0,
    cacheWriteTokens: s.cacheWriteTokens ?? 0,
    reasoningTokens: s.reasoningTokens ?? 0,
    cacheHitRate: 0,
    tokPerSec: (s as any).tokPerSec ?? 0,
  }
  u.cacheHitRate = cacheHitRate(u)
  return u
})

const rounds = computed(() => (isDemo.value ? 1 : detail.data.value?.rounds ?? 0))
const stepsCount = computed(() => (isDemo.value ? 7 : (detail.data.value as any)?.steps ?? 0))
const sessionCtxPct = computed(() => {
  const u = displayUsage.value
  if (!u.totalTokens) return isDemo.value ? 1 : 0
  return Math.min(1, u.totalTokens / 128_000)
})

const allMessages = computed<UiMessage[]>(() => {
  if (isDemo.value) {
    return buildDemoMessages(displayUsage.value)
  }
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
    ts: i,
  }))
  return [...historical, ...live.value]
})

function buildDemoMessages(_u: TurnUsage): UiMessage[] {
  const now = Date.now()
  return [
    {
      id: 'demo-u1',
      role: 'user',
      content: '用 typewriter 帮我写个 go hello world，保存到 hello.go 然后编译运行',
      ts: now - 32_000,
    },
    {
      id: 'demo-a1',
      role: 'assistant',
      content:
        '好的，我来创建 `hello.go` 然后运行它。\n\nHello world 程序是最简单的 Go 程序：使用 `package main` 声明这是入口包，`func main()` 是程序入口。',
      ts: now - 30_000,
      startedAt: now - 30_000,
      finishedAt: now - 1_000,
      model: 'DeepSeek-V4-1-Flash',
      steps: 4,
      usage: {
        promptTokens: 8_762,
        completionTokens: 1_493,
        totalTokens: 56_312,
        cacheReadTokens: 47_857,
        cacheWriteTokens: 0,
        reasoningTokens: 0,
        cacheHitRate: 0.85,
        tokPerSec: 258,
        startedAt: now - 30_000,
        finishedAt: now - 1_000,
      },
      toolCalls: [
        {
          id: 'demo-tc-1',
          name: 'file_write',
          args: JSON.stringify(
            { path: 'hello.go', content: 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("Hello, World!")\n}\n' },
            null,
            2,
          ),
        },
        {
          id: 'demo-tc-2',
          name: 'terminal',
          args: JSON.stringify({ command: 'go run hello.go' }, null, 2),
        },
      ],
      fileCard: {
        kind: 'create',
        path: 'hello.go',
        lineCol: '(0, 0)',
        description: 'Go Hello World 程序，使用 fmt.Println 输出问候',
        preview:
          'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("Hello, World!")\n}\n',
      },
      stepsData: [
        { id: 'ds1', icon: 'file', title: 'hello.go', detail: '40 行 · 新建', done: true },
        {
          id: 'ds2',
          icon: 'terminal',
          title: 'go run hello.go',
          detail: '运行命令',
          preview: '$ go run hello.go\nHello, World!\n\n进程已结束，退出代码为 0',
          previewKind: 'text',
          done: true,
        },
        { id: 'ds3', icon: 'search', title: 'file_search', detail: '*.go', done: true },
      ],
    },
  ]
}

async function send() {
  if (isDemo.value) {
    ui.pushToast('error', 'Demo 会话不可发送 — 请新建会话')
    return
  }
  const text = draft.value.trim()
  if (!text || sending.value) return
  draft.value = ''
  stickToBottom.value = true

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
    startedAt: Date.now(),
    steps: 0,
    model: selectedModel.value,
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
          if (stickToBottom.value) scrollToBottom()
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
          aMsg.steps = (aMsg.steps ?? 0) + 1
        } else if (ev === 'tool_result') {
          const name = String(d.name ?? 'tool')
          const content = String(d.content ?? '')
          live.value = [
            ...live.value,
            {
              id: `t-${Date.now()}`,
              role: 'tool',
              content,
              toolName: name,
              isError: Boolean(d.isError),
              ts: Date.now(),
            },
          ]
        } else if (ev === 'usage_update') {
          // v8.1 P1：流式期间实时 usage 增量；让 composer status bar 跳动。
          liveUsage.value = {
            promptTokens: Number(d.promptTokens ?? 0),
            completionTokens: Number(d.completionTokens ?? 0),
            totalTokens: Number(d.totalTokens ?? 0),
            cacheReadTokens: Number(d.cacheReadTokens ?? 0),
            cacheWriteTokens: Number(d.cacheWriteTokens ?? 0),
            reasoningTokens: Number(d.reasoningTokens ?? 0),
            cacheHitRate: Number(d.cacheHitRate ?? 0),
            tokPerSec: aMsg.usage?.tokPerSec ?? 0,
          }
        } else if (ev === 'loop_done') {
          aMsg.pending = false
          aMsg.finishedAt = Date.now()
          const turn: TurnUsage = {
            promptTokens: Number(d.promptTokens ?? 0),
            completionTokens: Number(d.completionTokens ?? 0),
            totalTokens: Number(d.totalTokens ?? 0),
            cacheReadTokens: Number(d.cacheReadTokens ?? 0),
            cacheWriteTokens: Number(d.cacheWriteTokens ?? 0),
            reasoningTokens: Number(d.reasoningTokens ?? 0),
            cacheHitRate: Number(d.cacheHitRate ?? 0),
            startedAt: aMsg.startedAt,
            finishedAt: aMsg.finishedAt,
            tokPerSec: Number(d.tokPerSec ?? 0),
          }
          if (!turn.cacheHitRate && (turn.promptTokens || turn.cacheReadTokens)) {
            turn.cacheHitRate = cacheHitRate(turn)
          }
          if (!turn.tokPerSec && turn.completionTokens && turn.startedAt && turn.finishedAt) {
            turn.tokPerSec = turn.completionTokens / ((turn.finishedAt - turn.startedAt) / 1000)
          }
          aMsg.usage = turn
          liveUsage.value = turn
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

function onScroll() {
  const el = scrollEl.value
  if (!el) return
  const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
  stickToBottom.value = distanceFromBottom < 80
}

watch(draft, () => {
  const el = composerEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 220) + 'px'
})

const copiedId = ref<string | null>(null)
async function copyMessage(m: UiMessage) {
  await copyToClipboard(m.content)
  copiedId.value = m.id
  setTimeout(() => {
    if (copiedId.value === m.id) copiedId.value = null
  }, 1200)
}

function cacheColor(hit: number): string {
  if (hit >= 0.7) return 'var(--success)'
  if (hit >= 0.4) return 'var(--warning)'
  return 'var(--text-muted)'
}

const currentModelLabel = computed(() => {
  const m = (models.data.value ?? []).find((x) => x.channel === selectedModel.value)
  return m?.model || m?.channel || selectedModel.value || 'DeepSeek-V4-1-Flash'
})

const cbCacheHitNum = computed(() => cacheHitRate(displayUsage.value))
const cbCacheHitPct = computed(() => Math.round(cbCacheHitNum.value * 100))

// v8.1 P3: Context % ring + speed label（DESKTOP-FRONTEND §4.3）
const speedLabel = computed(() => {
  switch (modelSpeed.value) {
    case 'low': return '慢'
    case 'medium': return '中'
    default: return '快'
  }
})
// Context % = usage 占上下文窗口（200k 简化）的占比；上限 100
const CONTEXT_WINDOW = 200_000
const contextPct = computed(() =>
  Math.min(100, Math.round((displayUsage.value.totalTokens / CONTEXT_WINDOW) * 100)),
)
const contextColor = computed(() => {
  const p = contextPct.value
  if (p >= 80) return 'var(--danger)'
  if (p >= 50) return 'var(--warning)'
  return 'var(--accent)'
})
const contextDash = computed(() => {
  // r=7 → 周长 ≈ 43.98
  const circ = 2 * Math.PI * 7
  return `${(circ * contextPct.value) / 100} ${circ}`
})

const recentActivity = computed(() => {
  if (isDemo.value) {
    return [
      { id: 1, kind: 'edit', text: '已创建 hello.go', time: '16:50' },
      { id: 2, kind: 'tool', text: '运行 go run hello.go · Hello, World!', time: '16:50' },
      { id: 3, kind: 'edit', text: '读取 file_search (*.go)', time: '16:49' },
      { id: 4, kind: 'tool', text: '运行 file_search · 12 个文件', time: '16:49' },
      { id: 5, kind: 'edit', text: '读取 workspace/.dsh/', time: '16:48' },
    ]
  }
  const items: Array<{ id: number; kind: string; text: string; time: string }> = []
  const msgs = (allMessages.value ?? []).slice(-12)
  for (const m of msgs) {
    if (m.role === 'tool') {
      items.push({
        id: items.length + 1,
        kind: 'tool',
        text: `${m.toolName || 'tool'} ${(m.content ?? '').slice(0, 60).replace(/\s+/g, ' ')}`,
        time: new Date(m.ts || Date.now()).toLocaleTimeString().slice(0, 5),
      })
    }
  }
  return items
})

function activityIcon(kind: string) {
  if (kind === 'edit') return FileEdit
  if (kind === 'tool') return TerminalIcon
  if (kind === 'new') return FileEdit
  return FileText
}
</script>

<template>
  <div class="chat-frame">
    <!-- ===== 顶栏 ===== -->
    <div class="topbar">
      <button class="icon-btn" @click="router.push('/sessions')" title="返回会话列表">
        <ArrowLeft :size="14" />
      </button>
      <div class="title-block">
        <div class="title">
          <Pin v-if="false" :size="11" class="title-pin" />
          <span class="title-text">{{ isDemo ? 'Demo：Go Hello World' : (detail.data.value?.title || '会话详情') }}</span>
          <span v-if="isDemo" class="title-demo-tag">DEMO</span>
        </div>
        <div class="subtitle">
          <span>{{ allMessages.length }} 条 · {{ rounds }} 轮</span>
          <span v-if="!isDemo && spillConnected" class="spill-dot connected" title="Spill 已连接"></span>
          <span v-else-if="!isDemo && spillReconnects > 0" class="spill-dot reconnecting" title="Spill 重连中"></span>
        </div>
      </div>
      <button class="icon-btn" @click="detail.refetch()" title="刷新">
        <RefreshCw :size="13" />
      </button>
      <div class="export-wrap" v-if="!isDemo">
        <button class="icon-btn" @click="toggleExportMenu" title="导出">
          <Download :size="13" />
        </button>
        <div v-if="exportMenuOpen" class="export-menu" @click="closeExport">
          <button class="em-item" @click="exportMd">
            <FileText :size="11" />
            <span>导出 Markdown</span>
          </button>
          <button class="em-item" @click="exportJsonl">
            <FileText :size="11" />
            <span>导出 JSONL</span>
          </button>
        </div>
      </div>
      <button
        class="icon-btn"
        @click="sidePanelOpen = !sidePanelOpen"
        :title="sidePanelOpen ? '隐藏活动面板' : '显示活动面板'"
      >
        <component :is="sidePanelOpen ? PanelRightClose : PanelRight" :size="13" />
      </button>
    </div>

    <div class="chat-body">
      <div class="messages-col" :class="{ 'with-side': sidePanelOpen }">
        <!-- 消息流 -->
        <div ref="scrollEl" class="messages" @scroll="onScroll">
          <div v-if="!isDemo && detail.isLoading.value && allMessages.length === 0" class="empty-state">
            <div class="empty-title">加载中…</div>
          </div>

          <div v-else-if="allMessages.length === 0" class="empty-state">
            <div class="empty-logo">⚡</div>
            <div class="empty-title">开始一个对话</div>
            <div class="empty-sub">向 {{ currentModelLabel }} 提问</div>
          </div>

          <div v-else class="messages-column">
            <div
              v-for="m in allMessages"
              :key="m.id"
              :class="['msg', `msg-${m.role}`, { pending: m.pending }]"
            >
              <!-- user 消息 -->
              <template v-if="m.role === 'user'">
                <div class="user-stack">
                  <div class="bubble user-bubble">
                    {{ m.content }}
                  </div>
                  <div class="msg-meta-row meta-right">
                    <Clock :size="10" />
                    <span>{{ new Date(m.ts || Date.now()).toLocaleTimeString().slice(0, 5) }}</span>
                  </div>
                </div>
              </template>

              <!-- assistant 消息 -->
              <template v-else-if="m.role === 'assistant'">
                <div class="assistant-row">
                  <div class="assistant-avatar">
                    <Sparkles :size="14" />
                  </div>
                  <div class="assistant-body">
                    <!-- body -->
                    <div class="msg-content">
                      <MarkdownView :source="m.content || (m.pending ? '' : '')" />
                      <span v-if="m.pending" class="caret" :class="{ dim: !caretVisible }">▍</span>
                    </div>

                    <!-- tool calls（dsh 风格：details + mono 工具名）-->
                    <details
                      v-if="m.toolCalls && m.toolCalls.length"
                      class="tool-section"
                    >
                      <summary>
                        <ChevronDown :size="11" class="chev" />
                        <span>{{ m.toolCalls.length }} 个工具调用</span>
                      </summary>
                      <div class="tool-list">
                        <details v-for="tc in m.toolCalls" :key="tc.id" class="tool-item">
                          <summary>
                            <span class="tool-item-name">{{ tc.name }}</span>
                            <ChevronDown :size="10" class="chev" />
                          </summary>
                          <pre>{{ tc.args }}</pre>
                        </details>
                      </div>
                    </details>

                    <!-- file card（dsh 原生 edit/create）-->
                    <FileCard
                      v-if="m.fileCard"
                      v-bind="m.fileCard"
                    />

                    <!-- step chain（dsh 原生：think/read/edit/terminal…）-->
                    <StepChain
                      v-if="!m.pending && (m.stepsData ?? []).length"
                      :steps="m.stepsData ?? []"
                    />

                    <!-- meta row + actions（hover 才显示 actions）-->
                    <div
                      v-if="!m.pending"
                      class="msg-meta-row meta-left"
                    >
                      <span v-if="m.model" class="meta-item">
                        <Cpu :size="10" />
                        {{ m.model }}
                      </span>
                      <span v-if="m.usage?.totalTokens" class="meta-item">
                        <ArrowUp :size="9" />
                        {{ shortNum(m.usage.promptTokens) }}
                      </span>
                      <span v-if="m.usage?.completionTokens" class="meta-item">
                        <ArrowDown :size="9" />
                        {{ shortNum(m.usage.completionTokens) }}
                      </span>
                      <span
                        v-if="m.usage && m.usage.cacheReadTokens > 0"
                        class="meta-item"
                        :style="{ color: cacheColor(m.usage.cacheHitRate) }"
                      >
                        <Database :size="10" />
                        {{ Math.round(m.usage.cacheHitRate * 100) }}%
                      </span>
                      <span
                        v-if="m.usage && m.usage.reasoningTokens > 0"
                        class="meta-item"
                      >
                        <Brain :size="10" />
                        {{ shortNum(m.usage.reasoningTokens) }}
                      </span>
                      <span v-if="m.finishedAt && m.startedAt" class="meta-item">
                        <Hourglass :size="10" />
                        {{ formatTime2(m.finishedAt - m.startedAt) }}
                      </span>
                      <span class="meta-item">
                        <Clock :size="10" />
                        {{ new Date(m.ts || Date.now()).toLocaleTimeString().slice(0, 5) }}
                      </span>

                      <button
                        class="ma-btn"
                        @click="copyMessage(m)"
                        :title="copiedId === m.id ? '已复制' : '复制'"
                      >
                        <component :is="copiedId === m.id ? Check : Copy" :size="11" />
                      </button>
                      <button
                        v-if="!isDemo && m.seq != null"
                        class="ma-btn"
                        :class="{ rated: ratings[m.seq] === 1 }"
                        @click="feedbackMut.mutate({ seq: m.seq, rating: ratings[m.seq] === 1 ? 0 : 1 })"
                        :title="ratings[m.seq] === 1 ? '取消点赞' : '点赞'"
                      >
                        <ThumbsUp :size="11" />
                      </button>
                      <button
                        v-if="!isDemo && m.seq != null"
                        class="ma-btn"
                        :class="{ rated: ratings[m.seq] === -1 }"
                        @click="feedbackMut.mutate({ seq: m.seq, rating: ratings[m.seq] === -1 ? 0 : -1 })"
                        :title="ratings[m.seq] === -1 ? '取消点踩' : '点踩'"
                      >
                        <ThumbsDown :size="11" />
                      </button>
                      <button
                        v-if="!isDemo && m.seq != null"
                        class="ma-btn"
                        @click="regenerate(m.seq)"
                        title="重生成"
                      >
                        <RotateCw :size="11" />
                      </button>
                    </div>

                    <!-- status row（dsh 风格：✓ 已完成 · 用时 N 秒）-->
                    <div
                      v-if="!m.pending && m.finishedAt && m.startedAt"
                      class="status-row"
                    >
                      <CheckCircle2 :size="11" class="status-success" />
                      <span>已完成</span>
                      <span class="dot-sep"></span>
                      <span>用时 {{ formatTime2(m.finishedAt - m.startedAt) }}</span>
                    </div>

                    <!-- v8.1 P3: Token usage disclosure row（DESKTOP-FRONTEND §4.2）
                         完成 turn 含 turn/start + 每个 started model attempt 上报完整 usage 才显示。
                         点击展开 token breakdown（prompt/completion/total/cache/reasoning）。 -->
                    <details
                      v-if="!m.pending && m.usage && m.usage.totalTokens > 0"
                      class="usage-disclosure"
                    >
                      <summary>
                        <ChevronRight :size="10" class="usage-chev" />
                        <span>{{ shortNum(m.usage.totalTokens) }} tokens</span>
                        <span class="dot-sep"></span>
                        <span class="usage-stat">
                          ↑{{ shortNum(m.usage.promptTokens) }}
                        </span>
                        <span class="usage-stat">
                          ↓{{ shortNum(m.usage.completionTokens) }}
                        </span>
                        <span v-if="m.usage.cacheReadTokens" class="usage-stat">
                          ♻{{ shortNum(m.usage.cacheReadTokens) }}
                        </span>
                        <span v-if="m.usage.reasoningTokens" class="usage-stat">
                          ∴{{ shortNum(m.usage.reasoningTokens) }}
                        </span>
                      </summary>
                      <div class="usage-detail">
                        <div class="usage-row">
                          <span class="usage-label">Prompt</span>
                          <span class="usage-value">{{ m.usage.promptTokens }}</span>
                        </div>
                        <div class="usage-row">
                          <span class="usage-label">Completion</span>
                          <span class="usage-value">{{ m.usage.completionTokens }}</span>
                        </div>
                        <div class="usage-row" v-if="m.usage.cacheReadTokens">
                          <span class="usage-label">Cache read</span>
                          <span class="usage-value">{{ m.usage.cacheReadTokens }}</span>
                        </div>
                        <div class="usage-row" v-if="m.usage.cacheWriteTokens">
                          <span class="usage-label">Cache write</span>
                          <span class="usage-value">{{ m.usage.cacheWriteTokens }}</span>
                        </div>
                        <div class="usage-row" v-if="m.usage.reasoningTokens">
                          <span class="usage-label">Reasoning</span>
                          <span class="usage-value">{{ m.usage.reasoningTokens }}</span>
                        </div>
                        <div class="usage-row usage-row-total">
                          <span class="usage-label">Total</span>
                          <span class="usage-value">{{ m.usage.totalTokens }}</span>
                        </div>
                      </div>
                    </details>
                  </div>
                </div>
              </template>

              <!-- tool 消息 -->
              <template v-else-if="m.role === 'tool'">
                <div class="tool-row">
                  <div class="tool-tag">
                    <component :is="TerminalIcon" :size="11" />
                    <span>{{ m.toolName || 'tool' }}</span>
                    <span v-if="m.isError" class="tool-flag">失败</span>
                  </div>
                  <pre class="tool-content">{{ m.content }}</pre>
                </div>
              </template>

              <!-- system 消息 -->
              <template v-else>
                <div class="system-row">
                  <span>{{ m.content }}</span>
                </div>
              </template>
            </div>
          </div>

          <div v-if="!stickToBottom && sending" class="scroll-to-bottom-fab">
            <button @click="stickToBottom = true; scrollToBottom()">
              <ChevronDown :size="12" />
              回到最新
            </button>
          </div>
        </div>

        <!-- Composer（DESKTOP-FRONTEND §4.3: attachment rail + textarea + footer 三控件 + dock）-->
        <div class="composer-area">
          <!-- v8.1 P3: attachment rail（ordered rail，Escape / × 关闭） -->
          <div v-if="attachments.length" class="composer-attachments">
            <div class="att-rail">
              <span
                v-for="att in attachments"
                :key="att.id"
                class="att-chip"
                :class="['att-kind-' + att.kind]"
                :title="att.name"
              >
                <Paperclip v-if="att.kind === 'file'" :size="11" />
                <component v-else-if="att.kind === 'image'" :is="FileText" :size="11" />
                <AtSign v-else :size="11" />
                <span class="att-name truncate">{{ att.name }}</span>
                <button class="att-close" @click="removeAttachment(att.id)" title="移除">
                  <XIcon :size="9" />
                </button>
              </span>
            </div>
          </div>

          <div class="composer">
            <textarea
              ref="composerEl"
              v-model="draft"
              rows="1"
              class="composer-textarea"
              placeholder="输入消息（Enter 发送 · Shift+Enter 换行）"
              @keydown.enter.exact.prevent="send"
            />

            <!-- footer（DESKTOP-FRONTEND §4.3: + attachment / model / effort / ↑ send）-->
            <div class="composer-foot">
              <div class="foot-left">
                <!-- + attachment -->
                <button class="plus-btn" @click="addAttachment" title="添加附件">
                  <Plus :size="14" />
                </button>
                <!-- workspace pill -->
                <button class="workspace-pill" title="工作区">
                  <FolderTree :size="12" />
                  <span>工作区内修改</span>
                  <ChevronDown :size="10" />
                </button>
              </div>
              <div class="foot-right">
                <!-- model selector (provider-grouped, 大 pill) -->
                <div class="model-pill">
                  <Cpu :size="12" />
                  <select v-model="selectedModel" class="model-pill-select" title="模型">
                    <option v-for="mm in models.data.value ?? []" :key="mm.channel" :value="mm.channel">
                      {{ mm.model || mm.channel }}
                    </option>
                  </select>
                  <span class="model-speed" :class="'speed-' + modelSpeed">
                    <span class="speed-dot" />
                    <span>{{ speedLabel }}</span>
                  </span>
                  <ChevronDown :size="10" />
                </div>
                <!-- effort selector（DESKTOP-FRONTEND §8.3）-->
                <div class="effort-pill">
                  <Zap :size="11" />
                  <select v-model="selectedEffort" class="effort-select" title="推理强度">
                    <option value="low">低</option>
                    <option value="medium">中</option>
                    <option value="high">高</option>
                  </select>
                </div>
                <!-- send / stop -->
                <button
                  v-if="!sending"
                  class="send-btn"
                  :disabled="!draft.trim()"
                  @click="send"
                  title="发送 (Enter)"
                >
                  <ArrowUp :size="15" />
                </button>
                <button v-else class="stop-btn" @click="abort" title="停止">
                  <span class="stop-square"></span>
                </button>
              </div>
            </div>

            <!-- v8.1 P3: Composer dock（DESKTOP-FRONTEND §4.2）
                 activity / usage / context % ring -->
            <div class="composer-dock">
              <span class="dock-item" title="轮数">
                <GitBranch :size="10" />
                <span>{{ rounds }}</span>
              </span>
              <span class="dock-dot"></span>
              <span class="dock-item" title="步骤数">
                <ListChecks :size="10" />
                <span>{{ stepsCount }}</span>
              </span>
              <span class="dock-dot"></span>
              <span class="dock-item" title="输出速度">
                <Gauge :size="10" />
                <span>{{ displayUsage.tokPerSec ? Math.round(displayUsage.tokPerSec) : 0 }}<span class="unit"> tok/s</span></span>
              </span>
              <span class="dock-dot"></span>
              <span class="dock-item" title="总 token">
                <Zap :size="10" />
                <span>{{ shortNum(displayUsage.totalTokens) }}</span>
              </span>
              <span class="dock-dot"></span>
              <span class="dock-item" :style="{ color: cacheColor(cbCacheHitNum) }" title="缓存命中">
                <Database :size="10" />
                <span>{{ cbCacheHitPct }}%</span>
              </span>

              <!-- v8.1 P3: Context % ring button（DESKTOP-FRONTEND §4.3） -->
              <span class="dock-spacer"></span>
              <button class="dock-ring" :title="`上下文 ${contextPct}%`">
                <svg viewBox="0 0 18 18" width="16" height="16">
                  <circle cx="9" cy="9" r="7" fill="none" stroke="var(--border)" stroke-width="2" />
                  <circle
                    cx="9" cy="9" r="7" fill="none"
                    :stroke="contextColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    :stroke-dasharray="contextDash"
                    transform="rotate(-90 9 9)"
                  />
                </svg>
                <span class="dock-ring-label">{{ contextPct }}%</span>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧 Activity Side Panel -->
      <aside v-if="sidePanelOpen" class="side-panel">
        <div class="sp-header">
          <span class="sp-title">Activity</span>
          <span class="sp-count">{{ recentActivity.length }}</span>
        </div>
        <div class="sp-tabs">
          <button class="sp-tab active">Changes</button>
          <button class="sp-tab">Tools</button>
          <button class="sp-tab">Logs</button>
        </div>
        <div class="sp-list">
          <div v-for="a in recentActivity" :key="a.id" class="sp-item">
            <div class="sp-item-icon">
              <component :is="activityIcon(a.kind)" :size="11" />
            </div>
            <div class="sp-item-body">
              <div class="sp-item-text">{{ a.text }}</div>
              <div class="sp-item-meta">{{ a.time }}</div>
            </div>
            <ChevronRight :size="11" class="sp-item-chev" />
          </div>
        </div>
        <div class="sp-footer">
          <div class="sp-task">
            <ListChecks :size="11" />
            <span>{{ Math.min(4, stepsCount) }} / {{ stepsCount }} subtasks</span>
          </div>
          <div class="sp-task">
            <Activity :size="11" />
            <span>{{ sending ? '1' : '0' }} agent running</span>
          </div>
        </div>
      </aside>
    </div>
  </div>
</template>

<style scoped>
/* === dsh 原生 ChatView 风格 === */
.chat-frame {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg);
  overflow: hidden;
}
.chat-body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.messages-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--bg);
}

/* === Top bar（dsh 38px 风格）== */
.topbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 14px;
  background: var(--bg-toolbar);
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
  height: 38px;
  flex-shrink: 0;
}
.title-block {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
}
.title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.3;
  display: flex;
  align-items: center;
  gap: 5px;
}
.title-pin { color: var(--text-faint); }
.title-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.title-demo-tag {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--accent);
  background: var(--accent-soft);
  padding: 1px 4px;
  border-radius: 2px;
}
.subtitle {
  font-size: 10px;
  color: var(--text-faint);
  display: flex;
  align-items: center;
  gap: 6px;
  line-height: 1.3;
}
.spill-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--text-faint);
  display: inline-block;
}
.spill-dot.connected {
  background: var(--success);
  animation: spill-pulse 2s ease-in-out infinite;
}
.spill-dot.reconnecting { background: var(--warning); }
@keyframes spill-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  background: transparent;
  border: 1px solid transparent;
  color: var(--text-muted);
  border-radius: var(--ds-radius-sm);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

/* export dropdown */
.export-wrap {
  position: relative;
}
.export-menu {
  position: absolute;
  top: 28px;
  right: 0;
  z-index: 20;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  box-shadow: 0 4px 14px color-mix(in srgb, var(--text) 8%, transparent);
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 160px;
}
.em-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  border: none;
  color: var(--text);
  font-size: 11px;
  padding: 4px 8px;
  border-radius: var(--ds-radius-xs);
  cursor: pointer;
  text-align: left;
  font-weight: 500;
}
.em-item:hover {
  background: var(--bg-hover);
  color: var(--accent);
}

/* === Messages === */
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 14px 16px 6px;
  background: var(--bg);
  scroll-behavior: smooth;
  position: relative;
}
.messages-column {
  max-width: 748px;
  width: 100%;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-muted);
  text-align: center;
  gap: 6px;
  padding: 32px;
}
.empty-logo {
  font-size: 40px;
  opacity: 0.4;
  margin-bottom: 4px;
}
.empty-title {
  font-size: 14px;
  color: var(--text);
  font-weight: 500;
}
.empty-sub {
  font-size: 11px;
  color: var(--text-faint);
}

/* === User bubble（dsh 原生：右对齐、大圆角 20px、82% max-width）=== */
.user-stack {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  min-width: 0;
  max-width: 82%;
  margin-left: auto;
}
.user-bubble {
  max-width: 100%;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 20px;
  padding: 10px 14px;
  font-size: 13px;
  line-height: 1.55;
  color: var(--text);
  white-space: pre-wrap;
  word-break: break-word;
}

/* === Assistant row（dsh 原生：左对齐、无 bubble）=== */
.assistant-row {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  max-width: 100%;
}
.assistant-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: var(--ds-radius-sm);
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  color: var(--text-muted);
  flex-shrink: 0;
}
.assistant-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.msg-content {
  color: var(--text);
  font-size: 13px;
  line-height: 1.6;
}
.caret {
  display: inline-block;
  color: var(--accent);
  font-weight: bold;
  margin-left: 1px;
}
.caret.dim { opacity: 0.2; }

/* === Tool calls（dsh 原生：collapsed details + mono 工具名）== */
.tool-section {
  font-size: 11px;
  max-width: 100%;
}
.tool-section > summary {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--text-faint);
  padding: 2px 6px;
  border-radius: var(--ds-radius-xs);
  user-select: none;
  list-style: none;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.tool-section > summary::-webkit-details-marker { display: none; }
.tool-section > summary:hover { background: var(--bg-hover); color: var(--text-muted); }
.tool-section > summary .chev {
  transition: transform var(--ds-duration-fast) var(--ds-ease-in-out);
}
.tool-section[open] > summary .chev { transform: rotate(180deg); }
.tool-list {
  margin-top: 4px;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.tool-item {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  padding: 4px 8px;
}
.tool-item > summary {
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 500;
  color: var(--text);
  user-select: none;
  list-style: none;
  font-size: 11px;
}
.tool-item > summary::-webkit-details-marker { display: none; }
.tool-item > summary .chev {
  transition: transform var(--ds-duration-fast) var(--ds-ease-in-out);
  color: var(--text-faint);
}
.tool-item[open] > summary .chev { transform: rotate(180deg); }
.tool-item pre {
  margin: 4px 0 0;
  font-family: var(--ds-font-family-code);
  font-size: 10px;
  background: var(--bg-code);
  color: var(--text);
  padding: 6px 8px;
  border-radius: var(--ds-radius-xs);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
  border: 1px solid var(--border);
}

/* === Meta row（dsh 风格：图标 + token + 时间）== */
.msg-meta-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: var(--text-faint);
  font-variant-numeric: tabular-nums;
  flex-wrap: wrap;
}
.msg-meta-row.meta-right { justify-content: flex-end; }
.meta-item {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  color: var(--text-faint);
}
.ma-btn {
  background: transparent;
  border: 1px solid transparent;
  color: var(--text-faint);
  border-radius: var(--ds-radius-xs);
  padding: 1px 4px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: all var(--ds-duration-fast) var(--ds-ease-in-out);
  margin-left: auto;
  opacity: 0;
}
.msg-meta-row:hover .ma-btn { opacity: 1; }
.ma-btn:hover:not(:disabled) {
  border-color: var(--border);
  color: var(--text);
  background: var(--bg-elevated);
}
.ma-btn.rated {
  color: var(--accent);
  background: var(--accent-soft);
  border-color: color-mix(in srgb, var(--accent) 30%, transparent);
}

/* === Status row（dsh 风格：✓ 已完成 · 用时 N 秒）== */
.status-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: var(--text-faint);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  padding: 2px 8px;
}
.status-row .status-success { color: var(--success); }
.dot-sep {
  width: 2px;
  height: 2px;
  border-radius: 50%;
  background: var(--text-faint);
}

/* v8.1 P3: Token usage disclosure（DESKTOP-FRONTEND §4.2）*/
.usage-disclosure {
  margin-top: 6px;
  font-size: 10px;
  color: var(--text-faint);
}
.usage-disclosure > summary {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border-radius: var(--ds-radius-sm);
  background: var(--bg-card);
  border: 1px solid var(--border);
  cursor: pointer;
  list-style: none;
  user-select: none;
  font-variant-numeric: tabular-nums;
  transition:
    background var(--ds-duration-fast) var(--ds-ease-in-out),
    border-color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.usage-disclosure > summary::-webkit-details-marker { display: none; }
.usage-disclosure > summary:hover {
  background: var(--bg-elevated);
  border-color: color-mix(in srgb, var(--accent) 30%, var(--border));
}
.usage-chev {
  transition: transform var(--ds-duration-fast) var(--ds-ease-out);
}
.usage-disclosure[open] > summary .usage-chev { transform: rotate(90deg); }
.usage-stat {
  display: inline-flex;
  align-items: center;
  font-weight: 500;
  color: var(--text-muted);
  letter-spacing: 0.02em;
}
.usage-detail {
  margin-top: 6px;
  margin-left: 4px;
  padding: 8px 10px;
  border-radius: var(--ds-radius-sm);
  background: var(--bg-card);
  border: 1px solid var(--border);
  display: grid;
  grid-template-columns: 1fr;
  gap: 3px;
  font-variant-numeric: tabular-nums;
  min-width: 220px;
  max-width: 320px;
}
.usage-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.usage-row .usage-label {
  color: var(--text-faint);
  font-weight: 500;
}
.usage-row .usage-value {
  color: var(--text);
  font-weight: 600;
}
.usage-row-total {
  border-top: 1px solid var(--border);
  padding-top: 4px;
  margin-top: 2px;
}

/* === Tool row（独立 tool 消息）== */
.tool-row {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-left: 2px solid var(--warning);
  border-radius: var(--ds-radius-sm);
  padding: 6px 10px;
  max-width: 82%;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.tool-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.tool-flag {
  background: var(--danger-bg);
  color: var(--danger);
  padding: 0 4px;
  border-radius: 2px;
}
.tool-content {
  margin: 0;
  font-family: var(--ds-font-family-code);
  font-size: 10px;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text);
  max-height: 180px;
  overflow: auto;
}

/* === System row === */
.system-row {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  padding: 4px 10px;
  font-size: 11px;
  color: var(--text-faint);
  text-align: center;
  max-width: 100%;
  align-self: center;
}

/* === Scroll FAB === */
.scroll-to-bottom-fab {
  position: absolute;
  bottom: 150px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 5;
}
.scroll-to-bottom-fab button {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow);
  cursor: pointer;
  font-size: 11px;
  color: var(--text);
}
.scroll-to-bottom-fab button:hover {
  border-color: var(--accent);
  color: var(--accent);
}

/* === Composer area === */
.composer-area {
  padding: 6px 16px 12px;
  background: var(--bg);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: center;
}

/* v8.1 P3: Composer attachment rail（DESKTOP-FRONTEND §4.3 draft attachment rail） */
.composer-attachments {
  max-width: 748px;
  width: 100%;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 0;
}
.att-rail {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  width: 100%;
  padding: 0;
}
.att-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px 4px 6px;
  border-radius: 10px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  font-size: 11px;
  color: var(--text);
  max-width: 220px;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.att-chip:hover { background: var(--bg-card); }
.att-chip.att-kind-image { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }
.att-chip.att-kind-reference { background: var(--bg-card); }
.att-name { max-width: 140px; font-weight: 500; }
.att-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border-radius: 4px;
  border: none;
  background: transparent;
  color: var(--text-faint);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.att-close:hover { background: var(--bg-hover); color: var(--text); }

/* === Composer card === */
.composer {
  max-width: 748px;
  width: 100%;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 16px;
  transition: border-color var(--ds-duration-fast) var(--ds-ease-in-out),
    box-shadow var(--ds-duration-fast) var(--ds-ease-in-out);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.composer:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 20%, transparent);
}

.composer-textarea {
  border: none;
  background: transparent;
  padding: 10px 16px 4px;
  font-size: 14px;
  line-height: 1.5;
  outline: none;
  max-height: 220px;
  overflow-y: auto;
  resize: none;
  width: 100%;
  color: var(--text);
  font-family: inherit;
}
.composer-textarea::placeholder {
  color: var(--text-faint);
  font-size: 13px;
}

.composer-mid {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 2px 12px 4px;
}
.mid-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: var(--ds-radius-xs);
  background: transparent;
  border: none;
  color: var(--text-faint);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.mid-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

.composer-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px 6px;
  gap: 6px;
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
}
.foot-left {
  display: flex;
  align-items: center;
  gap: 4px;
}
.foot-right {
  display: flex;
  align-items: center;
  gap: 4px;
}
.plus-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: var(--ds-radius-sm);
  background: transparent;
  border: 1px solid var(--border);
  color: var(--text-muted);
  cursor: pointer;
}
.plus-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
  border-color: var(--border-strong);
}

.workspace-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  color: var(--text-muted);
  cursor: pointer;
  height: 26px;
  font-size: 11px;
  font-weight: 500;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    border-color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.workspace-pill:hover {
  background: var(--bg-hover);
  border-color: var(--border-strong);
  color: var(--text);
}

.model-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  color: var(--text);
  cursor: pointer;
  height: 26px;
  font-size: 11px;
}
.model-pill:hover {
  background: var(--bg-hover);
  border-color: var(--border-strong);
}
.model-pill-select {
  border: none;
  background: transparent;
  outline: none;
  color: var(--text);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  padding: 0;
  max-width: 120px;
  text-overflow: ellipsis;
  font-family: inherit;
}
.model-speed {
  font-size: 9px;
  color: var(--text-faint);
  text-transform: uppercase;
  font-weight: 600;
  letter-spacing: 0.04em;
  padding: 0 4px;
  border-left: 1px solid var(--border);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.speed-dot {
  display: inline-block;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--text-faint);
}
.speed-low .speed-dot { background: var(--text-faint); }
.speed-medium .speed-dot { background: var(--warning); }
.speed-high .speed-dot { background: var(--success); }

/* v8.1 P3: Effort pill (DESKTOP-FRONTEND §8.3) */
.effort-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  height: 26px;
  font-size: 11px;
  color: var(--text);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.effort-pill:hover { background: var(--bg-hover); border-color: var(--border-strong); }
.effort-select {
  border: none;
  background: transparent;
  outline: none;
  color: var(--text);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  padding: 0;
  font-family: inherit;
}

.send-btn,
.stop-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    opacity var(--ds-duration-fast) var(--ds-ease-in-out);
}
.send-btn {
  width: 30px;
  height: 30px;
}
.send-btn:hover:not(:disabled) { background: var(--accent-hover); }
.send-btn:disabled {
  background: var(--bg-active);
  color: var(--text-faint);
  cursor: not-allowed;
}
.stop-btn {
  width: 30px;
  height: 30px;
  background: var(--danger);
}
.stop-btn:hover { background: var(--danger); opacity: 0.9; }
.stop-square {
  width: 11px;
  height: 11px;
  background: #fff;
  border-radius: 2px;
}

/* === Composer dock（DESKTOP-FRONTEND §4.2: activity / usage / context % ring）== */
.composer-dock {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px 8px;
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  font-size: 10px;
  color: var(--text-faint);
  font-variant-numeric: tabular-nums;
  flex-wrap: wrap;
}
.dock-item {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  transition: color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.dock-item:hover { color: var(--text-muted); }
.dock-item .unit { opacity: 0.7; font-size: 9px; margin-left: 1px; }
.dock-dot {
  width: 2px;
  height: 2px;
  border-radius: 50%;
  background: var(--text-faint);
  opacity: 0.6;
}
.dock-spacer { flex: 1; }
.dock-ring {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px 2px 4px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  font-family: inherit;
  font-size: 10px;
  color: var(--text-muted);
  cursor: pointer;
  transition:
    border-color var(--ds-duration-fast) var(--ds-ease-in-out),
    background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.dock-ring:hover {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent);
}
.dock-ring-label { font-weight: 600; font-variant-numeric: tabular-nums; }

/* === Side Panel === */
.side-panel {
  width: 240px;
  background: var(--bg-card);
  border-left: 1px solid var(--dsw-alias-border-l1, transparent);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  overflow: hidden;
}
.sp-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
}
.sp-title {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text);
}
.sp-count {
  font-size: 10px;
  color: var(--text-faint);
  background: var(--bg-elevated);
  padding: 1px 6px;
  border-radius: 8px;
}
.sp-tabs {
  display: flex;
  align-items: center;
  gap: 1px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
}
.sp-tab {
  padding: 3px 8px;
  font-size: 10px;
  color: var(--text-faint);
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--ds-radius-xs);
  cursor: pointer;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.sp-tab:hover { background: var(--bg-hover); color: var(--text-muted); }
.sp-tab.active {
  background: var(--bg-elevated);
  color: var(--text);
  border-color: var(--border);
}
.sp-list {
  flex: 1;
  overflow-y: auto;
  padding: 4px 6px;
}
.sp-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--ds-radius-sm);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.sp-item:hover { background: var(--bg-hover); }
.sp-item-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 3px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  color: var(--text-faint);
  flex-shrink: 0;
  margin-top: 1px;
}
.sp-item-body { flex: 1; min-width: 0; }
.sp-item-text {
  font-size: 11px;
  color: var(--text);
  line-height: 1.4;
  word-wrap: break-word;
}
.sp-item-meta {
  font-size: 9px;
  color: var(--text-faint);
  margin-top: 2px;
}
.sp-item-chev {
  color: var(--text-faint);
  flex-shrink: 0;
  margin-top: 3px;
}
.sp-footer {
  padding: 8px 12px;
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.sp-task {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  color: var(--text-faint);
}
</style>