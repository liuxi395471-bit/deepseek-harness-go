package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"deepseek-harness-go/internal/tool"
)

// Greet is the simplest possible tool: it returns a Chinese greeting
// for a given name. Used to verify the ReAct loop end-to-end before
// introducing real side-effecting tools.
type Greet struct{}

// NewGreet returns a fresh Greet instance.
func NewGreet() *Greet { return &Greet{} }

func (*Greet) Name() string { return "greet" }

func (*Greet) Description() string {
	return "向指定的人打招呼（用于验证 agent 闭环）。参数 name 为必填字符串。"
}

func (*Greet) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "要打招呼的人名，例如 Ada",
			},
		},
		"required": []string{"name"},
	}
}

func (*Greet) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.Err(fmt.Sprintf("invalid args: %v", err)), nil
	}
	if p.Name == "" {
		return tool.Err("name is required"), nil
	}
	return tool.Ok(fmt.Sprintf("你好，%s！我是 dsh。", p.Name)), nil
}
