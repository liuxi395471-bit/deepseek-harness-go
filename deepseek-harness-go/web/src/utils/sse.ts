// utils/sse.ts — 带 since_seq 的 SSE 客户端
//
// EventSource 原生 API 不支持自定义 Authorization header 与请求 body；
// 这里用 fetch + ReadableStream 实现自己的 SSE 解析器，并支持：
//   - Authorization 注入（从 userStore 取 JWT）
//   - since_seq query 参数（断点续传）
//   - 自动重连（onerror 时 backoff 1s → 2s → 4s → ... max 30s）
//   - onMessage / onError / onOpen 回调

import { useUserStore } from '@/stores/user'

export interface SSERequest {
  url: string
  sinceSeq?: number
  signal?: AbortSignal
  onMessage: (data: string, event: string) => void
  onError?: (err: Error) => void
  onOpen?: () => void
  onReconnect?: (attempt: number) => void
}

export interface SSEHandle {
  /** Stop the connection (idempotent). */
  close(): void
}

export function openSSE(req: SSERequest): SSEHandle {
  let stopped = false
  let attempt = 0
  let ctrl: AbortController | null = null

  function buildUrl(): string {
    const u = new URL(req.url, window.location.origin)
    if (req.sinceSeq !== undefined && req.sinceSeq >= 0) {
      u.searchParams.set('since', String(req.sinceSeq))
    }
    return u.toString()
  }

  async function runOnce() {
    if (stopped) return
    if (attempt > 0 && req.onReconnect) req.onReconnect(attempt)

    ctrl = new AbortController()
    const onAbort = () => ctrl?.abort()
    req.signal?.addEventListener('abort', onAbort, { once: true })

    try {
      const token = useUserStore().token || ''
      const resp = await fetch(buildUrl(), {
        headers: {
          Accept: 'text/event-stream',
          Authorization: token ? `Bearer ${token}` : '',
        },
        signal: ctrl.signal,
      })
      if (!resp.ok || !resp.body) {
        throw new Error(`SSE ${resp.status} ${resp.statusText}`)
      }
      attempt = 0 // reset on success
      req.onOpen?.()

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      while (!stopped) {
        const { value, done } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // 解析 SSE 帧（\n\n 分隔）
        let idx: number
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          let event = 'message'
          const dataLines: string[] = []
          for (const line of frame.split('\n')) {
            if (line.startsWith('event:')) {
              event = line.slice(6).trim()
            } else if (line.startsWith('data:')) {
              dataLines.push(line.slice(5).trim())
            }
          }
          if (dataLines.length > 0) {
            try {
              req.onMessage(dataLines.join('\n'), event)
            } catch (e) {
              req.onError?.(e as Error)
            }
          }
        }
      }
    } catch (e) {
      if (stopped) return
      req.onError?.(e as Error)
    } finally {
      req.signal?.removeEventListener('abort', onAbort)
      ctrl = null
    }

    // 退出循环后做 backoff 重连
    if (!stopped) {
      attempt++
      const wait = Math.min(30_000, 1000 * Math.pow(2, Math.min(attempt - 1, 5)))
      setTimeout(runOnce, wait)
    }
  }

  runOnce()

  return {
    close() {
      stopped = true
      ctrl?.abort()
    },
  }
}
