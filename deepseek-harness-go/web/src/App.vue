<script setup lang="ts">
// App.vue — dsh-go 控制台顶层（dsh 桌面端真实视觉）
//
// 布局（按 dsh 原生 web AppFrame）：
//
//   ┌──────┬──────────────────────────────────────────────────────┐
//   │ Side │  Topbar（顶栏）                                       │
//   │ bar  ├──────────────────────────────────────────────────────┤
//   │      │                                                      │
//   │      │  Content（router-view，flex 1）                      │
//   │      │                                                      │
//   │      │                                                      │
//   │  ⊙   │                                                      │  ← footer 状态条（桌面模式）
//   └──────┴──────────────────────────────────────────────────────┘
//
// Sidebar（dsh 原生风格）：
//   - 顶部 brand（仅展开状态可见）
//   - 「+ New session」大按钮
//   - 区域：RECENT / WORKSPACE / APPROVALS / SCHEDULES / WEBHOOKS / TASKS / PLUGINS / MODELS
//   - 底部 settings / user 座位（永久可见）
//   - 折叠状态：rail 模式（仅图标）

import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { useUserStore } from '@/stores/user'
import { useUIStore } from '@/stores/ui'
import { useWorkspaceStore } from '@/stores/workspace'
import { bootApp } from '@/stores/boot'
import { bindRouter, client } from '@/api/client'
import { modelsApi } from '@/api/models'
import { fetchMe, logout as apiLogout } from '@/api/auth'
import { useI18n } from '@/i18n'
import {
  MessageSquare,
  LogOut,
  Monitor,
  Moon,
  Sun,
  RefreshCw,
  Languages,
  User as UserIcon,
  Plus,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Sparkles,
  Zap,
  CircleDot,
  Briefcase,
  Plus as PlusIcon,
  Pencil,
  Trash2 as TrashIcon,
  Check,
  X as XIcon,
  FlaskConical,
  Atom,
  Bot,
  Code2,
  Beaker,
  BookOpen,
} from 'lucide-vue-next'
import ToastStack from '@/components/ToastStack.vue'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const uiStore = useUIStore()
const workspaceStore = useWorkspaceStore()
const { locale, setLocale, t } = useI18n()

bindRouter(router)

const collapsed = ref(false)
const healthState = ref<'unknown' | 'ok' | 'failed'>('unknown')
const buildVersion = ref<string>('v8.1.0')
const buildStamp = ref<string>('')

const isDesktop = ref(false)
const desktopInfo = ref<{ version?: string; pid?: string } | null>(null)

interface NavItem {
  to: string
  label: string
  icon: any
  badge?: string
}

const navItems = computed<NavItem[]>(() => [
  // v8.1 P2: 顶层 navItems 只保留 sessions。其它入口（plugins / models / tasks /
  // approvals / schedules / webhooks）是 admin 控制台的内容，不在 chat workspace
  // 里展示，避免左栏冗余（dsh 原生 chat 工作区 = session list only）。
  { to: '/sessions', label: t('nav.sessions'), icon: MessageSquare },
])

async function fetchVersion() {
  try {
    const { data } = await client.get<{ version: string }>('/health')
    if (data?.version && data.version !== 'dev') {
      buildVersion.value = 'v' + data.version
    }
  } catch {
    /* ignore */
  }
  try {
    const r = await fetch('/api/v1/console/state?key=buildStamp', {
      headers: userStore.token ? { Authorization: `Bearer ${userStore.token}` } : {},
    })
    if (r.ok) {
      const j = await r.json()
      // v8.1 P3: build code badge（DESKTOP-FRONTEND §3.1，version[-commit][-dirty]）
      // 短码 = commit 前 7 字符 + dirty 标识
      const raw = String(j?.value || '').trim()
      if (raw) {
        const m = raw.match(/^([0-9a-f]{7,40})(-dirty)?$/i)
        if (m) {
          buildStamp.value = (m[1].slice(0, 7)) + (m[2] ? '*' : '')
        } else {
          buildStamp.value = raw
        }
      }
    }
  } catch {
    /* ignore */
  }
}

onMounted(async () => {
  // v8.1 P4: bootApp() 已在 main.ts app mount 前完成（NoAuth 自动 stub login）
  const params = new URLSearchParams(window.location.search)
  isDesktop.value = params.get('from') === 'desktop'
  const urlToken = params.get('token')
  if (urlToken) {
    userStore.setSession(
      urlToken,
      userStore.user || { id: 0, username: 'desktop', role: 'admin', createdAt: '' },
      '',
    )
    params.delete('token')
    const qs = params.toString()
    const newUrl = window.location.pathname + (qs ? '?' + qs : '') + window.location.hash
    window.history.replaceState({}, '', newUrl)
  }
  if (userStore.token) {
    try {
      await fetchMe()
    } catch {
      /* interceptor will handle 401 */
    }
  }
  await fetchVersion()
  await pingHealth()
  if (isDesktop.value) {
    fetch('/api/v1/console/state?key=desktop', {
      headers: { Authorization: `Bearer ${userStore.token}` },
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d?.value) {
          try {
            desktopInfo.value = JSON.parse(d.value)
          } catch {
            /* ignore */
          }
        }
      })
      .catch(() => {})
  }
})

async function pingHealth() {
  try {
    await client.get('/health')
    healthState.value = 'ok'
  } catch {
    healthState.value = 'failed'
  }
}

async function logout() {
  await apiLogout()
  healthState.value = 'unknown'
  router.push('/login')
}

function changeLocale() {
  setLocale(locale.value === 'zh-CN' ? 'en-US' : 'zh-CN')
}

function newSession() {
  // v8.1 P3: 新建会话直接走 API，落入当前工作区（由 SessionsView
  // 监听 sid 变化显示）。这样左栏"+ New session"按钮不需要再
  // 跳到 ?new=1 弹窗。
  // 为保留 SessionsView 里"命名"弹窗的可选性，这里仍走 sessions 页
  // 但用 query 标记；SessionsView 会读取并弹窗。
  router.push('/sessions?new=1')
}

// v8.1 P3: 工作区管理 UI 状态
const wsEditing = ref(false)
const wsNewName = ref('')
const wsRenameId = ref<string | null>(null)
const wsRenameValue = ref('')
const wsMenuOpen = ref(false)

// v8.1 P3: 顶部 model selector + tabs（dsh 原生风格）
const models = useQuery({
  queryKey: ['models'],
  queryFn: modelsApi.list,
  refetchInterval: 30_000,
})
const modelMenuOpen = ref(false)
const currentChannelId = ref('default')
const currentModelLabel = computed(() => {
  const m = (models.data.value ?? []).find((x) => x.channel === currentChannelId.value)
  return m?.model || 'deepseek-flash'
})
const currentChannelLabel = computed(() => currentChannelId.value)

// 顶部 tabs（dsh 原生风格：DeepResearch / DR / T4 / ...）
const primaryTabs = [
  { id: 'chat', label: 'Chat', icon: MessageSquare },
  { id: 'research', label: 'DeepResearch', icon: Beaker },
  { id: 'r1', label: 'R1', icon: Atom },
  { id: 't4', label: 'T4', icon: FlaskConical },
  { id: 'agent', label: 'Agent', icon: Bot },
  { id: 'code', label: 'Code', icon: Code2 },
  { id: 'docs', label: 'Docs', icon: BookOpen },
]
const activeTab = ref('chat')
const isSessionsDetail = computed(() => /^\/sessions\/[^/?]+/.test(route.path))

function setActiveTab(id: string) {
  activeTab.value = id
  // 简化路由：tabs 暂都跳到 sessions（用户主页面），后续可扩展
  if (route.path !== '/sessions') router.push('/sessions')
}

function onDocClickTabs(e: MouseEvent) {
  if (!modelMenuOpen.value) return
  const t = e.target as HTMLElement | null
  if (t && t.closest && t.closest('.model-selector')) return
  modelMenuOpen.value = false
}

function wsAddNew() {
  const name = (wsNewName.value || '').trim() || '新工作区'
  workspaceStore.add(name)
  wsNewName.value = ''
  wsEditing.value = false
}

function wsStartRename(id: string) {
  const w = workspaceStore.workspaces.find((x) => x.id === id)
  if (!w) return
  wsRenameId.value = id
  wsRenameValue.value = w.name
}

function wsApplyRename() {
  if (wsRenameId.value) {
    workspaceStore.rename(wsRenameId.value, wsRenameValue.value)
  }
  wsRenameId.value = null
  wsRenameValue.value = ''
}

function wsDelete(id: string) {
  if (id === 'default') return
  if (!window.confirm('删除该工作区？属于该工作区的会话会归到默认工作区。')) return
  workspaceStore.remove(id)
}

// v8.1 P3: 点击外部关闭工作区菜单 + model 菜单
function onDocClick(e: MouseEvent) {
  if (!wsMenuOpen.value && !modelMenuOpen.value) return
  const t = e.target as HTMLElement | null
  if (t && t.closest && t.closest('.ws-picker, .ws-picker-rail, .sb-workspace, .sb-workspace-rail, .model-selector, .ws-menu-overlay')) return
  wsMenuOpen.value = false
  modelMenuOpen.value = false
}

// v8.1 P3: Ctrl+N / Mod+N 快捷键（DESKTOP-FRONTEND §3.2）
function onShortcut(e: KeyboardEvent) {
  // 当焦点在输入控件 / textarea 时不拦截
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) {
    return
  }
  const mod = e.ctrlKey || e.metaKey
  if (mod && !e.shiftKey && !e.altKey && (e.key === 'n' || e.key === 'N')) {
    e.preventDefault()
    newSession()
  }
}
onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onShortcut)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onShortcut)
})

const connectionLabel = computed(() => {
  if (healthState.value === 'ok') return t('common.connected')
  if (healthState.value === 'failed') return t('common.authFailed')
  return t('common.disconnected')
})

// v8.1 P3: app-frame class binding
const appFrameClasses = computed(() => ({
  'sidebar-collapsed': collapsed,
}))
</script>

<template>
  <div class="app-frame" :class="appFrameClasses">
    <!-- === Sidebar（dsh 原生风格） === -->
    <aside class="sidebar">
      <!-- 顶部 brand 区 + 折叠按钮（dsh 原生风格）-->
      <div class="sb-top">
        <div class="brand" :title="'DeepSeek Harness' + (buildStamp ? ' · ' + buildStamp : '')">
          <span class="brand-mark">
            <Zap :size="15" />
          </span>
          <span v-if="!collapsed" class="brand-text">
            <span class="brand-line-1">DeepSeek</span>
            <span class="brand-line-2">Harness</span>
            <!-- v8.1 P3: build code badge（DESKTOP-FRONTEND §3.1） -->
            <span v-if="buildStamp" class="brand-badge">{{ buildStamp }}</span>
          </span>
        </div>
        <button
          class="collapse-btn"
          @click="collapsed = !collapsed"
          :title="collapsed ? '展开' : '折叠'"
        >
          <component :is="collapsed ? PanelLeftOpen : PanelLeftClose" :size="14" />
        </button>
      </div>

      <!-- v8.1 P3: 当前工作区 chip（dsh 原生风格：大圆角 + 渐变）-->
      <div class="sb-workspace" v-if="!collapsed">
        <button class="ws-chip" @click="wsMenuOpen = !wsMenuOpen" :title="workspaceStore.current?.name">
          <span class="ws-chip-icon">
            <Briefcase :size="13" />
          </span>
          <span class="ws-chip-text">
            <span class="ws-chip-name truncate">{{ workspaceStore.current?.name || '默认' }}</span>
            <span class="ws-chip-sub">工作区</span>
          </span>
          <span class="ws-chip-caret">▾</span>
        </button>
      </div>
      <div v-else class="sb-workspace-rail">
        <button
          class="ws-chip-rail"
          @click="wsMenuOpen = !wsMenuOpen"
          :title="workspaceStore.current?.name || '默认'"
        >
          <Briefcase :size="15" />
        </button>
      </div>

      <!-- v8.1 P3: New Session 按钮（DESKTOP-FRONTEND §3.2: primary action）-->
      <div class="sb-new" v-if="!collapsed">
        <button class="new-session-btn" @click="newSession">
          <Plus :size="14" />
          <span>New session</span>
          <span class="new-session-shortcut" title="快捷键 Ctrl+N">Ctrl N</span>
        </button>
      </div>
      <div class="sb-new sb-new-rail" v-else>
        <button class="new-session-btn-rail" @click="newSession" title="New session">
          <Plus :size="14" />
        </button>
      </div>

      <!-- 导航 -->
      <nav class="sb-nav">
        <router-link
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          active-class="active"
          :title="collapsed ? item.label : undefined"
        >
          <component :is="item.icon" :size="15" />
          <span v-if="!collapsed" class="nav-label">{{ item.label }}</span>
          <span v-if="!collapsed && item.badge" class="nav-badge">{{ item.badge }}</span>
        </router-link>
      </nav>

      <!-- v8.1 P3: 工作区菜单（chip 触发；菜单浮动） -->
      <div v-if="wsMenuOpen" class="ws-menu-overlay" @click="wsMenuOpen = false">
        <div class="ws-menu" @click.stop>
          <div class="ws-menu-header">
            <Briefcase :size="13" />
            <span>工作区</span>
            <button class="ws-close" @click="wsMenuOpen = false" title="关闭">
              <XIcon :size="12" />
            </button>
          </div>
          <ul class="ws-menu-list">
            <li
              v-for="w in workspaceStore.workspaces"
              :key="w.id"
              class="ws-menu-item"
              :class="{ active: w.id === workspaceStore.currentId }"
              @click="workspaceStore.setCurrent(w.id); wsMenuOpen = false"
            >
              <template v-if="wsRenameId === w.id">
                <input
                  v-model="wsRenameValue"
                  class="ws-rename-input"
                  @keyup.enter="wsApplyRename"
                  @keyup.esc="wsRenameId = null"
                  @click.stop
                  autofocus
                />
                <button class="ws-mini" @click.stop="wsApplyRename" title="保存">
                  <Check :size="11" />
                </button>
                <button class="ws-mini" @click.stop="wsRenameId = null" title="取消">
                  <XIcon :size="11" />
                </button>
              </template>
              <template v-else>
                <span class="ws-menu-mark">
                  <Briefcase :size="13" />
                </span>
                <span class="ws-menu-name truncate">{{ w.name }}</span>
                <span v-if="w.id === workspaceStore.currentId" class="ws-menu-active-dot" />
                <button
                  v-if="w.id !== 'default'"
                  class="ws-mini"
                  @click.stop="wsStartRename(w.id)"
                  title="重命名"
                >
                  <Pencil :size="11" />
                </button>
                <button
                  v-if="w.id !== 'default'"
                  class="ws-mini"
                  @click.stop="wsDelete(w.id)"
                  title="删除"
                >
                  <TrashIcon :size="11" />
                </button>
              </template>
            </li>
          </ul>
          <div class="ws-menu-foot">
            <template v-if="wsEditing">
              <input
                v-model="wsNewName"
                class="ws-new-input"
                placeholder="新工作区名称"
                @keyup.enter="wsAddNew"
                @keyup.esc="wsEditing = false"
                @click.stop
                autofocus
              />
              <button class="ws-mini" @click="wsAddNew" title="创建">
                <Check :size="11" />
              </button>
              <button class="ws-mini" @click="wsEditing = false" title="取消">
                <XIcon :size="11" />
              </button>
            </template>
            <button v-else class="ws-add-btn" @click="wsEditing = true">
              <PlusIcon :size="12" />
              <span>新建工作区</span>
            </button>
          </div>
        </div>
      </div>

      <!-- 底部 settings / user 座位（永久可见）-->
      <div class="sb-foot">
        <div v-if="!collapsed" class="user-card">
          <div class="user-avatar">
            <UserIcon :size="14" />
          </div>
          <div class="user-info">
            <div class="user-name truncate">
              {{ userStore.user?.username || 'user' }}
            </div>
            <div class="user-meta truncate">
              {{ userStore.isAdmin ? 'Admin' : 'Member' }}
            </div>
          </div>
          <button class="user-action" @click="uiStore.toggleTheme" :title="uiStore.theme === 'light' ? '深色' : '浅色'">
            <component :is="uiStore.theme === 'light' ? Moon : Sun" :size="13" />
          </button>
        </div>
        <button
          v-else
          class="rail-action"
          @click="uiStore.toggleTheme"
          :title="uiStore.theme === 'light' ? '深色' : '浅色'"
        >
          <component :is="uiStore.theme === 'light' ? Moon : Sun" :size="14" />
        </button>
      </div>
    </aside>

    <!-- === 右侧区域：Topbar + Content === -->
    <div class="main-col">
      <!-- v8.1 P3: 顶栏 - model selector + tabs + actions（dsh 原生风格）-->
      <header class="topbar">
        <div class="tb-left">
          <!-- Model selector（dsh 风格：圆角 chip + 下拉）-->
          <div class="model-selector">
            <button class="model-chip" @click="modelMenuOpen = !modelMenuOpen">
              <span class="model-mark">
                <Sparkles :size="13" />
              </span>
              <span class="model-info">
                <span class="model-name">{{ currentModelLabel }}</span>
                <span class="model-provider">{{ currentChannelLabel }}</span>
              </span>
              <span class="model-caret">▾</span>
            </button>
            <div v-if="modelMenuOpen" class="model-menu" @click.stop>
              <div class="model-menu-header">选择模型</div>
              <ul class="model-menu-list">
                <li
                  v-for="m in models.data.value ?? []"
                  :key="m.channel"
                  class="model-menu-item"
                  :class="{ active: m.channel === currentChannelId }"
                  @click="currentChannelId = m.channel; modelMenuOpen = false"
                >
                  <span class="m-mark">
                    <Sparkles :size="12" />
                  </span>
                  <span class="m-name">{{ m.model }}</span>
                  <span class="m-channel">{{ m.channel }}</span>
                </li>
              </ul>
            </div>
          </div>

          <!-- dsh 原生风格 tabs：DeepResearch / DR / T4 / ... -->
          <nav class="tb-tabs" v-if="!isSessionsDetail">
            <button
              v-for="t in primaryTabs"
              :key="t.id"
              class="tb-tab"
              :class="{ active: activeTab === t.id }"
              @click="setActiveTab(t.id)"
            >
              <component v-if="t.icon" :is="t.icon" :size="13" />
              <span>{{ t.label }}</span>
            </button>
          </nav>
        </div>
        <div class="tb-right">
          <span class="tb-conn" :class="['conn-' + healthState]" :title="connectionLabel">
            <CircleDot :size="6" />
            <span>{{ connectionLabel }}</span>
          </span>
          <span class="tb-version">{{ buildVersion }}</span>
          <button class="tb-btn" @click="changeLocale" :title="t('common.language')">
            <Languages :size="13" />
            <span class="tb-btn-label">{{ locale === 'zh-CN' ? 'EN' : '中' }}</span>
          </button>
          <button class="tb-btn" @click="pingHealth" :title="t('common.refresh')">
            <RefreshCw :size="13" />
          </button>
          <button v-if="!isDesktop" class="tb-btn" @click="logout" :title="t('login.logout')">
            <LogOut :size="13" />
          </button>
          <span v-if="isDesktop" class="tb-desktop-pill">
            <Monitor :size="11" />
            Desktop
          </span>
        </div>
      </header>

      <!-- 主内容（dsh 原生渐变背景 + 装饰）-->
      <main class="content">
        <div class="content-deco" aria-hidden="true">
          <span class="deco-spark deco-spark-1"><Sparkles :size="20" /></span>
          <span class="deco-spark deco-spark-2"><Sparkles :size="14" /></span>
          <span class="deco-blob deco-blob-1" />
          <span class="deco-blob deco-blob-2" />
        </div>
        <router-view v-slot="{ Component }">
          <component :is="Component" :key="buildStamp + locale" />
        </router-view>
      </main>

      <!-- 状态条（桌面模式）-->
      <footer v-if="isDesktop" class="statusbar">
        <span><Monitor :size="11" /> DeepSeek Harness Desktop v{{ desktopInfo?.version ?? '8.0.0' }}</span>
        <span class="text-muted">pid={{ desktopInfo?.pid ?? '-' }}</span>
        <span class="text-muted">{{ connectionLabel }}</span>
      </footer>
    </div>

    <ToastStack />
  </div>
</template>

<style scoped>
/* === App Frame 整体（dsh 原生 grid 风格）== */
.app-frame {
  height: 100%;
  display: grid;
  /* dsh 原生 (DESKTOP-FRONTEND §2)：default sidebar 280px / rail 56px */
  grid-template-columns: 280px 1fr;
  background: var(--bg);
  transition: grid-template-columns var(--ds-duration) var(--ds-ease-in-out);
}
.app-frame.sidebar-collapsed {
  grid-template-columns: 56px 1fr;
}

/* === Sidebar === */
.sidebar {
  background: var(--dsw-specific-sidebar-fill, var(--bg-sidebar));
  color: var(--text);
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--dsw-alias-border-l1, transparent);
  overflow: hidden;
  min-width: 0;
}

/* 顶部：brand + 折叠按钮 */
.sb-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 52px;
  padding: 0 14px;
  flex-shrink: 0;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.brand-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  color: #fff;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(110, 89, 255, 0.35);
  transition: transform var(--ds-duration-fast) var(--ds-ease-out);
}
.brand-mark:hover { transform: rotate(-8deg) scale(1.05); }
.brand-text {
  display: flex;
  flex-direction: column;
  gap: 1px;
  line-height: 1.1;
  white-space: nowrap;
  overflow: hidden;
}
.brand-line-1 {
  font-size: 13px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: 0.01em;
}
.brand-line-2 {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.12em;
}
/* v8.1 P3: build code badge (DESKTOP-FRONTEND §3.1) */
.brand-badge {
  display: inline-block;
  margin-top: 2px;
  padding: 1px 5px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: var(--ds-font-family-code, monospace);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--text-faint);
  align-self: flex-start;
  line-height: 1.3;
}
.collapse-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: var(--ds-radius-xs);
  color: var(--text-faint);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.collapse-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}

/* 新会话按钮（DESKTOP-FRONTEND §3.2: primary action） */
.sb-new {
  padding: 6px 12px 10px;
  flex-shrink: 0;
}
.sb-new-rail {
  padding: 6px 12px 10px;
  display: flex;
  justify-content: center;
}
.new-session-btn {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  width: 100%;
  height: 34px;
  padding: 0 10px;
  border-radius: 10px;
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  box-shadow: 0 4px 14px rgba(110, 89, 255, 0.32);
  position: relative;
  font-family: inherit;
  transition:
    transform var(--ds-duration-fast) var(--ds-ease-out),
    background var(--ds-duration-fast) var(--ds-ease-in-out),
    box-shadow var(--ds-duration-fast) var(--ds-ease-in-out);
}
.new-session-btn:hover {
  background: linear-gradient(135deg, #7a66ff 0%, #ba7aff 100%);
  transform: translateY(-1px);
  box-shadow: 0 6px 18px rgba(110, 89, 255, 0.42);
}
.new-session-btn:active {
  transform: translateY(0);
  background: linear-gradient(135deg, #6452f0 0%, #a362ec 100%);
}
.new-session-btn > span:nth-child(2) { flex: 1; text-align: left; }
.new-session-shortcut {
  margin-left: auto;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.18);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.04em;
  opacity: 0;
  transition: opacity var(--ds-duration-fast) var(--ds-ease-in-out);
}
.new-session-btn:hover .new-session-shortcut,
.new-session-btn:focus-within .new-session-shortcut { opacity: 1; }
.new-session-btn-rail {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--accent);
  color: var(--accent-fg, #fff);
  cursor: pointer;
  border: none;
}
.new-session-btn-rail:hover { background: var(--accent-hover); }

/* 导航列表 */
.sb-nav {
  flex: 1;
  overflow-y: auto;
  padding: 6px 8px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  border-radius: var(--ds-radius-sm);
  color: var(--text-muted);
  font-size: 13px;
  text-decoration: none;
  position: relative;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
  white-space: nowrap;
  overflow: hidden;
}
.nav-item:hover {
  background: var(--bg-hover);
  color: var(--text);
  text-decoration: none;
}
.nav-item.active {
  background: var(--bg-elevated);
  color: var(--text);
  font-weight: 500;
}
.app-frame.sidebar-collapsed .nav-item {
  justify-content: center;
  padding: 8px 0;
}
.nav-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nav-badge {
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 10px;
  padding: 1px 5px;
  border-radius: var(--ds-radius-xs);
  font-weight: 600;
}

/* 底部 user 座位 */
.sb-foot {
  flex-shrink: 0;
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  padding: 8px;
}
.user-card {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--ds-radius-sm);
  background: var(--bg-elevated);
}
.user-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--bg-active);
  color: var(--text-muted);
  flex-shrink: 0;
}
.user-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.user-name {
  font-size: 12px;
  font-weight: 500;
  color: var(--text);
}
.user-meta {
  font-size: 10px;
  color: var(--text-faint);
}
.user-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: var(--ds-radius-xs);
  color: var(--text-faint);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
  flex-shrink: 0;
}
.user-action:hover {
  background: var(--bg-hover);
  color: var(--text);
}
.rail-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: var(--ds-radius-sm);
  color: var(--text-faint);
  cursor: pointer;
  margin: 0 auto;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.rail-action:hover {
  background: var(--bg-hover);
  color: var(--text);
}

/* === 工作区切换器（dsh 原生风格：黑底渐变 chip） === */
.sb-workspace {
  flex-shrink: 0;
  padding: 0 10px 10px;
}
.ws-chip {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border-radius: 10px;
  background: linear-gradient(135deg, #2a2436 0%, #1f1b2e 100%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  color: var(--text);
  cursor: pointer;
  font-family: inherit;
  text-align: left;
  transition: transform var(--ds-duration-fast) var(--ds-ease-out), background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.ws-chip:hover {
  background: linear-gradient(135deg, #312a40 0%, #251f37 100%);
  transform: translateY(-1px);
}
.ws-chip:active { transform: translateY(0); }
.ws-chip-icon {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
  box-shadow: 0 2px 6px rgba(110, 89, 255, 0.35);
}
.ws-chip-text {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 1;
  min-width: 0;
}
.ws-chip-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  line-height: 1.2;
}
.ws-chip-sub {
  font-size: 10px;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  line-height: 1;
}
.ws-chip-caret {
  color: var(--text-faint);
  font-size: 11px;
  flex-shrink: 0;
  transition: transform var(--ds-duration-fast) var(--ds-ease-out);
}
.ws-chip[aria-expanded="true"] .ws-chip-caret { transform: rotate(180deg); }

.sb-workspace-rail {
  flex-shrink: 0;
  padding: 0 0 14px;
  display: flex;
  justify-content: center;
}
.ws-chip-rail {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  border-radius: 10px;
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  color: #fff;
  cursor: pointer;
  border: none;
  box-shadow: 0 2px 6px rgba(110, 89, 255, 0.35);
  transition: transform var(--ds-duration-fast) var(--ds-ease-out);
}
.ws-chip-rail:hover { transform: translateY(-1px); }

.ws-menu-overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(0, 0, 0, 0.18);
  backdrop-filter: blur(2px);
  animation: ws-fade-in var(--ds-duration-fast) var(--ds-ease-out);
}
@keyframes ws-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
.ws-menu {
  position: absolute;
  top: 110px;
  left: 252px;
  width: 240px;
  background: var(--bg-elevated);
  border: 1px solid var(--dsw-alias-border-l1, transparent);
  border-radius: var(--ds-radius-md);
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.32);
  padding: 6px;
  animation: ws-pop-in var(--ds-duration-fast) var(--ds-ease-out);
}
@keyframes ws-pop-in {
  from { opacity: 0; transform: scale(0.96) translateY(-4px); }
  to { opacity: 1; transform: scale(1) translateY(0); }
}
.ws-menu-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-faint);
  padding: 6px 8px 8px;
}
.ws-menu-header > span { flex: 1; }
.ws-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: var(--ds-radius-xs);
  border: none;
  background: transparent;
  color: var(--text-faint);
  cursor: pointer;
}
.ws-close:hover { background: var(--bg-hover); color: var(--text); }

.ws-menu-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.ws-menu-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  border-radius: var(--ds-radius-sm);
  font-size: 12px;
  color: var(--text);
  cursor: pointer;
  user-select: none;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.ws-menu-item:hover { background: var(--bg-hover); }
.ws-menu-item.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 500;
}
.ws-menu-mark {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: var(--bg);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  flex-shrink: 0;
}
.ws-menu-item.active .ws-menu-mark {
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  color: #fff;
}
.ws-menu-name { flex: 1; min-width: 0; }
.ws-menu-active-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  flex-shrink: 0;
}
.ws-mini {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: var(--ds-radius-xs);
  color: var(--text-faint);
  cursor: pointer;
  border: none;
  background: transparent;
  flex-shrink: 0;
}
.ws-mini:hover { background: var(--bg-active); color: var(--text); }
.ws-rename-input,
.ws-new-input {
  flex: 1;
  min-width: 0;
  height: 22px;
  padding: 0 6px;
  font-size: 11px;
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-xs);
  background: var(--bg);
  color: var(--text);
  font-family: inherit;
}
.ws-menu-foot {
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  margin-top: 6px;
  padding: 6px 4px 2px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.ws-add-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  border: none;
  background: transparent;
  border-radius: var(--ds-radius-xs);
  width: 100%;
  font-family: inherit;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.ws-add-btn:hover { background: var(--bg-hover); color: var(--text); }

/* === 右侧 main-col === */
.main-col {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--bg);
}

/* === Topbar === */
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 46px;
  padding: 0 18px;
  background: var(--dsw-specific-toolbar-fill, var(--bg-toolbar));
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
  flex-shrink: 0;
  user-select: none;
  gap: 16px;
}
.tb-left {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  flex: 1;
}

/* v8.1 P3: Model selector（dsh 原生风格 chip） */
.model-selector { position: relative; }
.model-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 10px 0 8px;
  border-radius: 10px;
  background: linear-gradient(135deg, #2a2436 0%, #1f1b2e 100%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  color: var(--text);
  cursor: pointer;
  font-family: inherit;
  transition: transform var(--ds-duration-fast) var(--ds-ease-out), background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.model-chip:hover {
  background: linear-gradient(135deg, #312a40 0%, #251f37 100%);
  transform: translateY(-1px);
}
.model-mark {
  width: 22px;
  height: 22px;
  border-radius: 7px;
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
  box-shadow: 0 2px 6px rgba(110, 89, 255, 0.35);
}
.model-info {
  display: flex;
  flex-direction: column;
  gap: 0;
  line-height: 1.1;
}
.model-name {
  font-size: 12px;
  font-weight: 600;
}
.model-provider {
  font-size: 9px;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.model-caret {
  font-size: 10px;
  color: var(--text-faint);
  transition: transform var(--ds-duration-fast) var(--ds-ease-out);
}
.model-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  width: 280px;
  background: var(--bg-elevated);
  border: 1px solid var(--dsw-alias-border-l1, transparent);
  border-radius: var(--ds-radius-md);
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.32);
  padding: 6px;
  z-index: 40;
  animation: ws-pop-in var(--ds-duration-fast) var(--ds-ease-out);
}
.model-menu-header {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-faint);
  padding: 6px 8px 4px;
}
.model-menu-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.model-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  border-radius: var(--ds-radius-sm);
  cursor: pointer;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.model-menu-item:hover { background: var(--bg-hover); }
.model-menu-item.active {
  background: var(--accent-soft);
  color: var(--accent);
}
.m-mark {
  width: 24px;
  height: 24px;
  border-radius: 7px;
  background: var(--bg);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  flex-shrink: 0;
}
.model-menu-item.active .m-mark {
  background: linear-gradient(135deg, #6e59ff 0%, #b06bff 100%);
  color: #fff;
}
.m-name { flex: 1; font-size: 12px; font-weight: 500; min-width: 0; }
.m-channel { font-size: 10px; color: var(--text-faint); }

/* v8.1 P3: Topbar tabs（dsh 原生风格：下划线选中） */
.tb-tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: 1;
  overflow-x: auto;
  scrollbar-width: none;
}
.tb-tabs::-webkit-scrollbar { display: none; }
.tb-tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 10px;
  border-radius: 8px;
  background: transparent;
  border: none;
  color: var(--text-muted);
  font-family: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  position: relative;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out), color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.tb-tab:hover { color: var(--text); background: var(--bg-hover); }
.tb-tab.active {
  color: var(--text);
  background: var(--bg-hover);
}
.tb-tab.active::after {
  content: '';
  position: absolute;
  left: 10px;
  right: 10px;
  bottom: -1px;
  height: 2px;
  background: linear-gradient(90deg, #6e59ff, #b06bff);
  border-radius: 2px;
}

.tb-conn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
}
.conn-ok { color: var(--success); }
.conn-failed { color: var(--danger); }
.tb-sep {
  width: 1px;
  height: 14px;
  background: var(--border);
}
.tb-version {
  font-size: 11px;
  color: var(--text-faint);
  font-family: var(--ds-font-family-code);
}
.tb-right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.tb-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 8px;
  border-radius: 8px;
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  border: none;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out),
    color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.tb-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}
.tb-btn-label {
  font-size: 10px;
  font-weight: 600;
}
.tb-desktop-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 8px;
  background: var(--accent-soft);
  color: var(--accent);
  border-radius: var(--ds-radius-sm);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* === Content (dsh 原生渐变背景 + 装饰) === */
.content {
  flex: 1;
  min-width: 0;
  position: relative;
  overflow: auto;
  background: linear-gradient(
    180deg,
    #1a1424 0%,
    #14101d 35%,
    #100c1a 100%
  );
}
[data-theme="light"] .content {
  background: linear-gradient(
    180deg,
    #faf8ff 0%,
    #f3eef9 35%,
    #ede9f4 100%
  );
}
.content-deco {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
  z-index: 0;
}
.deco-spark {
  position: absolute;
  color: rgba(176, 107, 255, 0.28);
  animation: deco-float 12s ease-in-out infinite;
}
.deco-spark-1 {
  top: 14%;
  left: 8%;
  font-size: 20px;
}
.deco-spark-2 {
  top: 38%;
  right: 12%;
  font-size: 14px;
  animation-delay: -4s;
  color: rgba(110, 89, 255, 0.22);
}
.deco-blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(60px);
  opacity: 0.25;
  animation: deco-float 18s ease-in-out infinite;
}
.deco-blob-1 {
  top: 20%;
  left: -10%;
  width: 380px;
  height: 380px;
  background: radial-gradient(circle, #6e59ff 0%, transparent 70%);
}
.deco-blob-2 {
  bottom: -10%;
  right: -8%;
  width: 320px;
  height: 320px;
  background: radial-gradient(circle, #b06bff 0%, transparent 70%);
  animation-delay: -8s;
}
@keyframes deco-float {
  0%, 100% { transform: translateY(0) scale(1); }
  50% { transform: translateY(-20px) scale(1.08); }
}
.content > :not(.content-deco) {
  position: relative;
  z-index: 1;
}

/* === Status bar === */
.statusbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 14px;
  background: var(--bg-toolbar);
  color: var(--text-faint);
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  font-size: 10px;
  height: 22px;
  flex-shrink: 0;
}
</style>
