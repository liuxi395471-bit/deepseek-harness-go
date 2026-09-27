// stores/token.ts — Bearer token 持久化
//
// 用户在登录页（/login 隐式：第一次未配置时）输入 token；
// localStorage 持久化；供 axios interceptor 读取。

import { defineStore } from 'pinia'
import { ref } from 'vue'

const STORAGE_KEY = 'dsh.console.token'

export const useTokenStore = defineStore('token', () => {
  const token = ref<string>(loadInitial())

  function setToken(v: string) {
    token.value = v
    if (v) {
      localStorage.setItem(STORAGE_KEY, v)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }

  function clearToken() {
    setToken('')
  }

  return { token, setToken, clearToken }
})

function loadInitial(): string {
  // 1. localStorage（用户已登录）
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v) return v
  } catch {
    /* SSR / localStorage 禁用 */
  }
  // 2. Vite 构建期注入（开发期 .env.local）
  const env = (import.meta as any).env?.VITE_AUTH_TOKEN
  if (typeof env === 'string' && env) return env
  return ''
}
