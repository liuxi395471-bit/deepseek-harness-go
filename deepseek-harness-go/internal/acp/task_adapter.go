// Package acp 把 v6 task.Executor 适配到 acp.TaskSubmitter 接口。
//
// 解耦原因：
//   - acp 包依赖 storage；
//   - v6 task 包不依赖 storage / acp；
//   - 因此 acp 自己定义最小 TaskSubmitter 接口，由 main.go 在装配时
//     用这个 adapter 把 task.Executor 桥过去。
package acp

import (
	"context"
	"encoding/json"

	"deepseek-harness-go/internal/task"
)

// TaskExecutorAdapter 把 task.Executor 适配成 acp.TaskSubmitter。
//
// Get / List / Cancel 直接转给 Executor；Submit 把 ACP Send 转换为
// SubmitRequest（profile 兜底为 "headless"，sessionID 可选）。
type TaskExecutorAdapter struct {
	Exec task.Executor
}

// NewTaskExecutorAdapter 构造。
func NewTaskExecutorAdapter(exec task.Executor) *TaskExecutorAdapter {
	return &TaskExecutorAdapter{Exec: exec}
}

// Submit 实现 acp.TaskSubmitter。
func (a *TaskExecutorAdapter) Submit(ctx context.Context, input string) (taskHandle, error) {
	if a.Exec == nil {
		return taskHandle{}, errTaskAdapter("nil executor")
	}
	t, err := a.Exec.Submit(ctx, task.SubmitRequest{
		Title:   "acp",
		Input:   input,
		Profile: "headless",
	})
	if err != nil {
		return taskHandle{}, err
	}
	return convert(t), nil
}

// Get 实现 acp.TaskSubmitter。
func (a *TaskExecutorAdapter) Get(ctx context.Context, id string) (taskHandle, error) {
	if a.Exec == nil {
		return taskHandle{}, errTaskAdapter("nil executor")
	}
	t, err := a.Exec.Get(ctx, id)
	if err != nil {
		return taskHandle{}, err
	}
	return convert(t), nil
}

// Cancel 实现 acp.TaskSubmitter。
func (a *TaskExecutorAdapter) Cancel(ctx context.Context, id string) error {
	if a.Exec == nil {
		return errTaskAdapter("nil executor")
	}
	return a.Exec.Cancel(ctx, id)
}

// List 实现 acp.TaskSubmitter。
func (a *TaskExecutorAdapter) List(ctx context.Context) ([]taskHandle, error) {
	if a.Exec == nil {
		return nil, errTaskAdapter("nil executor")
	}
	ts, err := a.Exec.List(ctx, task.Filter{Limit: 50})
	if err != nil {
		return nil, err
	}
	out := make([]taskHandle, len(ts))
	for i, t := range ts {
		out[i] = convert(t)
	}
	return out, nil
}

// convert 把 *task.Task 映射到 acp 包的内部 taskHandle。
//
// Content / Usage 不是 task 持久化的字段；这里作"任务最后一次结果"留
// 扩展位（v7.0 留空）。
func convert(t *task.Task) taskHandle {
	content := ""
	if t.Error != "" {
		content = t.Error
	}
	usage := map[string]any{
		"prompt_tokens":     t.Usage.PromptTokens,
		"completion_tokens": t.Usage.CompletionTokens,
		"total_tokens":      t.Usage.TotalTokens,
	}
	// 仅用于 fmt 序列化无关的展示
	if buf, err := json.Marshal(usage); err == nil {
		_ = buf
	}
	return taskHandle{
		ID:    t.ID,
		State: t.State.String(),
		Content: content,
		Error: t.Error,
		Usage: usage,
	}
}

type adapterError string

func (e adapterError) Error() string { return string(e) }
func errTaskAdapter(s string) error  { return adapterError("acp: " + s) }
