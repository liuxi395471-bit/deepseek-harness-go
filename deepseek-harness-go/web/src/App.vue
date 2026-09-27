<script setup lang="ts">
// App.vue — 顶层布局
//
// 顶部栏 + 侧边导航 + 主内容区；token 通过右侧表单输入。

import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useTokenStore } from '@/stores/token'
import { client } from '@/api/client'
import { MessageSquare, Package, Cpu, ListTodo, ShieldCheck, LogOut } from 'lucide-vue-next'

const router = useRouter()
const tokenStore = useTokenStore()

const tokenInput = ref(tokenStore.token)
const healthState = ref<'unknown' | 'ok' | 'failed'>('unknown')

async function saveToken() {
  tokenStore.setToken(tokenInput.value.trim())
  await pingHealth()
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

const nav = [
  { to: '/sessions', label: '会话', icon: MessageSquare },
  { to: '/plugins', label: '插件', icon: Package },
  { to: '/models', label: '模型', icon: Cpu },
  { to: '/tasks', label: '任务', icon: ListTodo },
  { to: '/approvals', label: '审批', icon: ShieldCheck },
]
</script>

<template>
  <div class="layout">
    <header class="topbar">
      <div class="brand">
        <span class="logo">⚡</span>
        <span class="title">DeepSeek Harness · 控制台</span>
        <span class="version">v8.0.0-dev</span>
      </div>
      <div class="token-form">
        <input
          v-model="tokenInput"
          type="text"
          placeholder="Bearer token"
          @keyup.enter="saveToken"
        />
        <button class="btn" @click="saveToken">连接</button>
        <button v-if="tokenStore.token" class="btn" @click="logout" title="登出">
          <LogOut :size="14" />
        </button>
        <span v-if="healthState === 'ok'" class="badge badge-loaded">已连接</span>
        <span v-else-if="healthState === 'failed'" class="badge badge-failed">认证失败</span>
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
        <router-view />
      </main>
    </div>
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
  background: #edf2f7;
}
.nav-item.active {
  background: #ebf4ff;
  border-left-color: var(--accent);
  color: var(--accent);
  font-weight: 500;
}
.content {
  flex: 1;
  padding: 20px;
  overflow: auto;
}
</style>
