// utils/format.ts — 通用格式化

export function formatRelative(input: string | number | Date): string {
  const t = input instanceof Date ? input.getTime() : Date.parse(String(input))
  if (isNaN(t)) return String(input)
  const diff = (Date.now() - t) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

export function formatTime(input: string | number | Date): string {
  const t = input instanceof Date ? input.getTime() : Date.parse(String(input))
  if (isNaN(t)) return String(input)
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function shortId(s: string, n = 8): string {
  return s.length > n ? s.slice(0, n) : s
}

export function copyToClipboard(text: string): Promise<void> {
  return navigator.clipboard?.writeText(text) ?? Promise.resolve()
}

export function downloadText(filename: string, text: string) {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
