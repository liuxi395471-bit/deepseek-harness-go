// api/sessions.ts — Sessions REST 客户端

import { client } from './client'
import type { SessionDetail, SessionItem, SessionFrame } from './types'

export interface ListSessionsResponse {
  items: SessionItem[]
  limit: number
  cursor: string
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
    const token = localStorage.getItem('dsh.console.token') || ''
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
