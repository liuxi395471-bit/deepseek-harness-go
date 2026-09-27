// api/audit.ts — Audit REST 客户端 + 浏览器端 JSONL 下载。

import { client } from './client'

export interface AuditRecord {
  ts: string
  sessionId?: string
  event: string
  round?: number
  tool?: string
  argsHash?: string
  argsRaw?: string
  decision?: string
  source?: string
  model?: string
  method?: string
  path?: string
  status?: number
  durMs?: number
}

export const auditApi = {
  query: async (limit = 100): Promise<AuditRecord[]> => {
    const { data } = await client.get<{ items: AuditRecord[] }>(`/audit?limit=${limit}`)
    return data.items ?? []
  },
  exportJsonlUrl: (limit = 1000): string => {
    const token = localStorage.getItem('dsh.console.token') || ''
    // 通过 fetch 直接下，避免 axios 解析成 JSON。
    return `/api/v1/console/audit/export?limit=${limit}&token=${encodeURIComponent(token)}`
  },
  downloadJsonl: async (limit = 1000): Promise<void> => {
    const token = localStorage.getItem('dsh.console.token') || ''
    const resp = await fetch(`/api/v1/console/audit/export?limit=${limit}`, {
      headers: { Authorization: token ? `Bearer ${token}` : '' },
    })
    if (!resp.ok) {
      throw new Error(`audit export failed: ${resp.status}`)
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `audit-${new Date().toISOString().replace(/[:.]/g, '-')}.jsonl`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  },
}
