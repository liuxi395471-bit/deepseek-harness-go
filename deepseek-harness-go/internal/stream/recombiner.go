// Recombiner 保存一条正在构建的 assistant 消息的状态。
//
// 关键不变量参见 DESIGN-v2 §A.1.1：
//   - 相同 Index 的 delta 按到达顺序拼接
//   - tool_call 片段合并优先级：ID > Index > Name > 追加到最后一条
//   - 当一个片段只带 arguments（无 ID / Index / Name）时，
//     视为"最近一次 tool_call 的续传"，把 arguments 追加过去
//   - Final() 是幂等的
//
// 状态：v2 T1.c。并发性：Append 不支持并发调用。
package stream

import (
	"fmt"
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

// NewRecombiner 返回可接收 chunk 的空 Recombiner。
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
//
// 优先级：
//  1. tc.ID 非空 → 按 ID 匹配 / 新建（ID 是 OpenAI 协议最稳定的 key）
//  2. tc.Index 非 nil（*int 区分了"未提供"与"index=0"）→ 按 "__idx_<i>" 匹配
//  3. tc.Function.Name 非空 → 按 Name 匹配已有同 name 的 entry / 新建
//  4. 否则 → 视为对"最后一条 tool_call"的 arguments 续传
//     （DeepSeek 等 provider 偶尔只发 arguments 字符片段；按 SSE
//     顺序保证它们属于最后一次 ID 已声明的调用）
//  5. 若当前还没有任何 tool_call → 丢弃（无主）
func (r *Recombiner) mergeToolCall(tc llm.ToolCall) {
	switch {
	case tc.ID != "":
		if existing, ok := r.toolCalls[tc.ID]; ok {
			applyToolCallDelta(existing, tc)
			return
		}
		// 边缘场景：上一条 entry 是 arguments-only fragments 拼成的
		// "幻影"（无 name / 无 id），且本 delta 带 id → 视为同一条
		// tool_call 的 ID 补齐，直接合并。
		if n := len(r.order); n > 0 {
			last := r.toolCalls[r.order[n-1]]
			if last != nil && last.Function.Name == "" && last.ID == "" {
				applyToolCallDelta(last, tc)
				// 把 key 从 idx-N 升级为 id
				oldKey := r.order[n-1]
				delete(r.toolCalls, oldKey)
				r.toolCalls[tc.ID] = last
				r.order[n-1] = tc.ID
				return
			}
		}
		cp := tc
		r.toolCalls[tc.ID] = &cp
		r.order = append(r.order, tc.ID)
		return

	case tc.Index != nil:
		key := indexKey(*tc.Index)
		if existing, ok := r.toolCalls[key]; ok {
			applyToolCallDelta(existing, tc)
			return
		}
		// 边缘场景 1：上一条 entry 是 arguments-only fragments 拼成的
		// "幻影"（无 name / 无 id），且本 delta 属于同一 index → 合并。
		if n := len(r.order); n > 0 {
			last := r.toolCalls[r.order[n-1]]
			if last != nil && last.Function.Name == "" && last.ID == "" {
				applyToolCallDelta(last, tc)
				return
			}
		}
		// 边缘场景 2：本 delta 只带 arguments（无 id / 无 name）但
		// index 与已有的"有 name" entry 相同 → 当作续传，追加到那条。
		// 解决 DeepSeek 把"完整 tool_call"和"args-only phantom"
		// 用同一个 index 重复发出来的问题。
		if tc.ID == "" && tc.Function.Name == "" && tc.Function.Arguments != "" {
			for _, key := range r.order {
				ex := r.toolCalls[key]
				if ex == nil || ex.Index == nil || *ex.Index != *tc.Index {
					continue
				}
				if ex.Function.Name == "" {
					continue
				}
				ex.Function.Arguments += tc.Function.Arguments
				return
			}
		}
		cp := tc
		// 用 index 生成伪 id，保证下游一致
		if cp.ID == "" {
			cp.ID = fmt.Sprintf("idx-%d", *cp.Index)
		}
		r.toolCalls[key] = &cp
		r.order = append(r.order, key)
		return

	case tc.Function.Name != "":
		// 已有同 name 的 entry → 合并 arguments（罕见，但兼容）
		for _, key := range r.order {
			if existing := r.toolCalls[key]; existing != nil && existing.Function.Name == tc.Function.Name {
				existing.Function.Arguments += tc.Function.Arguments
				return
			}
		}
		// 新建（用 name 作为 key）
		key := "name:" + tc.Function.Name
		if _, exists := r.toolCalls[key]; !exists {
			cp := tc
			if cp.ID == "" {
				cp.ID = "name:" + tc.Function.Name
			}
			r.toolCalls[key] = &cp
			r.order = append(r.order, key)
			return
		}
		return

	default:
		// arguments-only delta：追加到最后一条 tool_call
		if n := len(r.order); n > 0 {
			last := r.toolCalls[r.order[n-1]]
			if last != nil {
				// 注意：name/id 也可能为空的"伪 fragments"
				// 只把 arguments 续上去；其他字段保留已合并值。
				if delta := tc.Function.Arguments; delta != "" {
					last.Function.Arguments += delta
				}
			}
		}
		// 若还没有任何 tool_call，丢弃
		return
	}
}

// applyToolCallDelta 把片段的内容（name / type / arguments）合并进
// 已有 entry；ID 已有则保留原值。
func applyToolCallDelta(existing *llm.ToolCall, delta llm.ToolCall) {
	if existing == nil {
		return
	}
	if delta.Function.Name != "" && existing.Function.Name == "" {
		existing.Function.Name = delta.Function.Name
	}
	if delta.Type != "" {
		existing.Type = delta.Type
	}
	if delta.ID != "" && existing.ID == "" {
		existing.ID = delta.ID
	}
	if delta.Index != nil {
		existing.Index = delta.Index
	}
	existing.Function.Arguments += delta.Function.Arguments
}

// indexKey 返回 index 维度的合并 key。
func indexKey(i int) string {
	return fmt.Sprintf("__idx_%d", i)
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
