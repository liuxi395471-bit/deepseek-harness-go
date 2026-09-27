<script setup lang="ts">
// App.vue — 顶层布局
//
// 顶部栏 + 侧边导航 + 主内容区；token 通过右侧表单输入。
// 当 ?from=desktop 时（启动器嵌入场景），隐藏 token 输入框（已自动注入）
// 并在底部显示状态条（启动器版本/工作区）。

import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useTokenStore } from '@/stores/token'
import { useUIStore } from '@/stores/ui'
import { client } from '@/api/client'
import { useI18n } from '@/i18n'
import {
  MessageSquare,
  Package,
  Cpu,
  ListTodo,
  ShieldCheck,
  LogOut,
  Monitor,
  Moon,
  Sun,
  RefreshCw,
  Languages,
} from 'lucide-vue-next'
import ToastStack from '@/components/ToastStack.vue'

const router = useRouter()
const tokenStore = useTokenStore()
const uiStore = useUIStore()
const { locale, setLocale, t } = useI18n()

const tokenInput = ref(tokenStore.token)
const healthState = ref<'unknown' | 'ok' | 'failed'>('unknown')
const buildVersion = ref<string>('v8.0.0-dev')
const buildStamp = ref<string>('')

// 启动器模式：通过 ?from=desktop 进入时启用
const isDesktop = ref(false)
const desktopInfo = ref<{ version?: string; pid?: string } | null>(null)

const nav = computed(() => [
  { to: '/sessions', label: t('nav.sessions'), icon: MessageSquare },
  { to: '/plugins', label: t('nav.plugins'), icon: Package },
  { to: '/models', label: t('nav.models'), icon: Cpu },
  { to: '/tasks', label: t('nav.tasks'), icon: ListTodo },
  { to: '/approvals', label: t('nav.approvals'), icon: ShieldCheck },
])

async function fetchVersion() {
  try {
    const { data } = await client.get<{ version: string }>('/health')
    if (data?.version && data.version !== 'dev') {
      buildVersion.value = 'v' + data.version
    }
  } catch {
    /* ignore：未连接时静默 */
  }
  try {
    const r = await fetch('/api/v1/console/state?key=buildStamp', {
      headers: tokenStore.token ? { Authorization: `Bearer ${tokenStore.token}` } : {},
    })
    if (r.ok) {
      const j = await r.json()
      if (j?.value) buildStamp.value = j.value
    }
  } catch {
    /* ignore */
  }
}

onMounted(async () => {
  const params = new URLSearchParams(window.location.search)
  isDesktop.value = params.get('from') === 'desktop'
  // 桌面模式下从 URL 读取 token（启动器生成并注入）
  const urlToken = params.get('token')
  if (urlToken) {
    tokenStore.setToken(urlToken)
    // 清理 URL（避免 token 留在历史里）
    params.delete('token')
    const qs = params.toString()
    const newUrl =
      window.location.pathname + (qs ? '?' + qs : '') + window.location.hash
    window.history.replaceState({}, '', newUrl)
  }
  // 若启动器没注入 token，尝试读 localStorage
  if (!tokenStore.token) {
    const stored = localStorage.getItem('dsh.console.token')
    if (stored) tokenStore.setToken(stored)
  }
  if (tokenStore.token) await pingHealth()
  await fetchVersion()
  if (isDesktop.value) {
    fetch('/api/v1/console/state?key=desktop', { headers: { Authorization: `Bearer ${tokenStore.token}` } })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d?.value) {
          try { desktopInfo.value = JSON.parse(d.value) } catch { /* ignore */ }
        }
      })
      .catch(() => {})
  }
})

async function saveToken() {
  tokenStore.setToken(tokenInput.value.trim())
  await pingHealth()
  await fetchVersion()
}

async function pingHealth() {
  try {
    await client.get('/health')
    healthState.value = 'ok'
  } catch {
    healthState.value = 'failed'
  }
}

function logout() {
  tokenStore.clearToken()
  tokenInput.value = ''
  healthState.value = 'unknown'
  router.push('/sessions')
}

function changeLocale() {
  setLocale(locale.value === 'zh-CN' ? 'en-US' : 'zh-CN')
}

const connectionLabel = computed(() => {
  if (healthState.value === 'ok') return t('common.connected')
  if (healthState.value === 'failed') return t('common.authFailed')
  return t('common.disconnected')
})
</script>

<template>
  <div class="layout">
    <header class="topbar">
      <div class="brand">
        <span class="logo">⚡</span>
        <span class="title">{{ t('app.brand') }}</span>
        <span class="version">{{ buildVersion }}<span v-if="buildStamp"> · {{ buildStamp }}</span></span>
      </div>
      <div class="token-form">
        <template v-if="!isDesktop">
          <input
            v-model="tokenInput"
            type="text"
            placeholder="Bearer token"
            @keyup.enter="saveToken"
          />
          <button class="btn" @click="saveToken">
            <RefreshCw :size="14" />
            {{ t('common.connect') }}
          </button>
          <button v-if="tokenStore.token" class="btn" @click="logout" :title="t('common.disconnect')">
            <LogOut :size="14" />
          </button>
        </template>
        <template v-else>
          <Monitor :size="14" />
          <span class="badge badge-loaded">桌面模式</span>
          <span class="text-xs text-muted">自动连接本地 dsh</span>
        </template>
        <button class="btn" @click="uiStore.toggleTheme" :title="uiStore.theme === 'light' ? t('common.theme.dark') : t('common.theme.light')">
          <component :is="uiStore.theme === 'light' ? Moon : Sun" :size="14" />
        </button>
        <button class="btn" @click="changeLocale" :title="t('common.language')">
          <Languages :size="14" />
          <span class="text-xs">{{ locale === 'zh-CN' ? 'EN' : '中' }}</span>
        </button>
        <span v-if="healthState === 'ok'" class="badge badge-loaded">{{ connectionLabel }}</span>
        <span v-else-if="healthState === 'failed'" class="badge badge-failed">{{ connectionLabel }}</span>
        <span v-else class="badge">{{ connectionLabel }}</span>
      </div>
    </header>

    <div class="body">
      <aside class="sidebar">
        <nav>
          <router-link
            v-for="item in nav"
            :key="item.to"
            :to="item.to"
            class="nav-item"
            active-class="active"
          >
            <component :is="item.icon" :size="16" />
            <span>{{ item.label }}</span>
          </router-link>
        </nav>
      </aside>
      <main class="content">
        <router-view v-slot="{ Component }">
          <component :is="Component" :key="buildStamp + locale" />
        </router-view>
      </main>
    </div>

    <footer v-if="isDesktop" class="statusbar">
      <span><Monitor :size="12" /> DeepSeek Harness Desktop v{{ desktopInfo?.version ?? '8.0.0' }}</span>
      <span class="text-muted">pid={{ desktopInfo?.pid ?? '-' }}</span>
      <span class="text-muted">{{ connectionLabel }}</span>
    </footer>

    <ToastStack />
  </div>
</template>

<style scoped>
.layout {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 20px;
  background: #1a202c;
  color: #fff;
  border-bottom: 1px solid #2d3748;
}
:global([data-theme="dark"]) .topbar {
  background: #020617;
  border-bottom-color: #1e293b;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
.logo {
  font-size: 20px;
}
.title {
  font-weight: 600;
  font-size: 15px;
}
.version {
  font-size: 11px;
  opacity: 0.5;
  padding: 2px 6px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 4px;
}
.token-form {
  display: flex;
  align-items: center;
  gap: 8px;
}
.token-form input {
  width: 240px;
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  border-color: rgba(255, 255, 255, 0.2);
}
.token-form input::placeholder {
  color: rgba(255, 255, 255, 0.5);
}
.token-form .btn {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  border-color: rgba(255, 255, 255, 0.2);
}
.token-form .btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.2);
}

.body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.sidebar {
  width: 180px;
  background: var(--bg-card);
  border-right: 1px solid var(--border);
  padding: 12px 0;
}
.sidebar nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  color: var(--text);
  font-size: 13px;
  border-left: 3px solid transparent;
  text-decoration: none;
}
.nav-item:hover {
  background: var(--border);
}
.nav-item.active {
  background: var(--bg);
  border-left-color: var(--accent);
  color: var(--accent);
  font-weight: 500;
}
.content {
  flex: 1;
  padding: 20px;
  overflow: auto;
}
.statusbar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 6px 20px;
  background: #1a202c;
  color: rgba(255, 255, 255, 0.7);
  border-top: 1px solid #2d3748;
  font-size: 11px;
}
</style>
