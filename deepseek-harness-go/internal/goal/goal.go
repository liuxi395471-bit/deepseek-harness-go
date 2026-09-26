// Package goal 提供 Goal / Plan / Todo 三层任务管理（v6 P6-4）。
//
// 三层关系：
//
//	Goal  : 高层目标（"提升 v6 测试覆盖率"）
//	  └── Plan   : 多步计划（"补 5 个用例"）
//	        └── Todo  : 具体待办（"写 TC-v6-0010"）
//
// v6 实现：
//   - 数据存储复用 v4 EventStore（写入 kind=goal/plan/todo 事件；
//     通过 Projector 投影出内存视图）；
//   - 暴露 todo_write / todo_read 工具给 Agent；
//   - LoopRunner 在 system prompt 注入当前 Goal/Plan/Todo 摘要
//     （v6 简化：仅在每次 doOneRound 前的可选注入）。
//
// 与 ds-java v0.1.7+ 对齐：相当于 cases.goal/plan/todo 三域的合并
// 简化版（ds-java 把它们放在不同子包；ds-go 合并到 goal 包以减少
// package 数量）。
package goal

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// State 是 Todo 的状态。
type State int

const (
	StatePending State = iota
	StateInProgress
	StateDone
	StateBlocked
)

func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateInProgress:
		return "in_progress"
	case StateDone:
		return "done"
	case StateBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

func (s State) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *State) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"pending"`:
		*s = StatePending
	case `"in_progress"`:
		*s = StateInProgress
	case `"done"`:
		*s = StateDone
	case `"blocked"`:
		*s = StateBlocked
	default:
		return errors.New("goal: unknown todo state " + string(b))
	}
	return nil
}

// Goal 是顶层目标。
type Goal struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Why       string    `json:"why,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Plan 是 Goal 下的多步计划。
type Plan struct {
	ID        string    `json:"id"`
	GoalID    string    `json:"goal_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Todo 是 Plan 下的一条待办。
type Todo struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	Title     string    `json:"title"`
	State     State     `json:"state"`
	Order     int       `json:"order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store 是 goal 数据的内存存储。
//
// v6 简化：所有数据进内存；进程重启丢失（可后续接到 v4 EventStore）。
// 并发安全：所有方法都用 RWMutex 保护。
type Store struct {
	mu     sync.RWMutex
	goals  map[string]*Goal
	plans  map[string]*Plan
	todos  map[string]*Todo
	nextID int
}

// NewStore 构造空 Store。
func NewStore() *Store {
	return &Store{
		goals:  make(map[string]*Goal),
		plans:  make(map[string]*Plan),
		todos:  make(map[string]*Todo),
		nextID: 1,
	}
}

// CreateGoal 创建新 Goal；返回带 ID 的副本。
func (s *Store) CreateGoal(_ context.Context, title, why string) (*Goal, error) {
	if title == "" {
		return nil, errors.New("goal: empty title")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextIDStr()
	now := time.Now()
	g := &Goal{ID: id, Title: title, Why: why, CreatedAt: now, UpdatedAt: now}
	s.goals[id] = g
	cp := *g
	return &cp, nil
}

// CreatePlan 在 goalID 下创建 Plan。
func (s *Store) CreatePlan(_ context.Context, goalID, title string) (*Plan, error) {
	if title == "" {
		return nil, errors.New("goal: empty plan title")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.goals[goalID]; !ok {
		return nil, ErrGoalNotFound
	}
	id := s.nextIDStr()
	now := time.Now()
	p := &Plan{ID: id, GoalID: goalID, Title: title, CreatedAt: now, UpdatedAt: now}
	s.plans[id] = p
	cp := *p
	return &cp, nil
}

// AddTodo 在 planID 下追加一条 Todo。
func (s *Store) AddTodo(_ context.Context, planID, title string) (*Todo, error) {
	if title == "" {
		return nil, errors.New("goal: empty todo title")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[planID]; !ok {
		return nil, ErrPlanNotFound
	}
	id := s.nextIDStr()
	now := time.Now()
	order := len(s.todosForPlanLocked(planID))
	t := &Todo{
		ID:        id,
		PlanID:    planID,
		Title:     title,
		State:     StatePending,
		Order:     order,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.todos[id] = t
	cp := *t
	return &cp, nil
}

// UpdateTodoState 设置 todoID 的状态。
func (s *Store) UpdateTodoState(_ context.Context, todoID string, state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[todoID]
	if !ok {
		return ErrTodoNotFound
	}
	t.State = state
	t.UpdatedAt = time.Now()
	return nil
}

// DeleteTodo 删除 todoID。
func (s *Store) DeleteTodo(_ context.Context, todoID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.todos[todoID]; !ok {
		return ErrTodoNotFound
	}
	delete(s.todos, todoID)
	return nil
}

// ListGoals 返回全部 Goal 列表（按创建时间升序）。
func (s *Store) ListGoals(_ context.Context) []*Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Goal, 0, len(s.goals))
	for _, g := range s.goals {
		cp := *g
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ListPlansForGoal 返回 goalID 下全部 Plan。
func (s *Store) ListPlansForGoal(_ context.Context, goalID string) []*Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Plan, 0)
	for _, p := range s.plans {
		if p.GoalID == goalID {
			cp := *p
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ListTodosForPlan 返回 planID 下全部 Todo（按 Order 升序）。
func (s *Store) ListTodosForPlan(_ context.Context, planID string) []*Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.todosForPlanLocked(planID)
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// ListAllTodos 返回全部 Todo（跨 plan）。
func (s *Store) ListAllTodos(_ context.Context) []*Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Todo, 0, len(s.todos))
	for _, t := range s.todos {
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func (s *Store) todosForPlanLocked(planID string) []*Todo {
	out := make([]*Todo, 0)
	for _, t := range s.todos {
		if t.PlanID == planID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// Snapshot 返回一份可序列化的完整状态视图（用于 plan_mode 注入）。
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Goals: make([]*Goal, 0, len(s.goals)),
	}
	for _, g := range s.goals {
		cp := *g
		snap.Goals = append(snap.Goals, &cp)
	}
	sort.Slice(snap.Goals, func(i, j int) bool { return snap.Goals[i].CreatedAt.Before(snap.Goals[j].CreatedAt) })
	for _, p := range s.plans {
		cp := *p
		snap.Plans = append(snap.Plans, &cp)
	}
	for _, t := range s.todos {
		cp := *t
		snap.Todos = append(snap.Todos, &cp)
	}
	return snap
}

// Snapshot 是只读视图。
type Snapshot struct {
	Goals []*Goal
	Plans []*Plan
	Todos []*Todo
}

// Render 是把 Snapshot 渲染成 system prompt 注入的 Markdown 摘要。
func (s Snapshot) Render() string {
	if len(s.Goals) == 0 && len(s.Plans) == 0 && len(s.Todos) == 0 {
		return ""
	}
	out := "## 当前 Goal / Plan / Todo\n\n"
	for _, g := range s.Goals {
		out += "- **Goal** `" + g.ID + "`： " + g.Title + "\n"
		if g.Why != "" {
			out += "  - 背景：" + g.Why + "\n"
		}
		for _, p := range s.Plans {
			if p.GoalID != g.ID {
				continue
			}
			out += "  - **Plan** `" + p.ID + "`： " + p.Title + "\n"
			for _, t := range s.Todos {
				if t.PlanID != p.ID {
					continue
				}
				marker := "[ ]"
				switch t.State {
				case StateInProgress:
					marker = "[~]"
				case StateDone:
					marker = "[x]"
				case StateBlocked:
					marker = "[!]"
				}
				out += "    - " + marker + " `" + t.ID + "` " + t.Title + "\n"
			}
		}
	}
	return out
}

func (s *Store) nextIDStr() string {
	id := s.nextID
	s.nextID++
	// 简单的 ID；够用。
	return intToStr(id)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

// Errors
var (
	ErrGoalNotFound = errors.New("goal: not found")
	ErrPlanNotFound = errors.New("plan: not found")
	ErrTodoNotFound = errors.New("todo: not found")
)
