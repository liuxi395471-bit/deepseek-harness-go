<script setup lang="ts">
/**
 * 侧边栏 —— ChatGPT 风格的会话导航。
 *
 * 包含：品牌区、新建对话按钮、会话搜索、会话列表、底部用户区。
 */
import { computed, ref } from 'vue'
import {
  ChevronDown,
  LogOut,
  MessageSquare,
  Plus,
  Search,
  Trash2,
} from 'lucide-vue-next'
import {
  currentSessionId,
  deleteSession,
  newConversation,
  selectSession,
  sessions,
  sessionsError,
  sessionsLoading,
} from '@/composables/useChat'

const emit = defineEmits<{
  (e: 'logout'): void
  (e: 'close'): void
}>()

const search = ref('')
const showSearch = ref(false)

/** 按搜索词过滤会话列表。 */
const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return sessions.value
  return sessions.value.filter((s) => {
    return (
      s.preview?.toLowerCase().includes(q) || s.id.toLowerCase().includes(q)
    )
  })
})

/** 把时间戳格式化为相对时间（ChatGPT 风格的分组标签）。 */
function formatWhen(iso: string): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const diff = Date.now() - t
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const hour = Math.floor(min / 60)
  if (hour < 24) return `${hour} 小时前`
  const day = Math.floor(hour / 24)
  if (day < 7) return `${day} 天前`
  const d = new Date(t)
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

function onSelect(id: string): void {
  void selectSession(id)
}

function onDelete(id: string): void {
  void deleteSession(id)
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar__head">
      <div class="brand">
        <span class="brand__mark" aria-hidden="true"></span>
        <span class="brand__name">DeepSeek Harness</span>
      </div>
      <button
        class="icon-btn icon-btn--ghost"
        title="收起侧边栏"
        aria-label="收起侧边栏"
        @click="emit('close')"
      >
        <ChevronDown :size="18" class="sidebar__collapse-icon" />
      </button>
    </div>

    <div class="sidebar__actions">
      <button class="new-chat" @click="newConversation()">
        <Plus :size="17" />
        <span>新建对话</span>
      </button>
      <button
        class="icon-btn icon-btn--ghost"
        title="搜索会话"
        aria-label="搜索会话"
        @click="showSearch = !showSearch"
      >
        <Search :size="17" />
      </button>
    </div>

    <div v-if="showSearch" class="sidebar__search">
      <input
        v-model="search"
        class="search-input"
        type="search"
        placeholder="搜索会话…"
        aria-label="搜索会话"
      />
    </div>

    <nav class="sidebar__list" aria-label="会话列表">
      <p v-if="sessionsLoading" class="sidebar__hint">加载中…</p>
      <p v-else-if="sessionsError" class="sidebar__hint sidebar__hint--error">
        {{ sessionsError }}
      </p>
      <p v-else-if="filtered.length === 0" class="sidebar__hint">
        {{ search ? '没有匹配的会话' : '还没有会话' }}
      </p>

      <ul v-else class="session-list">
        <li v-for="s in filtered" :key="s.id">
          <button
            class="session-item"
            :class="{ 'session-item--active': s.id === currentSessionId }"
            @click="onSelect(s.id)"
          >
            <MessageSquare :size="15" class="session-item__icon" />
            <span class="session-item__text">
              <span class="session-item__title">{{ s.preview || '（空会话）' }}</span>
              <span class="session-item__meta">{{ formatWhen(s.updated_at) }}</span>
            </span>
            <span
              class="session-item__delete"
              role="button"
              tabindex="0"
              title="从列表移除"
              aria-label="从列表移除"
              @click.stop="onDelete(s.id)"
              @keydown.enter.stop="onDelete(s.id)"
            >
              <Trash2 :size="14" />
            </span>
          </button>
        </li>
      </ul>
    </nav>

    <div class="sidebar__foot">
      <div class="user-chip">
        <span class="user-chip__avatar" aria-hidden="true">DS</span>
        <span class="user-chip__name">本地用户</span>
      </div>
      <button
        class="icon-btn icon-btn--ghost"
        title="退出登录"
        aria-label="退出登录"
        @click="emit('logout')"
      >
        <LogOut :size="17" />
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: var(--sidebar-width);
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-subtle);
  flex-shrink: 0;
}

.sidebar__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 12px 8px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}

.brand__mark {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: linear-gradient(135deg, var(--accent), var(--accent-hover));
  flex-shrink: 0;
}

.brand__name {
  font-weight: 600;
  font-size: 14.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 收起侧边栏时把箭头旋转成"展开"语义 */
.sidebar__collapse-icon {
  transform: rotate(90deg);
}

.sidebar__actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px 10px;
}

.new-chat {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 9px 12px;
  border-radius: var(--radius-md);
  background: var(--bg-elevated);
  border: 1px solid var(--border-subtle);
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
  transition: background var(--transition), border-color var(--transition);
}

.new-chat:hover {
  background: var(--bg-hover);
  border-color: var(--border-strong);
}

.sidebar__search {
  padding: 0 12px 8px;
}

.search-input {
  width: 100%;
  padding: 8px 11px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-subtle);
  background: var(--bg-input);
  font-size: 13.5px;
  outline: none;
  transition: border-color var(--transition);
}

.search-input:focus {
  border-color: var(--accent);
}

.sidebar__list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px;
}

.sidebar__hint {
  margin: 12px 8px;
  font-size: 13px;
  color: var(--text-tertiary);
}

.sidebar__hint--error {
  color: var(--danger);
}

.session-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.session-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 9px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  text-align: left;
  transition: background var(--transition), color var(--transition);
}

.session-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.session-item--active {
  background: var(--bg-active);
  color: var(--text-primary);
  font-weight: 500;
}

.session-item__icon {
  flex-shrink: 0;
  opacity: 0.65;
}

.session-item__text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.session-item__title {
  font-size: 13.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item__meta {
  font-size: 11.5px;
  color: var(--text-tertiary);
}

.session-item__delete {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 5px;
  opacity: 0;
  flex-shrink: 0;
  transition: opacity var(--transition), background var(--transition);
}

.session-item:hover .session-item__delete {
  opacity: 0.7;
}

.session-item__delete:hover {
  opacity: 1;
  background: var(--danger-soft);
  color: var(--danger);
}

.sidebar__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid var(--border-subtle);
}

.user-chip {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}

.user-chip__avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--bg-active);
  color: var(--text-secondary);
  font-size: 10.5px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.user-chip__name {
  font-size: 13px;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
