// Package server — Gateway 内置 source handler（v4 §B.3）。
//
// gateway_handler.go 实现 Router 内置的 source handler：
//   - events.subscribe
//   - session.send
//   - session.snapshot
//   - tools.list       (P2 stub，P3 接入)
//   - plugins.list     (P2 stub，P3 接入)
//   - llm.call         (直调 Client.Chat)
//
// P2 阶段 tools/plugins 返回空数组；handler 签名固定，P3 在不破坏
// 兼容的前提下注入真实数据。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/plugin"
	"deepseek-harness-go/internal/store"
)

// GatewayHandlers 持有 handler 依赖。
type GatewayHandlers struct {
	Runner    agent.StreamingRunner
	Store     store.Store
	Client    llm.Client      // 用于 llm.call
	Inventory plugin.Inventory // v4 P3: 用于 tools.list / plugins.list
}

// RegisterAll 把全部内置 source 注册到 router。
func (h *GatewayHandlers) RegisterAll(r *Router) {
	r.Register("events.subscribe", h.handleEventsSubscribe)
	r.Register("session.send", h.handleSessionSend)
	r.Register("session.snapshot", h.handleSessionSnapshot)
	r.Register("tools.list", h.handleToolsList)
	r.Register("plugins.list", h.handlePluginsList)
	r.Register("llm.call", h.handleLLMCall)
}

// --- 内部 helper ---

func emit(ctx context.Context, out chan<- GatewayEvent, source, typ string, payload any) error {
	b, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	ev := GatewayEvent{Source: source, Type: typ, Payload: b}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- ev:
		return nil
	}
}

func marshalPayload(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage("null"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("gateway: marshal payload: %w", err)
	}
	return b, nil
}

// --- events.subscribe ---

// eventsSubscribeParams 是 events.subscribe 的参数。
type eventsSubscribeParams struct {
	SID      string `json:"sid"`
	SinceSeq int64  `json:"since_seq,omitempty"`
}

func (h *GatewayHandlers) handleEventsSubscribe(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	var p eventsSubscribeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fmt.Errorf("events.subscribe: invalid params: %w", err)
		}
	}
	if p.SID == "" {
		return errors.New("events.subscribe: sid required")
	}
	if h.Store == nil {
		return errors.New("events.subscribe: store unavailable")
	}
	es, ok := store.AsEventStore(h.Store)
	if !ok {
		return errors.New("events.subscribe: store does not support events")
	}
	events, err := es.ReadEvents(ctx, p.SID, p.SinceSeq)
	if err != nil {
		return fmt.Errorf("events.subscribe: read: %w", err)
	}
	for _, ev := range events {
		// 把 store.Event 序列化为 gateway payload
		payload := map[string]any{
			"seq":       ev.Seq,
			"type":      ev.Type.String(),
			"ts":        ev.Timestamp,
			"raw":       json.RawMessage(ev.Payload),
			"actor":     ev.Actor,
		}
		if err := emit(ctx, out, "events.subscribe", "delta", payload); err != nil {
			return err
		}
	}
	return emit(ctx, out, "events.subscribe", "final", map[string]any{"count": len(events)})
}

// --- session.send ---

type sessionSendParams struct {
	SID    string `json:"sid,omitempty"`
	Prompt string `json:"prompt"`
}

func (h *GatewayHandlers) handleSessionSend(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	var p sessionSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fmt.Errorf("session.send: invalid params: %w", err)
	}
	if p.Prompt == "" {
		return errors.New("session.send: prompt required")
	}
	if h.Runner == nil {
		return errors.New("session.send: runner unavailable")
	}
	events, result := h.Runner.RunStream(ctx, p.Prompt, p.SID)
	for ev := range events {
		// 把 agent.Event 序列化为通用 payload。
		payload := map[string]any{
			"tag": ev.EventTag(),
			"val": ev,
		}
		if err := emit(ctx, out, "session.send", "delta", payload); err != nil {
			return err
		}
	}
	res := <-result
	if res.Error != nil {
		return emit(ctx, out, "session.send", "error", map[string]any{"err": res.Error.Error()})
	}
	return emit(ctx, out, "session.send", "final", map[string]any{
		"session_id":  res.SessionID,
		"rounds":      res.Rounds,
		"stop_reason": res.StopReason,
		"messages":    res.FinalMessages,
		"usage": map[string]int{
			"prompt_tokens":     res.Usage.PromptTokens,
			"completion_tokens": res.Usage.CompletionTokens,
			"total_tokens":      res.Usage.TotalTokens,
		},
	})
}

// --- session.snapshot ---

type sessionSnapshotParams struct {
	SID        string `json:"sid"`
	Projection string `json:"projection,omitempty"` // "messages" | "usage" | "phase"；默认 messages
}

func (h *GatewayHandlers) handleSessionSnapshot(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	var p sessionSnapshotParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fmt.Errorf("session.snapshot: invalid params: %w", err)
	}
	if p.SID == "" {
		return errors.New("session.snapshot: sid required")
	}
	if h.Store == nil {
		return errors.New("session.snapshot: store unavailable")
	}
	projection := p.Projection
	if projection == "" {
		projection = "messages"
	}
	// 优先 EventStore.Project，否则回退到 v3 Store.Load。
	if es, ok := h.Store.(store.ProjectionStore); ok {
		state, err := es.Project(ctx, p.SID, projection)
		if err != nil {
			return fmt.Errorf("session.snapshot: project: %w", err)
		}
		return emit(ctx, out, "session.snapshot", "final", map[string]any{
			"sid":        p.SID,
			"projection": projection,
			"state":      state,
		})
	}
	// 回退：v3 Store.Load 只能给 messages 投影
	if projection != "messages" {
		return fmt.Errorf("session.snapshot: projection %q not supported by non-EventStore backend", projection)
	}
	sess, err := h.Store.Load(ctx, p.SID)
	if err != nil {
		return fmt.Errorf("session.snapshot: load: %w", err)
	}
	return emit(ctx, out, "session.snapshot", "final", map[string]any{
		"sid":        p.SID,
		"projection": "messages",
		"state":      sess.Messages,
	})
}

// --- tools.list (P3 接入真实 Inventory) ---

func (h *GatewayHandlers) handleToolsList(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	if h.Inventory == nil {
		return emit(ctx, out, "tools.list", "final", map[string]any{"tools": []any{}})
	}
	entries, err := h.Inventory.List(ctx)
	if err != nil {
		return emit(ctx, out, "tools.list", "error", map[string]any{"err": err.Error()})
	}
	// 摊平：每个 plugin 的每个 tool 暴露为一个 tool 描述。
	tools := []map[string]any{}
	for _, e := range entries {
		for _, t := range e.Tools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"risk":        t.Risk,
				"parameters":  t.Parameters,
				"plugin":      e.Name,
				"plugin_kind": e.Kind,
			})
		}
	}
	return emit(ctx, out, "tools.list", "final", map[string]any{
		"tools":  tools,
		"plugin_count": len(entries),
	})
}

// --- plugins.list (P3 接入真实 Inventory) ---

func (h *GatewayHandlers) handlePluginsList(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	if h.Inventory == nil {
		return emit(ctx, out, "plugins.list", "final", map[string]any{"plugins": []any{}})
	}
	entries, err := h.Inventory.List(ctx)
	if err != nil {
		return emit(ctx, out, "plugins.list", "error", map[string]any{"err": err.Error()})
	}
	return emit(ctx, out, "plugins.list", "final", map[string]any{
		"plugins": entries,
		"count":   len(entries),
	})
}

// --- llm.call ---

type llmCallParams struct {
	Prompt string   `json:"prompt"`
	System string   `json:"system,omitempty"`
	Model  string   `json:"model,omitempty"`
	Temp   *float64 `json:"temperature,omitempty"`
}

func (h *GatewayHandlers) handleLLMCall(ctx context.Context, req GatewayRequest, out chan<- GatewayEvent) error {
	if h.Client == nil {
		return errors.New("llm.call: client unavailable")
	}
	var p llmCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fmt.Errorf("llm.call: invalid params: %w", err)
	}
	if p.Prompt == "" {
		return errors.New("llm.call: prompt required")
	}
	msgs := []llm.Message{}
	if p.System != "" {
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: p.System})
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: p.Prompt})
	resp, err := h.Client.Chat(ctx, llm.ChatRequest{
		Messages:    msgs,
		Model:       p.Model,
		Temperature: p.Temp,
	})
	if err != nil {
		return emit(ctx, out, "llm.call", "error", map[string]any{"err": err.Error()})
	}
	content := ""
	var usage llm.Usage
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	if resp.Usage != nil {
		usage = *resp.Usage
	}
	return emit(ctx, out, "llm.call", "final", map[string]any{
		"content": content,
		"usage": map[string]int{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		},
	})
}