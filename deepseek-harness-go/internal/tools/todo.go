// Package tools 包含 dsh 内置的工具；v6 P6-4 增加 todo_* 工具。
//
//   - todo_write : add / update / delete Todo
//   - todo_read  : 列出 Plan 下全部 Todo
//
// 工具共享同一个 goal.Store（main 通过 SetGoalStore 注入）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"deepseek-harness-go/internal/goal"
	"deepseek-harness-go/internal/tool"
)

// goalStoreHolder 持有 goal.Store（v6 P6-4）。
type goalStoreHolder struct {
	mu sync.RWMutex
	s  *goal.Store
}

func (h *goalStoreHolder) Get() *goal.Store {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.s
}

func (h *goalStoreHolder) Set(s *goal.Store) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.s = s
}

// SetGoalStore 注入 goal.Store；main 在启动时调用一次。
func SetGoalStore(s *goal.Store) {
	globalGoalStore.Set(s)
}

var globalGoalStore = &goalStoreHolder{}

// TodoWriteTool 写 Todo（add / update / delete）。
type TodoWriteTool struct{}

func NewTodoWriteTool() *TodoWriteTool { return &TodoWriteTool{} }
func (*TodoWriteTool) Name() string    { return "todo_write" }
func (*TodoWriteTool) Description() string {
	return "写 Todo：add 添加 / update 修改状态 / delete 删除。每次可同时给出多种操作。"
}
func (*TodoWriteTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plan_id": map[string]any{"type": "string", "description": "add 时必填；update / delete 时可省（用 id 即可）"},
			"add": map[string]any{
				"type": "array",
				"items": map[string]any{"type": "string"},
			},
			"update": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":    map[string]any{"type": "string"},
						"state": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done", "blocked"}},
					},
				},
			},
			"delete": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	}
}

func (*TodoWriteTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		PlanID string `json:"plan_id"`
		Add    []string `json:"add"`
		Update []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"update"`
		Delete []string `json:"delete"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	store := globalGoalStore.Get()
	if store == nil {
		return tool.Err("goal store unavailable"), nil
	}

	var added []*goal.Todo
	for _, title := range a.Add {
		if a.PlanID == "" {
			return tool.Err("plan_id required for add"), nil
		}
		t, err := store.AddTodo(ctx, a.PlanID, title)
		if err != nil {
			return tool.Err(fmt.Sprintf("add: %s", err.Error())), nil
		}
		added = append(added, t)
	}

	for _, u := range a.Update {
		var st goal.State
		switch u.State {
		case "pending":
			st = goal.StatePending
		case "in_progress":
			st = goal.StateInProgress
		case "done":
			st = goal.StateDone
		case "blocked":
			st = goal.StateBlocked
		default:
			return tool.Err(fmt.Sprintf("update: unknown state %q", u.State)), nil
		}
		if err := store.UpdateTodoState(ctx, u.ID, st); err != nil {
			return tool.Err(fmt.Sprintf("update: %s", err.Error())), nil
		}
	}

	for _, id := range a.Delete {
		if err := store.DeleteTodo(ctx, id); err != nil {
			return tool.Err(fmt.Sprintf("delete: %s", err.Error())), nil
		}
	}

	b, _ := json.Marshal(map[string]any{
		"added":  added,
		"update": a.Update,
		"delete": a.Delete,
	})
	return tool.Ok(string(b)), nil
}

// TodoReadTool 读 Todo 列表。
type TodoReadTool struct{}

func NewTodoReadTool() *TodoReadTool { return &TodoReadTool{} }
func (*TodoReadTool) Name() string    { return "todo_read" }
func (*TodoReadTool) Description() string {
	return "读 Todo 列表；plan_id 留空时返回全部。"
}
func (*TodoReadTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plan_id": map[string]any{"type": "string"},
		},
	}
}

func (*TodoReadTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a struct {
		PlanID string `json:"plan_id"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Err("invalid args: " + err.Error()), nil
	}
	store := globalGoalStore.Get()
	if store == nil {
		return tool.Err("goal store unavailable"), nil
	}
	var list []*goal.Todo
	if a.PlanID != "" {
		list = store.ListTodosForPlan(ctx, a.PlanID)
	} else {
		list = store.ListAllTodos(ctx)
	}
	if list == nil {
		list = []*goal.Todo{}
	}
	b, _ := json.Marshal(map[string]any{"todos": list, "count": len(list)})
	return tool.Ok(string(b)), nil
}
