// stores/ui.ts — UI 偏好 + 全局通知
//
// 管理：theme（light/dark）、locale、通知队列（toast）。
// 持久化：theme + locale 走 localStorage；通知仅在内存。

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { setLocale as setLocaleVal, type Locale, locale as localeRef } from '@/i18n'

export type Theme = 'light' | 'dark'

export interface Toast {
  id: number
  level: 'info' | 'error' | 'success'
  title: string
  message?: string
  ttlMs: number
}

const THEME_KEY = 'dsh.console.theme'

function loadTheme(): Theme {
  try {
    const v = localStorage.getItem(THEME_KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {
    /* ignore */
  }
  return 'light'
}

function applyTheme(theme: Theme) {
  try {
    document.documentElement.dataset.theme = theme
  } catch {
    /* ignore */
  }
}

export const useUIStore = defineStore('ui', () => {
  const theme = ref<Theme>(loadTheme())
  const toasts = ref<Toast[]>([])
  let nextId = 1

  applyTheme(theme.value)

  function setTheme(t: Theme) {
    theme.value = t
    try {
      localStorage.setItem(THEME_KEY, t)
    } catch {
      /* ignore */
    }
    applyTheme(t)
  }

  function toggleTheme() {
    setTheme(theme.value === 'light' ? 'dark' : 'light')
  }

  function setLocale(l: Locale) {
    setLocaleVal(l)
  }

  function pushToast(level: Toast['level'], title: string, message?: string, ttlMs = 4000) {
    const id = nextId++
    toasts.value.push({ id, level, title, message, ttlMs })
    setTimeout(() => {
      toasts.value = toasts.value.filter((t) => t.id !== id)
    }, ttlMs)
  }

  function dismissToast(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  function reportError(err: unknown, title = 'Action failed') {
    const msg = err instanceof Error ? err.message : String(err)
    pushToast('error', title, msg)
  }

  return {
    theme,
    locale: localeRef,
    toasts,
    setTheme,
    toggleTheme,
    setLocale,
    pushToast,
    dismissToast,
    reportError,
  }
})
