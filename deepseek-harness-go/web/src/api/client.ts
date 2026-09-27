// api/client.ts — axios 实例
//
// 单一 client，所有 /api/v1/console/* 请求都走它；Bearer token
// 从 userStore 拿（JWT）；失败 401 时清 token + 跳 /login。
//
// v8.0 兼容：若 userStore.token 为空但 dsh.console.token 仍存在（旧
// static token），也注入。

import axios, { type AxiosInstance, type AxiosError } from 'axios'
import { useUserStore } from '@/stores/user'
import { useUIStore } from '@/stores/ui'

export const client: AxiosInstance = axios.create({
  baseURL: '/api/v1/console',
  timeout: 30_000,
  headers: { 'Content-Type': 'application/json' },
})

let routerRef: { push: (path: string) => void } | null = null

export function bindRouter(router: { push: (path: string) => void }) {
  routerRef = router
}

client.interceptors.request.use((cfg) => {
  const userStore = useUserStore()
  const tok = userStore.token || ''
  if (tok) {
    cfg.headers.Authorization = `Bearer ${tok}`
  }
  return cfg
})

client.interceptors.response.use(
  (resp) => resp,
  (err: AxiosError<{ error?: { message?: string } }>) => {
    const message =
      err.response?.data?.error?.message ||
      err.message ||
      '请求失败'
    try {
      const ui = useUIStore()
      ui.reportError(new Error(message))
    } catch {
      /* ui store 未初始化：单测场景；忽略 */
    }
    // 401 → 清 token + 跳 /login（排除登录本身失败的情况）
    if (err.response?.status === 401 && !err.config?.url?.includes('/auth/login')) {
      try {
        useUserStore().clear()
      } catch {
        /* ignore */
      }
      if (routerRef && window.location.pathname !== '/console/login') {
        routerRef.push('/login')
      }
    }
    return Promise.reject(new Error(message))
  },
)
