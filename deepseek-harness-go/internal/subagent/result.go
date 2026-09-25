package subagent

import (
	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/tool"
)

// SubResult 是一次子 agent 运行的归档结果（result.go 的职责：
// SubResult → parent tool result）。
type SubResult struct {
	Text       string
	Rounds     int
	StopReason string
	Err        error
}

// ToToolResult 把子 agent 结果折叠为父 runner 可用的 tool.Result：
//   - 正常完成 → Ok(final assistant text)
//   - canceled → Err("canceled")（§B.3：ctx cancel → is_error, msg=canceled）
//   - panic / 其他错误 → Err(错误消息)（is_error=true）
func (r SubResult) ToToolResult() tool.Result {
	if r.Err != nil {
		if r.StopReason == "canceled" {
			return tool.Err("subagent canceled")
		}
		return tool.Err("subagent: " + r.Err.Error())
	}
	return tool.Ok(r.Text)
}

// finalAssistantText 从 RunResult 提取最终 assistant 文本。
func finalAssistantText(res agent.RunResult) string {
	for i := len(res.FinalMessages) - 1; i >= 0; i-- {
		m := res.FinalMessages[i]
		if m.Role == llm.RoleAssistant && m.Content != "" {
			return m.Content
		}
	}
	return ""
}
