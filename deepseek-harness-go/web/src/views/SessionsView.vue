<script setup lang="ts">
// SessionsView.vue — dsh 桌面端 session list
//
// 设计参考 dsh 原生 sidebar 的 session list 风格：
//   - 列表式（不卡片式）：每个 session 一行，紧凑
//   - active session 高亮
//   - 标题 + 预览 + meta

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Plus, Trash2, MessageSquare, Search } from 'lucide-vue-next'
import { sessionsApi } from '@/api/sessions'
import { formatRelative } from '@/utils/format'
import { useI18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'

const router = useRouter()
const route = useRoute()
const qc = useQueryClient()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

const keyword = ref('')

// v8.1 P3: 新建会话的"命名"已不再是必填步骤。命名由后续 LLM 回答后
// 自动改写 title（后端 Projector 已经会在收到 user 消息后更新 preview/title）。
// 这里只需点"+ New session" → 立即创建 → 跳进会话。

// ?new=1 → 自动创建并跳进去（左栏"New session"按钮 / App.vue 的入口）
watch(
  () => route.query.new,
  (v) => {
    if (v === '1' || v === 'true') {
      // 立即创建并跳到详情（由 current 工作区 bind）
      createMut.mutate(undefined as unknown as void)
      // 清掉 query，避免刷新时再次触发
      const q = { ...route.query }
      delete q.new
      router.replace({ path: '/sessions', query: q })
    }
  },
  { immediate: true },
)

const list = useQuery({
  queryKey: ['sessions'],
  queryFn: () => sessionsApi.list(50),
  refetchInterval: 5000,
})

// v8.1 P3: 新建会话不需要选 model — 由后端默认 channel 处理。
// 因此不再使用 models 列表。

// 按工作区分组：每个 session 用 workspaceStore.getSessionWorkspace(sid)
// 决定所属工作区；缺省归到 DEFAULT_ID。
const groupedByWorkspace = computed(() => {
  const items = list.data.value?.items ?? []
  const groups = new Map<string, typeof items>()
  for (const it of items) {
    const w = workspaceStore.getSessionWorkspace(it.sid)
    if (!groups.has(w)) groups.set(w, [])
    groups.get(w)!.push(it)
  }
  // 按 workspace 列表顺序输出（默认工作区先）
  return workspaceStore.workspaces.map((w) => ({
    workspace: w,
    items: groups.get(w.id) || [],
  }))
})

// v8.1 P3: 每个 workspace 默认展示前 N 条（DESKTOP-FRONTEND §3.4 默认 5 条 + Show more 每次 +5）
const DEFAULT_SHOW = 5
const expanded = ref<Set<string>>(new Set())
function isExpanded(wid: string) { return expanded.value.has(wid) }
function toggleShow(wid: string) {
  if (expanded.value.has(wid)) expanded.value.delete(wid)
  else expanded.value.add(wid)
  // 触发响应式
  expanded.value = new Set(expanded.value)
}

// 用于头部计数（不分组）
const totalCount = computed(() => {
  return (list.data.value?.items ?? []).length
})

// 搜索过滤（跨工作区）
const filteredGroups = computed(() => {
  if (!keyword.value.trim()) return groupedByWorkspace.value
  const k = keyword.value.toLowerCase()
  return groupedByWorkspace.value
    .map((g) => ({
      ...g,
      items: g.items.filter(
        (s) =>
          (s.title || '').toLowerCase().includes(k) ||
          (s.preview || '').toLowerCase().includes(k) ||
          (s.model || '').toLowerCase().includes(k),
      ),
    }))
    .filter((g) => g.items.length > 0)
})

const createMut = useMutation({
  mutationFn: async () => {
    // v8.1 P3: 不需要 title；让后端默认给"新会话"
    const s = await sessionsApi.create('', '')
    // 标记为当前工作区
    workspaceStore.bind(s.sid)
    return s
  },
  onSuccess: (session) => {
    qc.invalidateQueries({ queryKey: ['sessions'] })
    router.push(`/sessions/${encodeURIComponent(session.sid)}`)
  },
})

const removeMut = useMutation({
  mutationFn: (sid: string) => sessionsApi.remove(sid),
  onSuccess: (_data, sid) => {
    workspaceStore.unbind(sid)
    qc.invalidateQueries({ queryKey: ['sessions'] })
  },
})

function open(sid: string) {
  router.push(`/sessions/${encodeURIComponent(sid)}`)
}

function handleDelete(sid: string, ev: Event) {
  ev.stopPropagation()
  if (window.confirm('删除会话？')) removeMut.mutate(sid)
}
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">{{ t('sessions.title') }}</h1>
        <span class="page-count">{{ totalCount }}</span>
      </div>
      <div class="ph-right">
        <!-- v8.1 P3: 新建会话不需弹窗，直接点击创建并跳进会话 -->
        <button
          class="btn btn-primary"
          :disabled="createMut.isPending.value"
          @click="createMut.mutate()"
        >
          <Plus :size="13" />
          {{ createMut.isPending.value ? '创建中…' : t('sessions.newSession') }}
        </button>
      </div>
    </header>

    <div v-if="createMut.error.value" class="error-banner m-3">
      {{ (createMut.error.value as Error).message }}
    </div>

    <div v-if="list.error.value" class="error-banner m-3">
      {{ (list.error.value as Error).message }}
    </div>

    <div class="search-row">
      <Search :size="13" class="text-faint" />
      <input
        v-model="keyword"
        type="search"
        :placeholder="t('sessions.filter')"
      />
    </div>

    <div class="content-body">
      <div v-if="list.isLoading.value" class="empty">加载中…</div>
      <div v-else-if="totalCount === 0" class="empty">
        <MessageSquare :size="28" class="empty-icon" />
        <div class="empty-title">{{ t('sessions.empty') }}</div>
        <div class="empty-sub">点击右上角「新建会话」开始</div>
      </div>

      <div v-else class="session-list">
        <!-- v8.1 P3: 按工作区分组展示（DESKTOP-FRONTEND §3.4：每组默认 5 + Show more）-->
        <section
          v-for="g in filteredGroups"
          :key="g.workspace.id"
          class="ws-group"
        >
          <header class="ws-group-header">
            <span class="ws-name">{{ g.workspace.name }}</span>
            <span class="ws-count">{{ g.items.length }}</span>
          </header>
          <div
            v-for="s in (isExpanded(g.workspace.id) ? g.items : g.items.slice(0, DEFAULT_SHOW))"
            :key="s.sid"
            class="session-item"
            @click="open(s.sid)"
          >
            <div class="session-main">
              <div class="session-title truncate">
                {{ s.title || '未命名' }}
              </div>
              <div class="session-preview">
                {{ s.preview || '（暂无内容）' }}
              </div>
              <div class="session-meta">
                <span class="tag tag-mono">{{ s.model || 'default' }}</span>
                <span>{{ s.rounds }} 轮</span>
                <span>{{ formatRelative(s.updatedAt) }}</span>
              </div>
            </div>
            <button
              class="del-btn"
              @click="handleDelete(s.sid, $event)"
              title="删除"
            >
              <Trash2 :size="12" />
            </button>
          </div>
          <!-- v8.1 P3: Show more button (DESKTOP-FRONTEND §3.4) -->
          <button
            v-if="!isExpanded(g.workspace.id) && g.items.length > DEFAULT_SHOW"
            class="show-more-btn"
            @click="toggleShow(g.workspace.id)"
          >
            <Plus :size="11" />
            <span>显示更多（{{ g.items.length - DEFAULT_SHOW }}）</span>
          </button>
          <button
            v-else-if="isExpanded(g.workspace.id) && g.items.length > DEFAULT_SHOW"
            class="show-more-btn"
            @click="toggleShow(g.workspace.id)"
          >
            <span>收起</span>
          </button>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: transparent;
}

/* === Page header === */
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
  background: var(--bg-toolbar);
  flex-shrink: 0;
  gap: 12px;
}
.ph-left { display: flex; align-items: baseline; gap: 8px; }
.page-title { font-size: 13px; font-weight: 600; margin: 0; color: var(--text); }
.page-count {
  font-size: 10px;
  color: var(--text-faint);
  background: var(--bg-elevated);
  padding: 1px 6px;
  border-radius: 8px;
  font-weight: 600;
}
.ph-right { display: flex; align-items: center; gap: 6px; }

/* === New panel (inline) === */
.new-panel {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  background: var(--bg-card);
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
}
.new-title { flex: 1; }
.new-model { width: 200px; }
.m-3 { margin: 12px; }
.mt-2 { margin-top: 8px; }

/* === Search === */
.search-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
}
.search-row input {
  border: none;
  background: transparent;
  padding: 0;
  font-size: 12px;
  color: var(--text);
  outline: none;
  flex: 1;
  box-shadow: none;
}
.search-row input:focus { box-shadow: none; }

/* === List === */
.content-body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 8px 16px;
}
.session-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-width: 880px;
  margin: 0 auto;
}
/* v8.1 P3: 工作区分组样式 */
.ws-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ws-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-faint);
  user-select: none;
}
.ws-name { flex: 1; }
.ws-count {
  font-size: 10px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  padding: 1px 6px;
  border-radius: var(--ds-radius-xs);
  font-weight: 500;
  letter-spacing: 0;
}
.session-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  cursor: pointer;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.05);
  transition:
    background var(--ds-duration-fast) var(--ds-ease-in-out),
    transform var(--ds-duration-fast) var(--ds-ease-out),
    border-color var(--ds-duration-fast) var(--ds-ease-in-out);
  position: relative;
}
[data-theme="light"] .session-item {
  background: rgba(255, 255, 255, 0.55);
  border-color: rgba(0, 0, 0, 0.06);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}
.session-item:hover {
  background: rgba(110, 89, 255, 0.10);
  border-color: rgba(110, 89, 255, 0.35);
  transform: translateY(-1px);
}
.session-main { flex: 1; min-width: 0; }
.session-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--text);
  margin-bottom: 2px;
}
.session-preview {
  font-size: 11px;
  color: var(--text-faint);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-bottom: 4px;
}
.session-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: var(--text-faint);
  flex-wrap: wrap;
}
.tag {
  display: inline-block;
  padding: 1px 5px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 3px;
  font-size: 10px;
  color: var(--text-muted);
}
.tag-mono { font-family: var(--ds-font-family-code); }
.del-btn {
  background: transparent;
  border: none;
  color: var(--text-faint);
  padding: 4px;
  border-radius: var(--ds-radius-xs);
  cursor: pointer;
  opacity: 0;
  transition: opacity var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
  flex-shrink: 0;
}
.session-item:hover .del-btn { opacity: 1; }
.del-btn:hover { color: var(--danger); background: var(--danger-bg); }

/* v8.1 P3: Show more button (DESKTOP-FRONTEND §3.4) */
.show-more-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin: 2px auto 0;
  padding: 4px 12px;
  border-radius: var(--ds-radius-sm);
  border: 1px dashed var(--border);
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition:
    border-color var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out),
    background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.show-more-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-soft);
  border-style: solid;
}
</style>
