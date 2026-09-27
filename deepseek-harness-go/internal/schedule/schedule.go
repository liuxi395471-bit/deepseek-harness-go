// Package schedule 提供 v8.1 调度（cron 表达式解析 + worker）。
//
// 设计要点：
//   - cron 表达式：5 字段（minute / hour / dom / month / dow），无秒；
//     与标准 Linux cron 一致。不支持 @yearly/@reboot 等别名（用户场景罕见）。
//   - 解析：自写小解析器（无 robfig/cron 依赖）；
//     支持：* / N / N-M / N,M,... / *\/N 共 4 种通配符；
//     不支持 L / W / #（秒级 cron 不需要）。
//   - 触发：worker 每 30s 扫一次；命中 cron 时执行 action JSON。
//   - action 两种：
//       {"type":"agent_run","prompt":"...","profile":"default"}  → 调 agent runner
//       {"type":"webhook","webhookId":"abc"}                       → 通过 webhook 包 dispatch
package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"deepseek-harness-go/internal/webhook"
)

// Schedule 是 schedules 表的一行（v8.1）。
type Schedule struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Cron       string    `json:"cron"`
	Action     string    `json:"action"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	LastRunAt  time.Time `json:"lastRunAt,omitempty"`
	NextRunAt  time.Time `json:"nextRunAt,omitempty"`
	LastStatus string    `json:"lastStatus,omitempty"` // "ok" | "failed"
	LastError  string    `json:"lastError,omitempty"`
}

// Store 是 schedules 表的 CRUD（内存实现，足够 v8.1）。
//
// 注：v8.1 不做 schedule 持久化（重启后丢失）— 用户可在 v8.2 升级 SQLite；
//      action 在 worker 重启后会自动失效，符合"重启即清"语义。
type Store struct {
	mu    sync.Mutex
	items map[string]*Schedule
	next  int
}

// NewStore 构造空 store。
func NewStore() *Store { return &Store{items: make(map[string]*Schedule), next: 1} }

// ErrNotFound 是查询/更新/删除命中未知 id。
var ErrNotFound = errors.New("schedule: not found")

// List 列出全部 schedule。
func (s *Store) List() []Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Schedule, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, *it)
	}
	return out
}

// Get 按 id 取 schedule。
func (s *Store) Get(id string) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	return *it, nil
}

// Create 新增一条；name / cron / action 必填。
func (s *Store) Create(name, cronExpr, action string) (Schedule, error) {
	if name == "" || cronExpr == "" {
		return Schedule{}, errors.New("name and cron required")
	}
	if _, err := ParseCron(cronExpr); err != nil {
		return Schedule{}, fmt.Errorf("cron: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("sch-%d-%d", time.Now().UnixNano(), s.next)
	s.next++
	it := &Schedule{
		ID:        id,
		Name:      name,
		Cron:      cronExpr,
		Action:    action,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	it.NextRunAt = nextFire(cronExpr, time.Now())
	s.items[id] = it
	return *it, nil
}

// Update 按 id 更新；空字段保留原值。
func (s *Store) Update(id string, name, cronExpr, action string, enabled *bool) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	if cronExpr != "" {
		if _, err := ParseCron(cronExpr); err != nil {
			return Schedule{}, fmt.Errorf("cron: %w", err)
		}
		it.Cron = cronExpr
	}
	if name != "" {
		it.Name = name
	}
	if action != "" {
		it.Action = action
	}
	if enabled != nil {
		it.Enabled = *enabled
	}
	if it.Enabled {
		it.NextRunAt = nextFire(it.Cron, time.Now())
	}
	return *it, nil
}

// Delete 按 id 删除。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}

// MarkRun 更新 lastRunAt / lastStatus / nextRunAt。
func (s *Store) MarkRun(id, status, errStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return
	}
	it.LastRunAt = time.Now()
	it.LastStatus = status
	it.LastError = errStr
	if it.Enabled {
		it.NextRunAt = nextFire(it.Cron, time.Now())
	}
}

// ---------------------------------------------------------------------------
// Worker

// ActionHandler 接受 schedule Action JSON 字符串并执行。
// 返回 error（nil = ok）。
type ActionHandler func(ctx context.Context, actionJSON string) error

// Worker 是定时触发器：每 30s 检查全部 enabled schedule，到点调用 handler。
type Worker struct {
	store   *Store
	handler ActionHandler
}

// NewWorker 构造 worker。
func NewWorker(store *Store, handler ActionHandler) *Worker {
	return &Worker{store: store, handler: handler}
}

// Run 阻塞到 ctx 取消；每 30s 扫一次。
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	now := time.Now()
	for _, sch := range w.store.List() {
		if !sch.Enabled {
			continue
		}
		if sch.NextRunAt.IsZero() || now.Before(sch.NextRunAt) {
			continue
		}
		// 触发
		go func(id, action string) {
			err := w.handler(ctx, action)
			status := "ok"
			errStr := ""
			if err != nil {
				status = "failed"
				errStr = err.Error()
			}
			w.store.MarkRun(id, status, errStr)
		}(sch.ID, sch.Action)
	}
}

// WebhookActionHandler 是 Worker handler 的标准实现：解析 action JSON，
// 根据 type 调对应执行器（webhook.Dispatch / 或仅记日志）。
//
// 注：agent_run 类型在此版本留 placeholder（实际 agent run 由用户手动触发）；
//      Webhook 类型真调 webhook.Dispatch。
func WebhookActionHandler(wh *webhook.Dispatcher) ActionHandler {
	return func(ctx context.Context, actionJSON string) error {
		var a struct {
			Type      string `json:"type"`
			Prompt    string `json:"prompt,omitempty"`
			Profile   string `json:"profile,omitempty"`
			WebhookID string `json:"webhookId,omitempty"`
		}
		if err := json.Unmarshal([]byte(actionJSON), &a); err != nil {
			return fmt.Errorf("action json: %w", err)
		}
		switch a.Type {
		case "agent_run":
			// v8.1 placeholder：仅记录 + 等 v8.2 接入 agent runner。
			return nil
		case "webhook":
			if wh == nil {
				return errors.New("webhook dispatcher not configured")
			}
			payload := fmt.Sprintf(`{"schedule":true,"prompt":%q}`, a.Prompt)
			_, err := wh.Dispatch(ctx, a.WebhookID, payload)
			return err
		default:
			return fmt.Errorf("unknown action type %q", a.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// cron 解析（5 字段：minute hour dom month dow）

// CronField 是单个 cron 字段解析后的"哪些值命中"集合。
type CronField struct {
	allowed map[int]bool
	min, max int
}

// Matches 判定 v 是否在该字段的允许集合中。
func (f *CronField) Matches(v int) bool {
	return f.allowed[v]
}

// ParseCron 解析 5 字段 cron 表达式，返回 5 个字段的 CronField。
//
// 字段范围：minute 0-59，hour 0-23，dom 1-31，month 1-12，dow 0-6（0=Sun）。
func ParseCron(expr string) ([5]*CronField, error) {
	expr = strings.TrimSpace(expr)
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return [5]*CronField{}, fmt.Errorf("cron must have 5 fields (got %d)", len(parts))
	}
	ranges := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	out := [5]*CronField{}
	for i, p := range parts {
		f, err := parseCronField(p, ranges[i][0], ranges[i][1])
		if err != nil {
			return [5]*CronField{}, fmt.Errorf("field %d (%q): %w", i, p, err)
		}
		out[i] = f
	}
	return out, nil
}

func parseCronField(token string, min, max int) (*CronField, error) {
	allowed := make(map[int]bool, max-min+1)
	for _, part := range strings.Split(token, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			s := part[i+1:]
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("bad step %q", s)
			}
			step = n
			part = part[:i]
		}
		var lo, hi int
		if part == "*" {
			lo, hi = min, max
		} else if i := strings.Index(part, "-"); i >= 0 {
			a, err := strconv.Atoi(part[:i])
			if err != nil {
				return nil, fmt.Errorf("bad range start %q", part[:i])
			}
			b, err := strconv.Atoi(part[i+1:])
			if err != nil {
				return nil, fmt.Errorf("bad range end %q", part[i+1:])
			}
			if a < min || b > max || a > b {
				return nil, fmt.Errorf("range %d-%d out of [%d,%d]", a, b, min, max)
			}
			lo, hi = a, b
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("bad token %q", part)
			}
			if n < min || n > max {
				return nil, fmt.Errorf("%d out of [%d,%d]", n, min, max)
			}
			allowed[n] = true
			continue
		}
		for v := lo; v <= hi; v += step {
			allowed[v] = true
		}
	}
	return &CronField{allowed: allowed, min: min, max: max}, nil
}

// nextFire 返回 cron 在 from 之后的下一个命中时刻。
func nextFire(expr string, from time.Time) time.Time {
	fields, err := ParseCron(expr)
	if err != nil {
		return time.Time{}
	}
	// 简化算法：从 from+1分钟开始，递增 1 分钟直到 4 年（防死循环）。
	t := from.Truncate(time.Minute).Add(time.Minute)
	deadline := t.Add(4 * 365 * 24 * time.Hour)
	for t.Before(deadline) {
		if matchAll(fields, t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func matchAll(f [5]*CronField, t time.Time) bool {
	return f[0].Matches(t.Minute()) &&
		f[1].Matches(t.Hour()) &&
		f[2].Matches(t.Day()) &&
		f[3].Matches(int(t.Month())) &&
		f[4].Matches(int(t.Weekday()))
}
