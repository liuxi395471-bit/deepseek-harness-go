// store/boot.ts — 启动状态（v8.1 P4: no-auth 自动登录）
//
// 暴露一个 Promise，App.onMounted 内完成 health 探测 + 必要时自动
// stub login；router beforeEach 等待它 resolve 后再判断 isLoggedIn。

import { reactive } from 'vue'
import { useUserStore } from './user'
import { client } from '@/api/client'

interface BootState {
  ready: boolean
  noAuth: boolean
  error: string | null
}

const state = reactive<BootState>({
  ready: false,
  noAuth: false,
  error: null,
})

let pending: Promise<void> | null = null

export function getBootState(): BootState {
  return state
}

export function waitForBoot(): Promise<void> {
  return pending ?? Promise.resolve()
}

export async function bootApp(): Promise<void> {
  if (pending) return pending
  pending = (async () => {
    const userStore = useUserStore()
    // 1) 探测后端 /health
    try {
      const r = await fetch('/api/v1/console/health/')
      if (r.ok) {
        const j = await r.json()
        if (j?.noAuth === true) {
          state.noAuth = true
          // 后端 NoAuth 模式 → 没 token 时自动 stub login
          if (!userStore.token) {
            try {
              const lr = await client.post<{
                token: string
                expiresAt: string
                user: { id: number; username: string; role: 'admin' | 'user'; createdAt: string }
              }>('/auth/login', { username: 'no-auth', password: 'no-auth' })
              userStore.setSession(lr.data.token, lr.data.user, lr.data.expiresAt)
            } catch (e) {
              state.error = e instanceof Error ? e.message : String(e)
            }
          }
        }
      }
    } catch (e) {
      state.error = e instanceof Error ? e.message : String(e)
    }
    state.ready = true
  })()
  return pending
}