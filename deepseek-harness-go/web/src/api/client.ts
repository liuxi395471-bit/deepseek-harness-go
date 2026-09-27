// api/client.ts — axios 实例
//
// 单一 client，所有 /api/v1/console/* 请求都走它；Bearer token
// 从 tokenStore 拿（启动时用户输入或 localStorage 恢复）。

import axios, { type AxiosInstance, type AxiosError } from 'axios'
import { useTokenStore } from '@/stores/token'

export const client: AxiosInstance = axios.create({
  baseURL: '/api/v1/console',
  timeout: 30_000,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((cfg) => {
  const tokenStore = useTokenStore()
  if (tokenStore.token) {
    cfg.headers.Authorization = `Bearer ${tokenStore.token}`
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
    return Promise.reject(new Error(message))
  },
)
