// api/types.ts — 与 v8 Console API 对齐的 DTO

export interface SessionItem {
  sid: string
  title: string
  model: string
  createdAt: string
  updatedAt: string
  rounds: number
  preview: string
}

export interface SessionDetail extends SessionItem {
  messages: Array<{
    role: 'system' | 'user' | 'assistant' | 'tool'
    content?: string
    tool_calls?: Array<{
      id: string
      type: string
      function: { name: string; arguments: string }
    }>
    tool_call_id?: string
    name?: string
  }>
  usage: {
    promptTokens: number
    completionTokens: number
    totalTokens: number
  }
}

export interface PluginItem {
  name: string
  kind: string
  source: string
  version?: string
  state: string
  healthy: boolean
  tools: string[]
  lastError?: string
}

export interface ModelItem {
  channel: string
  model: string
  protocol: string
  active: boolean
  baseUrl?: string
  timeoutMs?: number
  lastTestedAt?: string
}

export interface PingResult {
  ok: boolean
  latencyMs: number
  sample?: string
  error?: string
}

export interface TaskItem {
  id: string
  code?: string
  title: string
  state: string
  profile: string
  owner?: string
  createdAt: string
  updatedAt: string
  startedAt?: string
  finishedAt?: string
  error?: string
  progress?: {
    jobId: string
    lines: number
    status: string
  }
}

export interface ApprovalItem {
  id: string
  tool: string
  args: Record<string, unknown>
  profile: string
  reason?: string
  createdAt: string
}

export type Decision = 'allow' | 'deny' | 'always'

export interface SessionFrame {
  event: string
  data: Record<string, unknown>
}

export interface ApiError {
  error: { code: string; message: string }
}
