package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"deepseek-harness-go/internal/tool"
)

// miniTool 是仅用于测试 system prompt 组装的 stub。
type miniTool struct{ name, desc string }

func (m *miniTool) Name() string        { return m.name }
func (m *miniTool) Description() string { return m.desc }
func (m *miniTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*miniTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Ok(""), nil
}

func TestDefaultSystemPrompt_OverrideWins(t *testing.T) {
	d := DefaultSystemPrompt{Override: "hi"}
	if got := d.Build(tool.NewRegistry()); got != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestDefaultSystemPrompt_ListsRegisteredTools(t *testing.T) {
	r := tool.NewRegistry()
	r.MustRegister(&miniTool{name: "alpha", desc: "first tool"})
	r.MustRegister(&miniTool{name: "beta", desc: "second tool"})

	got := DefaultSystemPrompt{}.Build(r)
	for _, want := range []string{"alpha", "beta", "first tool", "second tool", "ReAct"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestDefaultSystemPrompt_EmptyRegistryHasNoBullets(t *testing.T) {
	got := DefaultSystemPrompt{}.Build(tool.NewRegistry())
	if strings.Contains(got, "- ") {
		t.Errorf("expected no tool bullets, got:\n%s", got)
	}
}
