package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/approval"
	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/jobs"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/plugin/installer"
	"deepseek-harness-go/internal/runtime"
	"deepseek-harness-go/internal/server"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/task"
)

// adapter.go 把 v3-v7 的真实 backend 接到 console.Deps 上。
//
// 设计：保持 console 包不依赖具体的内部包（避免 import 循环与耦
// 合），让 main.go 显式构造各 adapter。

// SessionsAdapter 把 store.Store + agent.StreamingRunner 接到 SessionBackend。
type SessionsAdapter struct {
	Store  store.Store
	Runner agent.StreamingRunner
	// ChannelResolver 解析 "model" 字段 → 实际 LLM model 名（v5）。
	Channels *runtime.MemoryRegistry
}

// List 列出 store 里的全部 session（按 UpdatedAt 倒序）。
func (a *SessionsAdapter) List(ctx context.Context, limit int, _ string) ([]SessionItem, error) {
	if a.Store == nil {
		return nil, nil
	}
	list, err := a.Store.List(ctx, limit, 0)
	if err != nil {
		return nil, err
	}
	items := make([]SessionItem, len(list))
	for i, sess := range list {
		items[i] = SessionItem{
			SID:       sess.ID,
			Title:     titleFromPreview(sess.Preview),
			Model:     "",
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
			Rounds:    sess.Rounds,
			Preview:   sess.Preview,
		}
	}
	return items, nil
}

// Get 加载完整会话。
func (a *SessionsAdapter) Get(ctx context.Context, sid string) (SessionDetail, error) {
	if a.Store == nil {
		return SessionDetail{}, ErrSessionMissing
	}
	sess, err := a.Store.Load(ctx, sid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return SessionDetail{}, ErrSessionNotFound
		}
		return SessionDetail{}, err
	}
	d := SessionDetail{
		SessionItem: SessionItem{
			SID:       sess.ID,
			Title:     titleFromPreview(sess.Preview),
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
			Rounds:    sess.Rounds,
			Preview:   sess.Preview,
		},
		Messages: msgsToAny(sess.Messages),
	}
	d.Usage.PromptTokens = sess.UsageTotal.PromptTokens
	d.Usage.CompletionTokens = sess.UsageTotal.CompletionTokens
	d.Usage.TotalTokens = sess.UsageTotal.TotalTokens
	return d, nil
}

// Create 分配新 sid 并插入空行。
func (a *SessionsAdapter) Create(ctx context.Context, title, model string) (SessionItem, error) {
	if a.Store == nil {
		return SessionItem{}, errors.New("sessions adapter: store nil")
	}
	sess, err := a.Store.Begin(ctx)
	if err != nil {
		return SessionItem{}, err
	}
	now := time.Now()
	return SessionItem{
		SID:       sess.ID,
		Title:     title,
		Model:     model,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: now,
	}, nil
}

// Delete 真删除会话（v8 P0 引入）：把请求转发到 store.DeleteSession。
//
// 旧实现仅为 best-effort no-op，让 UI "看起来成功"；v8 P0 起改为
// 调用底层 Store.DeleteSession（MapStore / SQLiteStore 都已实现）。
func (a *SessionsAdapter) Delete(ctx context.Context, sid string) error {
	if a.Store == nil {
		return ErrSessionMissing
	}
	if err := a.Store.DeleteSession(ctx, sid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrSessionNotFound
		}
		return err
	}
	return nil
}

// EditMessage 透传到 store.EditMessage（v8 P0）。
func (a *SessionsAdapter) EditMessage(ctx context.Context, sid string, msgSeq int64, newContent string) error {
	if a.Store == nil {
		return ErrSessionMissing
	}
	if err := a.Store.EditMessage(ctx, sid, msgSeq, newContent); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return ErrSessionNotFound
		case errors.Is(err, store.ErrMessageNotFound):
			return ErrMessageNotFound
		case errors.Is(err, store.ErrMessageNotEditable):
			return ErrMessageNotEditable
		default:
			return err
		}
	}
	return nil
}

// DeleteMessage 透传到 store.DeleteMessage（v8 P0）。
func (a *SessionsAdapter) DeleteMessage(ctx context.Context, sid string, msgSeq int64) error {
	if a.Store == nil {
		return ErrSessionMissing
	}
	if err := a.Store.DeleteMessage(ctx, sid, msgSeq); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return ErrSessionNotFound
		case errors.Is(err, store.ErrMessageNotFound):
			return ErrMessageNotFound
		default:
			return err
		}
	}
	return nil
}

// Send 同步阻塞版本（v8 控制台默认走 SendStream；Send 留作未来用）。
func (a *SessionsAdapter) Send(ctx context.Context, sid, content string) (SendResult, error) {
	if a.Runner == nil {
		return SendResult{}, errors.New("sessions adapter: runner nil")
	}
	evs, resCh := a.Runner.RunStream(ctx, content, sid)
	for range evs {
	}
	res := <-resCh
	if res.Error != nil {
		return SendResult{SessionID: sid, StopReason: "error"}, res.Error
	}
	return SendResult{
		SessionID:  res.SessionID,
		Rounds:     res.Rounds,
		StopReason: res.StopReason,
	}, nil
}

// SendStream 把 agent.Event 转换成 SessionFrame。
func (a *SessionsAdapter) SendStream(ctx context.Context, sid, content string, out chan<- SessionFrame) error {
	if a.Runner == nil {
		return errors.New("sessions adapter: runner nil")
	}
	evs, resCh := a.Runner.RunStream(ctx, content, sid)
	for ev := range evs {
		f := eventToFrame(ev)
		if f.Event == "" {
			continue
		}
		select {
		case out <- f:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}
	out <- SessionFrame{
		Event: "loop_done",
		Data: map[string]any{
			"rounds":      res.Rounds,
			"stopReason":  res.StopReason,
			"sessionId":   res.SessionID,
		},
	}
	return nil
}

// eventToFrame 把 agent.Event 翻译为 SessionFrame。
func eventToFrame(ev agent.Event) SessionFrame {
	switch v := ev.(type) {
	case agent.AssistantDelta:
		return SessionFrame{
			Event: "assistant_delta",
			Data:  map[string]any{"text": v.Text},
		}
	case agent.AssistantMessage:
		return SessionFrame{
			Event: "assistant_message",
			Data: map[string]any{
				"content":    v.Content,
				"toolCalls":  v.ToolCalls,
			},
		}
	case agent.ToolCallStart:
		return SessionFrame{
			Event: "tool_call_start",
			Data:  map[string]any{"call": v.Call},
		}
	case agent.ToolResult:
		return SessionFrame{
			Event: "tool_result",
			Data: map[string]any{
				"callId":  v.CallID,
				"name":    v.Name,
				"content": v.Content,
				"isError": v.IsError,
				"tookMs":  v.Took.Milliseconds(),
			},
		}
	case agent.LoopError:
		return SessionFrame{
			Event: "loop_error",
			Data:  map[string]any{"err": v.Err.Error(), "phase": v.Phase.String()},
		}
	case agent.PhaseChange:
		return SessionFrame{
			Event: "phase_change",
			Data:  map[string]any{"phase": v.Phase.String()},
		}
	case agent.LoopDone:
		return SessionFrame{
			Event: "loop_done",
			Data:  map[string]any{"rounds": v.Rounds, "stopReason": "no_tool_calls"},
		}
	}
	return SessionFrame{}
}

func titleFromPreview(p string) string {
	if p == "" {
		return "未命名会话"
	}
	if len(p) > 32 {
		return p[:32] + "…"
	}
	return p
}

func msgsToAny(msgs []llm.Message) []any {
	out := make([]any, len(msgs))
	for i, m := range msgs {
		out[i] = m
	}
	return out
}

// --- Plugins adapter ---

// PluginsAdapter 把 plugin.Inventory + installer.StatusStore 接到 PluginBackend。
type PluginsAdapter struct {
	Inventory   pluginInventory // 抽象的 Inventory，避免 console → server import
	Statuses    *installer.StatusStore
	InstallRoot string
	// Toggle 是启停实现；默认 stub（只改 status，不重启）。
	Toggle func(ctx context.Context, name string, enabled bool) error
}

// pluginInventory 是 console 包对 plugin 的最小抽象。
//
// 真实实现由 main.go 用 server 包的 plugin.Inventory 适配。
type pluginInventory interface {
	List(ctx context.Context) ([]inventoryEntry, error)
}

type inventoryEntry struct {
	Name    string
	Kind    string
	Source  string
	Version string
	Healthy bool
	Tools   []string
}

// InventoryEntry 是 inventoryEntry 的导出别名，供 cmd/dsh 装配用。
type InventoryEntry = inventoryEntry

// List 聚合 inventory + status。
func (a *PluginsAdapter) List(ctx context.Context) ([]PluginItem, error) {
	items := []PluginItem{}
	if a.Inventory != nil {
		entries, err := a.Inventory.List(ctx)
		if err == nil {
			for _, e := range entries {
				items = append(items, PluginItem{
					Name:    e.Name,
					Kind:    e.Kind,
					Source:  e.Source,
					Version: e.Version,
					State:   "loaded",
					Healthy: e.Healthy,
					Tools:   e.Tools,
				})
			}
		}
	}
	if a.Statuses != nil {
		// status 覆盖 loaded（disabled / failed）
		for _, st := range a.Statuses.All() {
			found := false
			for i := range items {
				if items[i].Name == st.Name {
					items[i].State = string(st.State)
					if st.Message != "" {
						items[i].LastError = st.Message
						items[i].Healthy = false
					}
					found = true
					break
				}
			}
			if !found {
				items = append(items, PluginItem{
					Name: st.Name,
					State: string(st.State),
					LastError: st.Message,
				})
			}
		}
	}
	return items, nil
}

// Enable 改 status + 调 Toggle（可选）。
func (a *PluginsAdapter) Enable(ctx context.Context, name string) error {
	if a.Statuses == nil {
		return errors.New("plugins adapter: status store nil")
	}
	st, _ := a.Statuses.Get(name)
	if st == nil {
		st = &installer.Status{Name: name}
	}
	st.State = installer.StateLoaded
	st.Message = ""
	now := time.Now().UTC().Format(time.RFC3339)
	st.At = now
	if err := a.Statuses.Set(st); err != nil {
		return err
	}
	if a.Toggle != nil {
		return a.Toggle(ctx, name, true)
	}
	return nil
}

// Disable 同上。
func (a *PluginsAdapter) Disable(ctx context.Context, name string) error {
	if a.Statuses == nil {
		return errors.New("plugins adapter: status store nil")
	}
	st, _ := a.Statuses.Get(name)
	if st == nil {
		st = &installer.Status{Name: name}
	}
	st.State = installer.StateDisabled
	now := time.Now().UTC().Format(time.RFC3339)
	st.At = now
	if err := a.Statuses.Set(st); err != nil {
		return err
	}
	if a.Toggle != nil {
		return a.Toggle(ctx, name, false)
	}
	return nil
}

// Install 扫描 installRoot → 在 Inventory 里尝试加载 name → 写 status。
//
// 失败场景：
//   - name 已在 Statuses 中存在且 state = loaded/disabled：返回 ErrPluginExists；
//   - 磁盘上没找到：返回 ErrPluginNotFound；
//   - 扫描错误（如 installRoot 不存在）：原样返回。
func (a *PluginsAdapter) Install(ctx context.Context, name, _ string) error {
	if a.Statuses == nil {
		return errors.New("plugins adapter: status store nil")
	}
	if st, ok := a.Statuses.Get(name); ok && (st.State == installer.StateLoaded || st.State == installer.StateDisabled) {
		return ErrPluginExists
	}
	if a.InstallRoot == "" {
		return errors.New("plugins adapter: installRoot not set")
	}
	sc := installer.NewScanner()
	entries, err := sc.Scan(a.InstallRoot)
	if err != nil {
		return fmt.Errorf("plugins adapter: scan: %w", err)
	}
	var found *installer.Entry
	for _, e := range entries {
		if e.Name == name {
			found = e
			break
		}
	}
	if found == nil {
		return ErrPluginNotFound
	}
	st := &installer.Status{
		Name:    name,
		State:   installer.StateDiscovered,
		At:      time.Now().UTC().Format(time.RFC3339),
		Message: "installed via console",
	}
	return a.Statuses.Set(st)
}

// Uninstall 标记 name 为 uninstalled + 从 Statuses 移除（保留磁盘源，便于恢复）。
//
// name 不存在返回 ErrPluginNotFound。
func (a *PluginsAdapter) Uninstall(ctx context.Context, name string) error {
	if a.Statuses == nil {
		return errors.New("plugins adapter: status store nil")
	}
	st, ok := a.Statuses.Get(name)
	if !ok {
		return ErrPluginNotFound
	}
	st.State = installer.StateUninstalled
	st.Message = "uninstalled via console"
	st.At = time.Now().UTC().Format(time.RFC3339)
	return a.Statuses.Set(st)
}

// --- Models adapter ---

// ModelsAdapter 把 runtime.Registry 接到 ModelBackend。
//
// v8.0 写时只改内存中的 Registry；持久化由 v8.1 接 YAML 完成。
type ModelsAdapter struct {
	Registry *runtime.MemoryRegistry
}

// List 把 registry 转换成 []ModelItem。
func (a *ModelsAdapter) List(_ context.Context) ([]ModelItem, error) {
	if a.Registry == nil {
		return []ModelItem{}, nil
	}
	_ = a.Registry.Codes() // 仅触达；具体细节从 entry 内部拿
	out := []ModelItem{}
	for _, code := range a.Registry.Codes() {
		// 从 entry 拿 baseUrl / apiKey / 真实 model 名（runtime 私有字段，
		// 但 ResolveModel / Resolve 已暴露）。
		model, _ := a.Registry.ResolveModel(code)
		out = append(out, ModelItem{
			Channel:  code,
			Model:    model,
			Protocol: inferProtocol(code),
			Active:   true,
		})
	}
	return out, nil
}

// Update v8.0：Upsert 到 Registry（replace=true）。
func (a *ModelsAdapter) Update(_ context.Context, channel string, item ModelItem) error {
	if a.Registry == nil {
		return errors.New("models adapter: registry nil")
	}
	if channel == "" {
		return ErrBackendMissing
	}
	cfg := config.LLMConfig{
		Provider: item.Protocol,
		BaseURL:  item.BaseURL,
		Model:    item.Model,
		MaxTokens: 8192,
		Timeout:  120 * time.Second,
	}
	return a.Registry.Upsert(channel, cfg, true)
}

// Create 新增渠道；channel 已存在返回 ErrModelExists。
func (a *ModelsAdapter) Create(_ context.Context, item ModelItem) error {
	if a.Registry == nil {
		return errors.New("models adapter: registry nil")
	}
	if item.Channel == "" {
		return ErrBackendMissing
	}
	cfg := config.LLMConfig{
		Provider: item.Protocol,
		BaseURL:  item.BaseURL,
		Model:    item.Model,
		MaxTokens: 8192,
		Timeout:  120 * time.Second,
	}
	if err := a.Registry.Upsert(item.Channel, cfg, false); err != nil {
		if strings.Contains(err.Error(), "duplicate channel") {
			return ErrModelExists
		}
		if strings.Contains(err.Error(), "unknown channel") {
			return ErrModelNotFound
		}
		return err
	}
	return nil
}

// Remove 卸载渠道。
func (a *ModelsAdapter) Remove(_ context.Context, channel string) error {
	if a.Registry == nil {
		return errors.New("models adapter: registry nil")
	}
	if err := a.Registry.Remove(channel); err != nil {
		if strings.Contains(err.Error(), "unknown channel") {
			return ErrModelNotFound
		}
		return err
	}
	return nil
}

// Ping 调一次 llm.Client.Chat() 做空 probe。
func (a *ModelsAdapter) Ping(ctx context.Context, channel string) (PingResult, error) {
	if a.Registry == nil {
		return PingResult{OK: false, Error: "registry nil"}, nil
	}
	client, err := a.Registry.Resolve(channel)
	if err != nil {
		return PingResult{OK: false, Error: err.Error()}, nil
	}
	start := time.Now()
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model: channel,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "ping"},
		},
		MaxTokens: 1,
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return PingResult{OK: false, LatencyMs: latency, Error: err.Error()}, nil
	}
	sample := ""
	if len(resp.Choices) > 0 {
		sample = resp.Choices[0].Message.Content
	}
	return PingResult{OK: true, LatencyMs: latency, Sample: sample}, nil
}

// inferProtocol 是非常粗糙的协议猜测（仅作 UI 展示；真实协议由
// provider 在 config.LLMConfig.Provider 里表达）。
func inferProtocol(code string) string {
	switch {
	case strings.Contains(code, "anthropic"):
		return "anthropic"
	case strings.Contains(code, "gemini"):
		return "gemini"
	case strings.Contains(code, "deepseek"):
		return "deepseek"
	default:
		return "openai-compatible"
	}
}

// --- Tasks adapter ---

// TasksAdapter 把 task.Executor 接到 TaskBackend。
type TasksAdapter struct {
	Executor task.Executor
}

// List 按 state 过滤。
func (a *TasksAdapter) List(ctx context.Context, stateFilter string) ([]TaskItem, error) {
	if a.Executor == nil {
		return []TaskItem{}, nil
	}
	var filter task.Filter
	if stateFilter != "" {
		// 把字符串 state 转成 task.State
		switch stateFilter {
		case "pending":
			filter.States = []task.State{task.StatePending}
		case "running":
			filter.States = []task.State{task.StateRunning}
		case "completed":
			filter.States = []task.State{task.StateCompleted}
		case "failed":
			filter.States = []task.State{task.StateFailed}
		case "canceled":
			filter.States = []task.State{task.StateCanceled}
		}
	}
	list, err := a.Executor.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]TaskItem, len(list))
	for i, t := range list {
		out[i] = TaskItem{
			ID:         t.ID,
			Code:       t.Code,
			Title:      t.Title,
			State:      t.State.String(),
			Profile:    t.Profile,
			Owner:      t.Owner,
			CreatedAt:  t.CreatedAt,
			UpdatedAt:  t.UpdatedAt,
			StartedAt:  t.StartedAt,
			FinishedAt: t.FinishedAt,
			Error:      t.Error,
		}
	}
	return out, nil
}

func (a *TasksAdapter) Cancel(ctx context.Context, id string) error {
	if a.Executor == nil {
		return ErrTaskNotFound
	}
	if err := a.Executor.Cancel(ctx, id); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			return ErrTaskNotFound
		}
		return err
	}
	return nil
}

// Submit 提交一个新任务（v8 P0）。
func (a *TasksAdapter) Submit(ctx context.Context, title, input, profile string) (TaskItem, error) {
	if a.Executor == nil {
		return TaskItem{}, ErrBackendMissing
	}
	if title == "" {
		title = input
	}
	if profile == "" {
		profile = "headless"
	}
	out, err := a.Executor.Submit(ctx, task.SubmitRequest{
		Title:   truncate(title, 80),
		Input:   input,
		Profile: profile,
	})
	if err != nil {
		return TaskItem{}, err
	}
	if out == nil {
		return TaskItem{}, errors.New("tasks adapter: nil submit result")
	}
	return taskToItem(out), nil
}

// Retry 重跑已终止任务。
func (a *TasksAdapter) Retry(ctx context.Context, id string) (TaskItem, error) {
	if a.Executor == nil {
		return TaskItem{}, ErrBackendMissing
	}
	out, err := a.Executor.Retry(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, task.ErrNotFound):
			return TaskItem{}, ErrTaskNotFound
		case errors.Is(err, task.ErrAlreadyRunning):
			return TaskItem{}, ErrTaskRunning
		default:
			return TaskItem{}, err
		}
	}
	if out == nil {
		return TaskItem{}, errors.New("tasks adapter: nil retry result")
	}
	return taskToItem(out), nil
}

// taskToItem 把 *task.Task 转成 Console TaskItem。
func taskToItem(t *task.Task) TaskItem {
	return TaskItem{
		ID:         t.ID,
		Code:       t.Code,
		Title:      t.Title,
		State:      t.State.String(),
		Profile:    t.Profile,
		Owner:      t.Owner,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
		Error:      t.Error,
	}
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- Jobs adapter ---

// JobsAdapter 把 jobs.Registry 接到 JobsBackend。
type JobsAdapter struct {
	Registry *jobs.Registry
}

// Get 返回 job 的进度快照（lines 数 + state 名）。
func (a *JobsAdapter) Get(_ context.Context, jobID string) (TaskProgress, error) {
	if a.Registry == nil {
		return TaskProgress{}, errors.New("jobs adapter: registry nil")
	}
	job, err := a.Registry.Get(context.Background(), jobID)
	if err != nil {
		return TaskProgress{}, err
	}
	lines, err := a.Registry.Output(jobID, 0, 0)
	if err != nil {
		// Output 失败不致命（job 已结束）
		lines = nil
	}
	return TaskProgress{
		JobID:  jobID,
		Lines:  len(lines),
		Status: job.State.String(),
	}, nil
}

// --- Approvals adapter ---

// ApprovalsAdapter 把 console.approvalQueue 接到 ApprovalsBackend。
type ApprovalsAdapter struct {
	Queue *approvalQueue
}

func (a *ApprovalsAdapter) List(_ context.Context) ([]ApprovalItem, error) {
	if a.Queue == nil {
		return []ApprovalItem{}, nil
	}
	return a.Queue.List(), nil
}

func (a *ApprovalsAdapter) Decide(ctx context.Context, id, decision string) error {
	if a.Queue == nil {
		return ErrApprovalNotFound
	}
	return a.Queue.DecideSync(ctx, id, decision)
}

func (a *ApprovalsAdapter) Enqueue(_ context.Context, item ApprovalItem) (string, <-chan string, error) {
	if a.Queue == nil {
		return "", nil, ErrBackendMissing
	}
	return a.Queue.Enqueue(item)
}

// Resolver 返回 approval.Resolver（让 HTTPPollApprover 在 PolicyAsk 时
// 等待本队列的 Decide）。Decision 字符串 "allow" → approval.ApproveOnce
// 或 ApproveSession；"deny" → approval.Deny；"always" → ApproveSession。
func (a *ApprovalsAdapter) ApprovalResolver() approval.Resolver {
	if a.Queue == nil {
		return nil
	}
	return approvalResolverAdapter{q: a.Queue}
}

// approvalResolverAdapter 包装 console.approvalQueue 的 Resolver 为
// approval.Resolver（避免 console 直接 import approval 的 Resolver
// 接口造成耦合）。
type approvalResolverAdapter struct{ q *approvalQueue }

func (a approvalResolverAdapter) Resolve(ctx context.Context, id string) (approval.Decision, error) {
	res := a.q.Resolver()
	dec, err := res(ctx, id)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return approval.Deny, err
		}
		return approval.Deny, err
	}
	switch dec {
	case "allow":
		return approval.ApproveOnce, nil
	case "always":
		return approval.ApproveSession, nil
	default:
		return approval.Deny, nil
	}
}

// --- Events adapter ---

// EventsAdapter 把 v4 Gateway router 接到 EventStream。
//
// v8.0：直接转发 server.Router 的所有事件；前端按 source 过滤。
type EventsAdapter struct {
	Router *server.Router
}

// Subscribe 返回一个 channel + cancel；cancel 必须被调用以释放资源。
func (a *EventsAdapter) Subscribe(ctx context.Context, sources []string) (<-chan EventFrame, func(), error) {
	if a.Router == nil {
		return nil, func() {}, errors.New("events adapter: router nil")
	}
	out := make(chan EventFrame, 256)
	cancelCtx, cancel := context.WithCancel(ctx)

	// 构造 GatewayRequest 一次：source 字段填 "*" 表示通配（v4 不支持
	// 多 source；这里用单独 goroutine 起每个 source）。
	//
	// v8.0 简化：监听全部已注册 source。前端按 source 字段过滤。
	go func() {
		defer close(out)
		for _, src := range a.Router.Sources() {
			if len(sources) > 0 && !contains(sources, src) {
				continue
			}
			req := server.GatewayRequest{Source: src}
			ch := make(chan server.GatewayEvent, 64)
			done := make(chan error, 1)
			go func() {
				done <- a.Router.Dispatch(cancelCtx, req, ch)
				close(ch)
			}()
			for ev := range ch {
				frame := EventFrame{
					Source:    ev.Source,
					Type:      ev.Type,
					Payload:   rawToMap(ev.Payload),
					Timestamp: ev.Timestamp,
				}
				select {
				case out <- frame:
				case <-cancelCtx.Done():
					<-done
					return
				}
			}
			<-done
		}
	}()
	return out, cancel, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// rawToMap 把 json.RawMessage 转 map（payload 在 GatewayEvent 是 RawMessage）。
func rawToMap(b json.RawMessage) map[string]any {
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err == nil {
		return m
	}
	return map[string]any{"raw": string(b)}
}

// --- Inventory adapter for plugin.Inventory → console 抽象 ---

// PluginInventoryBridge 把 []*plugin.PluginEntry（来自 plugin.Inventory）
// 投影到 []inventoryEntry，供 console.PluginsAdapter 使用。
//
// main.go 用法：
//
//   inv := plugin.NewCombined()
//   ...
//   entries, _ := inv.List(ctx)
//   adapter := &console.PluginsAdapter{
//       Inventory: console.NewStaticPluginInventory(entries),
//       Statuses:  statusStore,
//   }
type staticPluginInventory struct{ entries []inventoryEntry }

// NewStaticPluginInventory 构造一个固定快照的 inventory 适配器。
//
// 每次 List 都返回传入时的副本；状态变化不会自动反映（控制台用
// 单独的 List 入口拉取）。
func NewStaticPluginInventory(entries []inventoryEntry) pluginInventory {
	cp := make([]inventoryEntry, len(entries))
	copy(cp, entries)
	return staticPluginInventory{entries: cp}
}

// List 返回快照。
func (s staticPluginInventory) List(_ context.Context) ([]inventoryEntry, error) {
	return s.entries, nil
}

// pluginEntry 是 server 包里 plugin.PluginEntry 的 console 包别名
// （结构相同；只取需要的字段，避免 import）。
type pluginEntry struct {
	Name    string
	Kind    string
	Source  string
	Version string
	Healthy bool
	Tools   []string
}

// EntriesFromPluginEntries 把一组 plugin.PluginEntry 转成 inventoryEntry。
//
// 字段映射：ToolSpecView.Name → inventoryEntry.Tools 字符串列表。
// 该函数故意在 console 包内：避免让 console 反向依赖 plugin。
func EntriesFromPluginEntries(in []pluginEntry) []inventoryEntry {
	out := make([]inventoryEntry, len(in))
	for i, e := range in {
		tools := make([]string, len(e.Tools))
		for j, t := range e.Tools {
			tools[j] = t
		}
		out[i] = inventoryEntry{
			Name:    e.Name,
			Kind:    e.Kind,
			Source:  e.Source,
			Version: e.Version,
			Healthy: e.Healthy,
			Tools:   tools,
		}
	}
	return out
}

// --- 工具函数 ---

// MustMarshal 是开发期 helper：失败 panic。生产代码用 marshalJSON。
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("console: marshal: %v", err))
	}
	return b
}
