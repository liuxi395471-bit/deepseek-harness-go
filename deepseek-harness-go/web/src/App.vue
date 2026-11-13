<script setup lang="ts">
/**
 * 应用根组件。
 *
 * 布局：左侧会话侧边栏 + 右侧主区（顶栏 + 消息流 + 输入框）。
 * 未登录时展示 LoginView。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { Menu, Moon, PanelLeft, Sun, X } from 'lucide-vue-next'
import Sidebar from '@/components/Sidebar.vue'
import Composer from '@/components/Composer.vue'
import MessageBubble from '@/components/MessageBubble.vue'
import LoginView from '@/components/LoginView.vue'
import {
  appError,
  clearAuthToken,
  currentSessionId,
  dismissError,
  isRunning,
  lastUsage,
  loadSessions,
  messages,
  phaseLabel,
  sendMessage,
  stopRun,
  token,
} from '@/composables/useChat'

/** 侧边栏是否展开（移动端为浮层，桌面端常驻）。 */
const sidebarOpen = ref(true)
/** 移动端遮罩是否显示。 */
const isNarrow = ref(false)
/** 主题：light / dark。 */
const theme = ref<'light' | 'dark'>('light')

/** 是否已登录。 */
const loggedIn = computed(() => token.value.length > 0)

/** 是否有消息。 */
const hasMessages = computed(() => messages.value.length > 0)

/** 消息滚动容器。 */
const scrollBox = ref<HTMLElement | null>(null)

/** 当前会话标题（取第一条用户消息的摘要）。 */
const title = computed(() => {
  const first = messages.value.find((m) => m.role === 'user')
  if (!first) return '新对话'
  const t = first.content.replace(/\s+/g, ' ').trim()
  return t.length > 40 ? `${t.slice(0, 40)}…` : t
})

/** 监听窗口宽度，判断是否为窄屏（移动端布局）。 */
function updateViewport(): void {
  isNarrow.value = window.innerWidth < 768
  if (!isNarrow.value) sidebarOpen.value = true
}

function applyTheme(): void {
  document.documentElement.setAttribute('data-theme', theme.value)
  try {
    localStorage.setItem('dsh.theme', theme.value)
  } catch {
    /* 忽略持久化失败 */
  }
}

function toggleTheme(): void {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
  applyTheme()
}

/** 消息变化时滚到底部。 */
function scrollToBottom(): void {
  const el = scrollBox.value
  if (!el) return
  requestAnimationFrame(() => {
    el.scrollTop = el.scrollHeight
  })
}

watch(
  () => messages.value.map((m) => m.content.length).join(','),
  () => scrollToBottom(),
)

watch(isRunning, () => scrollToBottom())

onMounted(() => {
  updateViewport()
  window.addEventListener('resize', updateViewport)

  // 恢复主题偏好
  try {
    const saved = localStorage.getItem('dsh.theme')
    if (saved === 'dark' || saved === 'light') {
      theme.value = saved
    } else if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
      theme.value = 'dark'
    }
  } catch {
    /* 忽略 */
  }
  applyTheme()

  if (loggedIn.value) {
    void loadSessions()
  }
})

async function onLoginSuccess(): Promise<void> {
  await loadSessions()
}

function onLogout(): void {
  clearAuthToken()
}

/** 移动端点击遮罩关闭侧边栏。 */
function closeSidebarOnNarrow(): void {
  if (isNarrow.value) sidebarOpen.value = false
}
</script>

<template>
  <div v-if="!loggedIn" class="app">
    <LoginView @success="onLoginSuccess" />
  </div>

  <div v-else class="app app--layout">
    <!-- 侧边栏 -->
    <transition name="slide">
      <div
        v-show="sidebarOpen"
        class="app__sidebar"
        :class="{ 'app__sidebar--overlay': isNarrow }"
      >
        <Sidebar @logout="onLogout" @close="closeSidebarOnNarrow" />
      </div>
    </transition>

    <!-- 窄屏遮罩 -->
    <div
      v-if="isNarrow && sidebarOpen"
      class="app__scrim"
      @click="closeSidebarOnNarrow"
    ></div>

    <!-- 主区 -->
    <main class="main">
      <!-- 顶栏 -->
      <header class="topbar">
        <button
          class="icon-btn icon-btn--ghost"
          :title="sidebarOpen ? '收起侧边栏' : '展开侧边栏'"
          :aria-label="sidebarOpen ? '收起侧边栏' : '展开侧边栏'"
          @click="sidebarOpen = !sidebarOpen"
        >
          <PanelLeft v-if="!isNarrow" :size="18" />
          <Menu v-else :size="18" />
        </button>

        <h1 class="topbar__title" :title="title">{{ title }}</h1>

        <div class="topbar__right">
          <span v-if="isRunning" class="topbar__phase">
            <span class="topbar__dot"></span>
            {{ phaseLabel }}
          </span>
          <button
            class="icon-btn icon-btn--ghost"
            :title="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
            :aria-label="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
            @click="toggleTheme"
          >
            <Moon v-if="theme === 'light'" :size="18" />
            <Sun v-else :size="18" />
          </button>
        </div>
      </header>

      <!-- 错误提示 -->
      <div v-if="appError" class="alert" role="alert">
        <span class="alert__text">{{ appError }}</span>
        <button class="icon-btn icon-btn--ghost" aria-label="关闭" @click="dismissError">
          <X :size="15" />
        </button>
      </div>

      <!-- 消息区 -->
      <div ref="scrollBox" class="messages">
        <!-- 空状态 -->
        <div v-if="!hasMessages" class="empty">
          <div class="empty__mark" aria-hidden="true"></div>
          <h2 class="empty__title">有什么可以帮你的？</h2>
          <p class="empty__subtitle">
            DeepSeek Harness 是一个 ReAct agent：它会调用工具、读取结果，
            然后给出最终答案。
          </p>
          <div class="empty__hints">
            <button
              v-for="h in ['用 greet 工具向我打个招呼', '读取工作区里的 README 文件', '介绍一下你能做哪些事']"
              :key="h"
              class="hint"
              @click="sendMessage(h)"
            >
              {{ h }}
            </button>
          </div>
        </div>

        <!-- 消息列表 -->
        <div v-else class="messages__inner">
          <MessageBubble v-for="m in messages" :key="m.key" :msg="m" />

          <!-- usage 摘要 -->
          <div v-if="lastUsage && lastUsage.total > 0" class="usage">
            本轮用量：{{ lastUsage.prompt }} 输入 / {{ lastUsage.completion }} 输出 /
            {{ lastUsage.total }} tokens
          </div>

          <!-- 会话 id 提示（便于用户对照服务端数据） -->
          <p v-if="currentSessionId" class="session-id">
            会话 ID：<code>{{ currentSessionId }}</code>
          </p>
        </div>
      </div>

      <!-- 输入区 -->
      <Composer
        :running="isRunning"
        @send="sendMessage"
        @stop="stopRun"
      />
    </main>
  </div>
</template>

<style scoped>
.app {
  height: 100%;
  display: flex;
}

.app--layout {
  overflow: hidden;
}

.app__sidebar {
  flex-shrink: 0;
  height: 100%;
}

/* 窄屏：侧边栏浮在内容之上 */
.app__sidebar--overlay {
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  z-index: 40;
  box-shadow: var(--shadow-lg);
}

.app__scrim {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  z-index: 30;
}

/* 主区 */
.main {
  flex: 1;
  min-width: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-app);
}

/* 顶栏 */
.topbar {
  height: 54px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-app);
}

.topbar__title {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: 15px;
  font-weight: 500;
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.topbar__right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.topbar__phase {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.topbar__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  animation: pulse-dot 1.2s ease-in-out infinite;
}

/* 错误提示 */
.alert {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 10px 16px 0;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  background: var(--danger-soft);
  border: 1px solid var(--danger);
  color: var(--danger);
  font-size: 13.5px;
}

.alert__text {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

/* 消息区 */
.messages {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
}

.messages__inner {
  width: 100%;
  max-width: var(--content-max-width);
  margin: 0 auto;
  padding: 28px 16px 12px;
}

/* 空状态 */
.empty {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 24px;
  text-align: center;
}

.empty__mark {
  width: 44px;
  height: 44px;
  border-radius: 13px;
  background: linear-gradient(135deg, var(--accent), var(--accent-hover));
  margin-bottom: 20px;
}

.empty__title {
  margin: 0 0 8px;
  font-size: 26px;
  font-weight: 600;
  letter-spacing: -0.02em;
}

.empty__subtitle {
  margin: 0 0 26px;
  max-width: 460px;
  font-size: 14.5px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.empty__hints {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  max-width: 560px;
}

.hint {
  padding: 9px 15px;
  border-radius: var(--radius-xl);
  border: 1px solid var(--border-strong);
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 13.5px;
  transition: background var(--transition), color var(--transition),
    border-color var(--transition);
}

.hint:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--text-tertiary);
}

/* usage 摘要 */
.usage {
  margin-top: 4px;
  padding-top: 12px;
  border-top: 1px solid var(--border-subtle);
  font-size: 12px;
  color: var(--text-tertiary);
}

/* 会话 id */
.session-id {
  margin: 8px 0 0;
  font-size: 11.5px;
  color: var(--text-tertiary);
}

.session-id code {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  background: var(--bg-code);
  border: 1px solid var(--border-subtle);
  border-radius: 4px;
  padding: 0.1em 0.35em;
}

/* 侧边栏滑动动画 */
.slide-enter-active,
.slide-leave-active {
  transition: transform 200ms cubic-bezier(0.4, 0, 0.2, 1);
}

.slide-enter-from,
.slide-leave-to {
  transform: translateX(-100%);
}

@media (max-width: 767px) {
  .topbar__title {
    font-size: 14.5px;
  }
  .messages__inner {
    padding: 20px 14px 8px;
  }
  .empty__title {
    font-size: 22px;
  }
}
</style>