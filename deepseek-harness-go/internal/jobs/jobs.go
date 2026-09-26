// Package jobs 提供后端长任务执行（v6 P6-3）。
//
// 一个 Job 是一个"启动起来、跑在后台、可被查询、可被取消"的 shell
// 命令。区别于 v5 shell 工具（同步、短超时、单次返回输出），Job 强调
// 长生命周期 + 多观察者 + 持久化。
//
// 设计：
//   - Registry 持有 map[jobID]*Job + 后台 goroutine 池；
//   - Job 状态：Pending / Running / Succeeded / Failed / Canceled；
//   - 输出采用行缓冲（环形，默认 1000 行），便于 `jobs_output` 查询。
//
// 与 ds-java v0.1.7+ 对齐：相当于 cases.jobs.JobRegistry 的简化版。
package jobs

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State 是 Job 生命周期状态。
type State int

const (
	StatePending State = iota
	StateRunning
	StateSucceeded
	StateFailed
	StateCanceled
)

// String 返回状态名。
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateSucceeded:
		return "succeeded"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// MarshalJSON / UnmarshalJSON 让 State 在 JSON 中以字符串形式出现。
func (s State) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *State) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"pending"`:
		*s = StatePending
	case `"running"`:
		*s = StateRunning
	case `"succeeded"`:
		*s = StateSucceeded
	case `"failed"`:
		*s = StateFailed
	case `"canceled"`:
		*s = StateCanceled
	default:
		return errors.New("jobs: unknown state " + string(b))
	}
	return nil
}

// Job 是单个后台任务。
type Job struct {
	ID         string    `json:"id"`
	Code       string    `json:"code,omitempty"`
	Cmd        string    `json:"cmd"`
	Args       []string  `json:"args,omitempty"`
	Workdir    string    `json:"workdir,omitempty"`
	State      State     `json:"state"`
	Owner      string    `json:"owner,omitempty"`
	ExitCode   int       `json:"exit_code"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Filter 是 List 的查询条件。
type Filter struct {
	State  State // 单状态过滤；StatePending 时跳过（与 Filter{} 等价）
	States []State
	Owner  string
	Code   string
	Limit  int
	Offset int
}

// Store 是 Job 的持久化接口。
//
// v6 实现：MemoryStore；SQLiteStore 留 v6.1。
type Store interface {
	Insert(ctx context.Context, j *Job) error
	Update(ctx context.Context, j *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, filter Filter) ([]*Job, error)
}

// Runner 是 Job 实际执行命令的依赖。
//
// v6 内置 ShellRunner（包装 os/exec）。测试可注入 fake。
type Runner interface {
	Run(ctx context.Context, j *Job, onLine func(string)) (exitCode int, err error)
}

// MemoryStore 是 Store 的进程内实现。
type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]*Job)}
}

func (s *MemoryStore) Insert(_ context.Context, j *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[j.ID]; ok {
		return errors.New("jobs: insert: id already exists")
	}
	cp := *j
	s.jobs[j.ID] = &cp
	return nil
}

func (s *MemoryStore) Update(_ context.Context, j *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[j.ID]; !ok {
		return ErrNotFound
	}
	cp := *j
	s.jobs[j.ID] = &cp
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (s *MemoryStore) List(_ context.Context, filter Filter) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Job
	for _, j := range s.jobs {
		if filter.Owner != "" && j.Owner != filter.Owner {
			continue
		}
		if filter.Code != "" && j.Code != filter.Code {
			continue
		}
		if len(filter.States) > 0 {
			hit := false
			for _, st := range filter.States {
				if j.State == st {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		cp := *j
		out = append(out, &cp)
	}
	// 按 UpdatedAt 降序
	for i := 0; i < len(out); i++ {
		for k := i + 1; k < len(out); k++ {
			if out[k].UpdatedAt.After(out[i].UpdatedAt) {
				out[i], out[k] = out[k], out[i]
			}
		}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	start := filter.Offset
	if start > len(out) {
		start = len(out)
	}
	end := start + limit
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

// ErrNotFound 在 Job 不存在时返回。
var ErrNotFound = errors.New("jobs: not found")

// ErrAlreadyTerminal 在 Kill 一个已终止 Job 时返回。
var ErrAlreadyTerminal = errors.New("jobs: already terminal")

// ErrNoRunner 是 Registry 创建时未传 Runner 的错误。
var ErrNoRunner = errors.New("jobs: runner not configured")
