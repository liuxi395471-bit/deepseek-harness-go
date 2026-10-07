// main.ts — SPA 入口
//
// createApp + pinia + vue-router + vue-query 全套接上。
// token 优先从 localStorage 读取，dev fallback 用 import.meta.env。
// v8.1 P4: App mount 前先 bootApp()（探测 NoAuth + 自动 stub login），
// 让 router guard 直接看到 isLoggedIn。

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import App from './App.vue'
import { router } from './router'
import './styles/main.css'
import { useUIStore } from './stores/ui'
import { bootApp } from './stores/boot'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      gcTime: 5 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)

// 提前初始化 ui store 以确保 data-theme 在 mounted 前已应用（避免 flash）。
useUIStore()

// 启动探测（NoAuth 自动 stub login）。必须在 router 解析前完成。
await bootApp()

app.use(router)
app.use(VueQueryPlugin, { queryClient })
app.mount('#app')
