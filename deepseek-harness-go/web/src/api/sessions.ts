// api/sessions.ts — Sessions REST 客户端
//
// v8.1 起：使用 userStore 取 JWT；新增 eventsSince() 用于 Spill 断点续传。

import { client } from './client'
import { useUserStore } from '@/stores/user'
import type { SessionDetail, SessionItem, SessionFrame } from './types'

export interface ListSessionsResponse {
  items: SessionItem[]
  limit: number
  cursor: string
}

export interface SessionEvent {
  seq: number
  type: number
  ts: string
  payload?: Record<string, unknown>
  actor?: string
}

export interface EventsSinceResponse {
  sid: string
  events: SessionEvent[]
  lastSeq: number
}

export const sessionsApi = {
  list: async (limit = 50, cursor = ''): Promise<ListSessionsResponse> => {
    const { data } = await client.get<ListSessionsResponse>('/sessions', {
      params: { limit, cursor },
    })
    return data
  },

  create: async (title: string, model: string): Promise<SessionItem> => {
    const { data } = await client.post<SessionItem>('/sessions', { title, model })
    return data
  },

  get: async (sid: string): Promise<SessionDetail> => {
    const { data } = await client.get<SessionDetail>(`/sessions/${encodeURIComponent(sid)}`)
    return data
  },

  remove: async (sid: string): Promise<void> => {
    await client.delete(`/sessions/${encodeURIComponent(sid)}`)
  },

  editMessage: async (sid: string, seq: number, content: string): Promise<void> => {
    await client.patch(
      `/sessions/${encodeURIComponent(sid)}/messages/${seq}`,
      { content },
    )
  },

  deleteMessage: async (sid: string, seq: number): Promise<void> => {
    await client.delete(
      `/sessions/${encodeURIComponent(sid)}/messages/${seq}`,
    )
  },

  // v8.1 P1：消息评分（dsh 原生 👍/👎）
  setFeedback: async (
    sid: string,
    seq: number,
    rating: -1 | 0 | 1,
    comment = '',
  ): Promise<void> => {
    await client.post(
      `/sessions/${encodeURIComponent(sid)}/messages/${seq}/feedback`,
      { rating, comment },
    )
  },

  // v8.1 P1：重生成 user 消息对应的 assistant 回复（返回 SSE stream）
  regenerate: async (
    sid: string,
    seq: number,
    onFrame: (f: SessionFrame) => void,
    signal?: AbortSignal,
  ): Promise<void> => {
    const token = useUserStore().token || ''
    const resp = await fetch(
      `/api/v1/console/sessions/${encodeURIComponent(sid)}/messages/${seq}/regenerate`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: token ? `Bearer ${token}` : '',
        },
        signal,
      },
    )
    if (!resp.ok || !resp.body) {
      throw new Error(`regenerate failed (${resp.status})`)
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      let idx
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const frame = buf.slice(0, idx)
        buf = buf.slice(idx + 2)
        const line = frame.replace(/^data:\s*/, '').trim()
        if (!line) continue
        try {
          onFrame(JSON.parse(line) as SessionFrame)
        } catch {
          /* skip */
        }
      }
    }
  },

  // v8.1 P1：导出（md / jsonl）返回 Blob URL
  exportMd: async (sid: string): Promise<Blob> => {
    const resp = await client.get<Blob>(
      `/sessions/${encodeURIComponent(sid)}/export`,
      { params: { format: 'md' }, responseType: 'blob' },
    )
    return resp.data
  },

  exportJsonl: async (sid: string): Promise<Blob> => {
    const resp = await client.get<Blob>(
      `/sessions/${encodeURIComponent(sid)}/export`,
      { params: { format: 'jsonl' }, responseType: 'blob' },
    )
    return resp.data
  },

  /**
   * eventsSince 返回 sid 中 seq > since 的 events（升序）；不存在时返回 null。
   * 用于 v8.1 Spill 断点续传。
   */
  eventsSince: async (sid: string, since: number): Promise<EventsSinceResponse | null> => {
    try {
      const { data, status } = await client.get<EventsSinceResponse>(
        `/sessions/${encodeURIComponent(sid)}/events`,
        { params: { since }, validateStatus: () => true },
      )
      if (status === 503) return null // backend 不支持 Spill
      return data
    } catch {
      return null
    }
  },

  /**
   * 流式发送消息：返回 ReadableStream<SessionFrame>（前端按 SSE 解析）。
   * fetch API 直接拿到 ReadableStream，不走 axios（SSE 友好）。
   */
  sendStream: async (
    sid: string,
    content: string,
    onFrame: (f: SessionFrame) => void,
    signal?: AbortSignal,
  ): Promise<void> => {
    const token = useUserStore().token || ''
    const resp = await fetch(
      `/api/v1/console/sessions/${encodeURIComponent(sid)}/messages`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: token ? `Bearer ${token}` : '',
        },
        body: JSON.stringify({ content }),
        signal,
      },
    )
    if (!resp.ok || !resp.body) {
      const text = await resp.text().catch(() => '')
      throw new Error(`send failed (${resp.status}): ${text}`)
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      // SSE: 一帧以 "\n\n" 结尾
      let idx
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const frame = buf.slice(0, idx)
        buf = buf.slice(idx + 2)
        const line = frame.replace(/^data:\s*/, '').trim()
        if (!line) continue
        try {
          const parsed = JSON.parse(line) as SessionFrame
          onFrame(parsed)
        } catch {
          /* 跳过非 JSON 行 */
        }
      }
    }
  },
}
