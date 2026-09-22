// Recombiner 保存一条正在构建的 assistant 消息的状态。
//
// 关键不变量参见 DESIGN-v2 §A.1.1：
//   - 相同 Index 的 delta 按到达顺序拼接
//   - tool_call 片段按 (ID) 合并，Name/Type 仅填充一次
//   - Final() 是幂等的
//
// 状态：v2 T1.c。并发性：Append 不支持并发调用。
package stream

import (
	"strings"

	"deepseek-harness-go/internal/llm"
)

// Recombiner 将 StreamChunk 流组装为一条 AssistantMessage。
type Recombiner struct {
	index     int
	role      string
	content   strings.Builder
	toolCalls map[string]*llm.ToolCall
	order     []string
	finished  bool
	finish    string
}

// NewRecombiner 返回一个可接收 chunk 的空 Recombiner。
func NewRecombiner() *Recombiner {
	return &Recombiner{toolCalls: map[string]*llm.ToolCall{}}
}

// Append 将一个 chunk 合并进组装状态。当 chunk.Finish 非空时，Recombiner
// 记录终止帧；后续 Append 调用变为无操作（幂等）。
func (r *Recombiner) Append(chunk llm.StreamChunk) {
	if r.finished {
		return
	}
	if r.index == 0 && chunk.Index != 0 {
		r.index = chunk.Index
	}
	if chunk.Text != "" {
		r.content.WriteString(chunk.Text)
	}
	for _, tc := range chunk.ToolCalls {
		r.mergeToolCall(tc)
	}
	if chunk.Finish != "" {
		r.finish = chunk.Finish
		r.finished = true
	}
}

// mergeToolCall 将片段折叠进进行中的工具调用列表。
// 带非空 ID 的片段按 ID 匹配；不带 ID 的片段（少见；OpenAI 通常在第一个
// 片段中带 ID）按 Function.Name 与现有调用匹配，否则丢弃。
func (r *Recombiner) mergeToolCall(tc llm.ToolCall) {
	if tc.ID == "" {
		for _, id := range r.order {
			existing := r.toolCalls[id]
			if existing.Function.Name != "" && existing.Function.Name == tc.Function.Name {
				existing.Function.Arguments += tc.Function.Arguments
				return
			}
		}
		return
	}
	if existing, ok := r.toolCalls[tc.ID]; ok {
		if tc.Function.Name != "" {
			existing.Function.Name = tc.Function.Name
		}
		existing.Function.Arguments += tc.Function.Arguments
		if tc.Type != "" {
			existing.Type = tc.Type
		}
		return
	}
	cp := tc
	r.toolCalls[tc.ID] = &cp
	r.order = append(r.order, tc.ID)
}

// Final 返回组装好的 AssistantMessage。允许在终止 chunk 之前调用；
// 消息反映目前已到达的内容。幂等。
func (r *Recombiner) Final() llm.Message {
	msg := llm.Message{
		Role:    llm.RoleAssistant,
		Content: r.content.String(),
	}
	if len(r.order) > 0 {
		msg.ToolCalls = make([]llm.ToolCall, 0, len(r.order))
		for _, id := range r.order {
			if tc, ok := r.toolCalls[id]; ok {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
			}
		}
	}
	return msg
}

// Finished 报告是否已观察到终止 chunk。
func (r *Recombiner) Finished() bool { return r.finished }

// FinishReason 返回记录的 finish_reason。终止帧之前为空。
func (r *Recombiner) FinishReason() string { return r.finish }
