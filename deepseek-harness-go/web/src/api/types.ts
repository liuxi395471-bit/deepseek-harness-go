/**
 * 后端 API 的类型定义。
 *
 * 字段与 Go 侧严格一一对应（internal/server/server.go、internal/llm/types.go），
 * 修改前请先确认后端契约。
 */

/** llm.Role —— 与 Go 的 llm.Role 常量一致。 */
export type Role = 'system' | 'user' | 'assistant' | 'tool'

/** llm.ToolCallFunc —— 工具调用的函数载荷。 */
export interface ToolCallFunc {
  name: string
  /** JSON 编码的字符串，原样保留（可能是空串或 "null"）。 */
  arguments: string
}

/** llm.ToolCall —— assistant 发起的工具调用请求。 */
export interface ToolCall {
  id: string
  type: string
  function: ToolCallFunc
}

/** llm.Message —— 会话历史中的一条记录。 */
export interface Message {
  role: Role
  content?: string
  tool_calls?: ToolCall[]
  /** 仅 role=tool 时使用，对应 ToolCall.ID。 */
  tool_call_id?: string
  /** 仅 role=tool 时表示对应的工具名。 */
  name?: string
}

/** store.Session —— 会话完整记录（GET /api/sessions/{id}）。 */
export interface SessionDetail {
  id: string
  created_at: string
  updated_at: string
  rounds: number
  preview: string
  messages: Message[]
  usage: Usage
}

/** 会话列表项（GET /api/sessions）。 */
export interface SessionItem {
  id: string
  created_at: string
  updated_at: string
  rounds: number
  preview: string
}

/** llm.Usage —— token 用量。 */
export interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

/** POST /api/agent/message 的请求体。 */
export interface MessageRequest {
  prompt: string
  session_id?: string
}

/** POST /api/agent/message 的响应体。 */
export interface MessageResponse {
  session_id: string
  messages: Message[]
  usage: Record<string, number>
  stop_reason: string
  rounds: number
}

/**
 * SSE 事件负载。
 *
 * 事件名与 internal/server/util.go 的 eventToFrame 一一对应。
 */
export interface SseEvents {
  assistant_delta: { text: string; index: number }
  assistant_message: { content: string; tool_calls: ToolCall[] }
  tool_call_start: { call: ToolCall }
  tool_result: {
    call_id: string
    name: string
    content: string
    is_error: boolean
    took_ms: number
  }
  loop_error: { err: string; phase: string }
  phase_change: { phase: string; at: string }
  loop_done: {
    rounds: number
    stop_reason: string
    /** 新建会话时由服务端分配；后续轮次带上它即可续用同一会话。 */
    session_id: string
    /** 运行失败时的原因；成功时为空串。 */
    error?: string
    usage: { prompt_tokens: number; completion_tokens: number; total_tokens: number }
  }
  unknown: { type: string }
}

export type SseEventName = keyof SseEvents

/** 服务端返回的错误体（internal/server 用 http.Error 返回纯文本）。 */
export interface ApiError {
  status: number
  message: string
}
