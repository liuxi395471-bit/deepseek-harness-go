// Package tools 包含 dsh 内置的工具；v6 P6-3 增加 4 个 jobs_* 工具。
//
// 工具名：
//   - jobs_run    : 启动 Job，返回 {job_id, state}
//   - jobs_list   : 列出 Job（可按 state 过滤）
//   - jobs_output : 读取 Job 输出（since / limit）
//   - jobs_kill   : 取消 Job
//
// 工具共享同一个 jobs.Registry（由 main 装配后通过 SetJobsRegistry 注入）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"deepseek-harness-go/internal/jobs"
	"deepseek-harness-go/internal/tool"
)

// jobsRegistryHolder 持有进程内的 jobs.Registry。
//
// 用 holder 而不是全局 var，便于测试时替换。
type jobsRegistryHolder struct {
	mu sync.RWMutex
	r  *jobs.Registry
}

func (h *jobsRegistryHolder) Get() *jobs.Registry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.r
}

func (h *jobsRegistryHolder) Set(r *jobs.Registry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.r = r
}

// SetJobsRegistry 注入 jobs.Registry；main 在启动时调用一次。
func SetJobsRegistry(r *jobs.Registry) {
	globalJobsRegistry.Set(r)
}

var globalJobsRegistry = &jobsRegistryHolder{}

// JobsRunTool 启动 Job。
type JobsRunTool struct{}

func NewJobsRunTool() *JobsRunTool { return &JobsRunTool{} }
func (*JobsRunTool) Name() string  { return "jobs_run" }
func (*JobsRunTool) Description() string {
	return "启动一个后台 Job；返回 {job_id, state}。"
}
func (*JobsRunTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cmd":     map[string]any{"type": "string", "description": "可执行文件名"},
			"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"code":    map[string]any{"type": "string"},
			"workdir": map[string]any{"type": "string"},
			"owner":   map[string]any{"type": "string"},
		},
		"required": []string{"cmd"},
	}
}

func (*JobsRunTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		Cmd     string   `json:"cmd"`
		Args    []string `json:"args"`
		Code    string   `json:"code"`
		Workdir string   `json:"workdir"`
		Owner   string   `json:"owner"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.Cmd == "" {
		return tool.Err("cmd required"), nil
	}
	reg := globalJobsRegistry.Get()
	if reg == nil {
		return tool.Err("jobs registry unavailable"), nil
	}
	j, err := reg.Submit(ctx, jobs.SubmitRequest{
		Code:    a.Code,
		Cmd:     a.Cmd,
		Args:    a.Args,
		Workdir: a.Workdir,
		Owner:   a.Owner,
	})
	if err != nil {
		return tool.Err(fmt.Sprintf("submit: %s", err.Error())), nil
	}
	return tool.Ok(fmt.Sprintf(`{"job_id":%q,"state":%q}`, j.ID, j.State.String())), nil
}

// JobsListTool 列出 Job。
type JobsListTool struct{}

func NewJobsListTool() *JobsListTool { return &JobsListTool{} }
func (*JobsListTool) Name() string   { return "jobs_list" }
func (*JobsListTool) Description() string {
	return "列出 Job；可按 states/owner/code 过滤。"
}
func (*JobsListTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"states": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"owner":  map[string]any{"type": "string"},
			"code":   map[string]any{"type": "string"},
			"limit":  map[string]any{"type": "integer"},
			"offset": map[string]any{"type": "integer"},
		},
	}
}

func (*JobsListTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		States []string `json:"states"`
		Owner  string   `json:"owner"`
		Code   string   `json:"code"`
		Limit  int      `json:"limit"`
		Offset int      `json:"offset"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	reg := globalJobsRegistry.Get()
	if reg == nil {
		return tool.Err("jobs registry unavailable"), nil
	}
	f := jobs.Filter{Owner: a.Owner, Code: a.Code, Limit: a.Limit, Offset: a.Offset}
	for _, s := range a.States {
		switch s {
		case "pending":
			f.States = append(f.States, jobs.StatePending)
		case "running":
			f.States = append(f.States, jobs.StateRunning)
		case "succeeded":
			f.States = append(f.States, jobs.StateSucceeded)
		case "failed":
			f.States = append(f.States, jobs.StateFailed)
		case "canceled":
			f.States = append(f.States, jobs.StateCanceled)
		default:
			return tool.Err(fmt.Sprintf("unknown state %q", s)), nil
		}
	}
	list, err := reg.List(ctx, f)
	if err != nil {
		return tool.Err(fmt.Sprintf("list: %s", err.Error())), nil
	}
	if list == nil {
		list = []*jobs.Job{}
	}
	b, _ := json.Marshal(map[string]any{"jobs": list, "count": len(list)})
	return tool.Ok(string(b)), nil
}

// JobsOutputTool 读 Job 输出。
type JobsOutputTool struct{}

func NewJobsOutputTool() *JobsOutputTool { return &JobsOutputTool{} }
func (*JobsOutputTool) Name() string     { return "jobs_output" }
func (*JobsOutputTool) Description() string {
	return "读取 Job 输出行；since 起始行号，limit 最多返回多少行（0=全部）。"
}
func (*JobsOutputTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
			"since":  map[string]any{"type": "integer"},
			"limit":  map[string]any{"type": "integer"},
		},
		"required": []string{"job_id"},
	}
}

func (*JobsOutputTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		JobID string `json:"job_id"`
		Since int    `json:"since"`
		Limit int    `json:"limit"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.JobID == "" {
		return tool.Err("job_id required"), nil
	}
	reg := globalJobsRegistry.Get()
	if reg == nil {
		return tool.Err("jobs registry unavailable"), nil
	}
	lines, err := reg.Output(a.JobID, a.Since, a.Limit)
	if err != nil {
		return tool.Err(fmt.Sprintf("output: %s", err.Error())), nil
	}
	if lines == nil {
		lines = []string{}
	}
	b, _ := json.Marshal(map[string]any{"job_id": a.JobID, "lines": lines, "count": len(lines)})
	return tool.Ok(string(b)), nil
}

// JobsKillTool 取消 Job。
type JobsKillTool struct{}

func NewJobsKillTool() *JobsKillTool { return &JobsKillTool{} }
func (*JobsKillTool) Name() string    { return "jobs_kill" }
func (*JobsKillTool) Description() string {
	return "取消一个正在运行的 Job；幂等，已终止返回 already-terminal。"
}
func (*JobsKillTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
		},
		"required": []string{"job_id"},
	}
}

func (*JobsKillTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		JobID string `json:"job_id"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	if a.JobID == "" {
		return tool.Err("job_id required"), nil
	}
	reg := globalJobsRegistry.Get()
	if reg == nil {
		return tool.Err("jobs registry unavailable"), nil
	}
	if err := reg.Kill(ctx, a.JobID); err != nil {
		return tool.Err(fmt.Sprintf("kill: %s", err.Error())), nil
	}
	return tool.Ok(`{"ok":true}`), nil
}
