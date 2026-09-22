package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"deepseek-harness-go/internal/tool"
)

// FsWrite writes content to a path inside workspace. Creates parent
// directories as needed.
//
// WorkspaceRoot must be set; the tool refuses to operate without it.
type FsWrite struct {
	WorkspaceRoot string
}

// NewFsWrite constructs a workspace-bound writer.
func NewFsWrite(workspaceRoot string) *FsWrite { return &FsWrite{WorkspaceRoot: workspaceRoot} }

func (*FsWrite) Name() string { return "fs_write" }

func (*FsWrite) Description() string {
	return "向 workspace 内的文件写入内容；自动创建中间目录。path 不允许越出 workspace。"
}

func (*FsWrite) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "目标文件路径（相对 workspace 或绝对，但必须落在 workspace 内）",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "要写入的内容（覆盖式写入）",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (w *FsWrite) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.Err(fmt.Sprintf("invalid args: %v", err)), nil
	}
	if p.Path == "" {
		return tool.Err("path is required"), nil
	}

	resolved, err := resolveInWorkspace(w.WorkspaceRoot, p.Path)
	if err != nil {
		return tool.Err(err.Error()), nil
	}

	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tool.Err(fmt.Sprintf("mkdir %s: %v", dir, err)), nil
	}
	if err := os.WriteFile(resolved, []byte(p.Content), 0o644); err != nil {
		return tool.Err(fmt.Sprintf("write %s: %v", p.Path, err)), nil
	}

	rel, _ := filepath.Rel(w.WorkspaceRoot, resolved)
	if rel == "" {
		rel = p.Path
	}
	return tool.Ok(fmt.Sprintf("wrote %d bytes to %s", len(p.Content), rel)), nil
}

// MustRegisterBuiltin registers greet, fs_read, fs_write into r.
// Panics on duplicate (programming error).
func MustRegisterBuiltin(r *tool.Registry, workspaceRoot string) {
	r.MustRegister(NewGreet())
	r.MustRegister(NewFsRead(workspaceRoot))
	r.MustRegister(NewFsWrite(workspaceRoot))
}
