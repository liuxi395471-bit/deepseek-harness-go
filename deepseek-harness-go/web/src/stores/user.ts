// stores/user.ts — 登录用户信息 + JWT 存储
//
// 用户登录后调用 setSession(token, user) 同时持久化到 localStorage；
// 应用启动时尝试 loadFromStorage() 自动恢复。
//
// 注意：v8.0 旧的 dsh.console.token 是 plain bearer token；v8.1 起改为
// JWT，存储 key 改为 dsh.console.jwt 以避免混淆。启动时若旧的 token
// 存在但 JWT 不存在，仍兼容（作为 static token fallback）。

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

const TOKEN_KEY = 'dsh.console.jwt'
const LEGACY_TOKEN_KEY = 'dsh.console.token'
const USER_KEY = 'dsh.console.user'

export interface CurrentUser {
  id: number
  username: string
  role: 'admin' | 'user'
  createdAt: string
  lastLoginAt?: string
}

export const useUserStore = defineStore('user', () => {
  const token = ref<string>(loadToken())
  const user = ref<CurrentUser | null>(loadUser())
  const expiresAt = ref<string>(localStorage.getItem('dsh.console.expiresAt') || '')

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  function setSession(t: string, u: CurrentUser, exp: string) {
    token.value = t
    user.value = u
    expiresAt.value = exp
    try {
      localStorage.setItem(TOKEN_KEY, t)
      localStorage.setItem(USER_KEY, JSON.stringify(u))
      localStorage.setItem('dsh.console.expiresAt', exp)
    } catch {
      /* ignore */
    }
  }

  function clear() {
    token.value = ''
    user.value = null
    expiresAt.value = ''
    try {
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem(USER_KEY)
      localStorage.removeItem('dsh.console.expiresAt')
    } catch {
      /* ignore */
    }
  }

  function refreshUser(u: CurrentUser) {
    user.value = u
    try {
      localStorage.setItem(USER_KEY, JSON.stringify(u))
    } catch {
      /* ignore */
    }
  }

  return { token, user, expiresAt, isLoggedIn, isAdmin, setSession, clear, refreshUser }
})

function loadToken(): string {
  try {
    const v = localStorage.getItem(TOKEN_KEY)
    if (v) return v
    // v8.0 fallback: 旧的 static token 仍可用
    const legacy = localStorage.getItem(LEGACY_TOKEN_KEY)
    if (legacy) return legacy
  } catch {
    /* ignore */
  }
  return ''
}

function loadUser(): CurrentUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY)
    if (!raw) return null
    return JSON.parse(raw) as CurrentUser
  } catch {
    return null
  }
}
