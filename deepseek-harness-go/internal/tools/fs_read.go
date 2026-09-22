package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"deepseek-harness-go/internal/tool"
)

const defaultFsReadMaxBytes int64 = 1 << 20 // 1 MiB

// FsRead reads a UTF-8 (or binary, length-only) file under workspace.
//
// WorkspaceRoot must be set; passing "" causes every read to fail with a
// configuration error rather than silently reading the host filesystem.
type FsRead struct {
	WorkspaceRoot string
}

// NewFsRead constructs a workspace-bound reader.
func NewFsRead(workspaceRoot string) *FsRead { return &FsRead{WorkspaceRoot: workspaceRoot} }

func (*FsRead) Name() string { return "fs_read" }

func (*FsRead) Description() string {
	return "读取 workspace 内的文本文件，返回内容（默认最大 1 MiB，可通过 max_bytes 覆盖）。"
}

func (*FsRead) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "文件路径（相对 workspace 或绝对，但必须落在 workspace 内）",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "可选，最大读取字节数（默认 1048576 = 1 MiB）",
			},
		},
		"required": []string{"path"},
	}
}

func (r *FsRead) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var p struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.Err(fmt.Sprintf("invalid args: %v", err)), nil
	}
	if p.Path == "" {
		return tool.Err("path is required"), nil
	}
	max := p.MaxBytes
	if max <= 0 {
		max = defaultFsReadMaxBytes
	}

	resolved, err := resolveInWorkspace(r.WorkspaceRoot, p.Path)
	if err != nil {
		return tool.Err(err.Error()), nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return tool.Err(fmt.Sprintf("file not found: %s", p.Path)), nil
		}
		return tool.Err(fmt.Sprintf("stat %s: %v", p.Path, err)), nil
	}
	if info.IsDir() {
		return tool.Err(fmt.Sprintf("is a directory: %s", p.Path)), nil
	}
	if info.Size() > max {
		return tool.Err(fmt.Sprintf("file too large (%d > %d bytes); raise max_bytes if you really need it", info.Size(), max)), nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return tool.Err(fmt.Sprintf("read %s: %v", p.Path, err)), nil
	}
	return tool.Ok(string(data)), nil
}
