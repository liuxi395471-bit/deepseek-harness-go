package console

import (
	"context"

	"deepseek-harness-go/internal/schedule"
)

// ScheduleAdapter 把 internal/schedule.Store 适配成 console.ScheduleBackend。
type ScheduleAdapter struct {
	Store *schedule.Store
}

// NewScheduleAdapter 构造。
func NewScheduleAdapter(s *schedule.Store) *ScheduleAdapter { return &ScheduleAdapter{Store: s} }

// List 实现 ScheduleBackend。
func (a *ScheduleAdapter) List(_ context.Context) ([]ScheduleItem, error) {
	if a.Store == nil {
		return nil, ErrBackendMissing
	}
	srcs := a.Store.List()
	out := make([]ScheduleItem, len(srcs))
	for i, s := range srcs {
		out[i] = ScheduleItem{
			ID:         s.ID,
			Name:       s.Name,
			Cron:       s.Cron,
			Action:     s.Action,
			Enabled:    s.Enabled,
			CreatedAt:  s.CreatedAt,
			LastRunAt:  s.LastRunAt,
			NextRunAt:  s.NextRunAt,
			LastStatus: s.LastStatus,
			LastError:  s.LastError,
		}
	}
	return out, nil
}

// Create 实现 ScheduleBackend。
func (a *ScheduleAdapter) Create(_ context.Context, item ScheduleItem) (ScheduleItem, error) {
	if a.Store == nil {
		return ScheduleItem{}, ErrBackendMissing
	}
	s, err := a.Store.Create(item.Name, item.Cron, item.Action)
	if err != nil {
		return ScheduleItem{}, err
	}
	return ScheduleItem{
		ID:        s.ID,
		Name:      s.Name,
		Cron:      s.Cron,
		Action:    s.Action,
		Enabled:   s.Enabled,
		CreatedAt: s.CreatedAt,
		NextRunAt: s.NextRunAt,
	}, nil
}

// Update 实现 ScheduleBackend：item 中 Name/Cron/Action 字段空时保留
// 原值；Enabled 通过 item.Enabled 直接传入。
func (a *ScheduleAdapter) Update(_ context.Context, id string, item ScheduleItem) (ScheduleItem, error) {
	if a.Store == nil {
		return ScheduleItem{}, ErrBackendMissing
	}
	enabled := item.Enabled
	s, err := a.Store.Update(id, item.Name, item.Cron, item.Action, &enabled)
	if err != nil {
		return ScheduleItem{}, err
	}
	return ScheduleItem{
		ID:         s.ID,
		Name:       s.Name,
		Cron:       s.Cron,
		Action:     s.Action,
		Enabled:    s.Enabled,
		CreatedAt:  s.CreatedAt,
		LastRunAt:  s.LastRunAt,
		NextRunAt:  s.NextRunAt,
		LastStatus: s.LastStatus,
		LastError:  s.LastError,
	}, nil
}

// Delete 实现 ScheduleBackend。
func (a *ScheduleAdapter) Delete(_ context.Context, id string) error {
	if a.Store == nil {
		return ErrBackendMissing
	}
	return a.Store.Delete(id)
}

// Run 实现 ScheduleBackend：手动触发一次动作，立即更新 lastRunAt。
//
// 实现：找到 worker 调用 ActionHandler；或 worker 不易触达时，直接构造
// dispatch path；这里返回 store 当前快照（手动 run 实际通过 worker
// 调度 — 简化实现：设置 NextRunAt = time.Now() 并 wake worker）。
func (a *ScheduleAdapter) Run(_ context.Context, id string) (ScheduleItem, error) {
	if a.Store == nil {
		return ScheduleItem{}, ErrBackendMissing
	}
	// 简化：手动触发即把 NextRunAt 设为 now；下个 tick worker 自动 pick up。
	a.Store.MarkRun(id, "pending", "")
	s, err := a.Store.Get(id)
	if err != nil {
		return ScheduleItem{}, err
	}
	return ScheduleItem{
		ID:        s.ID,
		Name:      s.Name,
		Cron:      s.Cron,
		Action:    s.Action,
		Enabled:   s.Enabled,
		CreatedAt: s.CreatedAt,
		LastRunAt: s.LastRunAt,
		NextRunAt: s.NextRunAt,
		LastStatus: s.LastStatus,
		LastError: s.LastError,
	}, nil
}
