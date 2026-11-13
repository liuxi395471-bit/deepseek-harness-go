// Package server 实现 agent runner 的 HTTP+SSE 接口（ARCHITECTURE.md §13）。
//
// 端点：
//
//	POST /api/agent/message   — 阻塞式 RunStream，返回 RunResult JSON
//	GET  /api/agent/stream    — SSE RunStream，每个 Event 发送一条事件
//	GET  /api/sessions        — 列出会话
//	GET  /api/sessions/{id}   — 获取完整会话记录
//	GET  /healthz             — 健康检查（无需认证）
//
// 认证：使用 Authorization 头中的 Bearer token；除 /healthz 外，所有
// /api/* 路径都要求有效 token。token 为空时服务器不启动。
//
// 并发安全：每会话互斥锁防止对同一 session id 同时发起两个 RunStream
// 调用（第二个请求收到 409）。
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/store"
)

// Config 是服务器的运行时配置（config.ServerConfig 的子集）。
type Config struct {
	Listen      string
	AuthToken   string
	Timeout     int    // 请求超时时间（秒）；0 表示不超时
	MaxSessions int    // 软上限；仅作参考
}

// Server 将 HTTP 路由与 Runner 和 Store 关联起来。
type Server struct {
	cfg    Config
	runner agent.StreamingRunner
	store  store.Store

	// 每会话锁；防止对同一 id 并发调用 RunStream。
	sessMu  sync.Mutex
	sessRun map[string]struct{}

	// ui 是可选的前端静态资源 handler（internal/webui 提供）。
	// 非 nil 时，根路径下的请求会被转发给它（用于 SPA）。
	ui http.Handler

	httpServer *http.Server
}

// New 构造一个 Server。runner 是要派发到的 agent；store 用于管理会话生命周期。
func New(cfg Config, runner agent.StreamingRunner, st store.Store) *Server {
	return &Server{
		cfg:     cfg,
		runner:  runner,
		store:   st,
		sessRun: make(map[string]struct{}),
	}
}

// ErrConcurrentSession 在会话已被另一个请求占用时由 acquire 返回。
var ErrConcurrentSession = errors.New("server: session in use")

// isPublicPath 判断路径是否免鉴权。
//
//   - /healthz：健康检查，任何人都可以访问（探针需要）。
//   - 前端静态资源：浏览器加载 index.html 时无法携带自定义请求头，
//     因此必须放行；页面本身不含敏感数据，真正的数据仍由受保护的
//     /api/* 提供。
//
// 注意：/api/* 一律**不**放行。
func (s *Server) isPublicPath(p string) bool {
	if p == "/healthz" {
		return true
	}
	if s.ui != nil && !strings.HasPrefix(p, "/api/") {
		return true
	}
	return false
}

// acquire 将 session id 标记为使用中。若其上已有另一个请求在运行，
// 则返回 ErrConcurrentSession。
func (s *Server) acquire(id string) error {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if _, ok := s.sessRun[id]; ok {
		return ErrConcurrentSession
	}
	s.sessRun[id] = struct{}{}
	return nil
}

// release 清除 session id 的使用中标记。
func (s *Server) release(id string) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	delete(s.sessRun, id)
}

// SetUI 设置前端静态资源 handler。
//
// 该 handler 收到的请求**不要求** Bearer token（前端页面本身不含
// 敏感数据），但 API 路径仍由 bearerAuth 保护。传入 nil 表示不启用
// 前端（纯 API 模式）。
func (s *Server) SetUI(h http.Handler) {
	s.ui = h
}

// Handler 返回配置好的 HTTP handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/agent/message", s.handleMessage)
	mux.HandleFunc("/api/agent/stream", s.handleStream)
	mux.HandleFunc("/api/sessions", s.handleListSessions)
	mux.HandleFunc("/api/sessions/", s.handleSession)

	// 前端 SPA 挂在根路径 "/"。注册在具体路径之后，
	// 因此 /healthz 与 /api/* 仍命中上面注册的 handler。
	if s.ui != nil {
		mux.Handle("/", s.ui)
	}

	return s.bearerAuth(mux)
}

// bearerAuth 为 next 包装 Bearer token 校验。
//
// 例外放行：/healthz（健康检查）与前端静态资源（不含敏感数据，
// 否则浏览器加载 index.html 时无法携带自定义请求头）。
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AuthToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start 在配置的监听地址上启动服务器并阻塞，直到 ctx 被取消或监听器
// 出错。返回 Shutdown 产生的第一个非 nil 错误。
func (s *Server) Start(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:    s.cfg.Listen,
		Handler: s.Handler(),
	}
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		return s.httpServer.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Shutdown 停止 HTTP 服务器。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// ---- 处理器 ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// messageReq 是 /api/agent/message 的 POST 请求体。
type messageReq struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
}

// messageResp 是 /api/agent/message 的响应。
type messageResp struct {
	SessionID string          `json:"session_id"`
	Messages  []any           `json:"messages"`
	Usage     map[string]int  `json:"usage"`
	StopReason string         `json:"stop_reason"`
	Rounds    int             `json:"rounds"`
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		// 从 handler 中的 panic 中恢复。
		if x := recover(); x != nil {
			http.Error(w, fmt.Sprintf("internal error: %v", x), http.StatusInternalServerError)
		}
	}()
	var req messageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	// 与 /api/agent/stream 一样，对同一 session id 做互斥：并发请求
	// 返回 409。否则两个 RunStream 会同时往同一个会话里追加消息，
	// 造成历史交错、usage 重复累加。
	if req.SessionID != "" {
		if err := s.acquire(req.SessionID); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		defer s.release(req.SessionID)
	}
	ctx := r.Context()
	if s.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, secondsToDuration(s.cfg.Timeout))
		defer cancel()
	}
	events, result := s.runner.RunStream(ctx, req.Prompt, req.SessionID)
	for ev := range events {
		_ = ev // 丢弃；客户端只需要最终结果
	}
	res := <-result
	if res.Error != nil {
		http.Error(w, "agent error: "+res.Error.Error(), http.StatusInternalServerError)
		return
	}
	resp := messageResp{
		SessionID:  res.SessionID,
		Messages:   msgsAsAny(res.FinalMessages),
		Usage: map[string]int{
			"prompt_tokens":     res.Usage.PromptTokens,
			"completion_tokens": res.Usage.CompletionTokens,
			"total_tokens":      res.Usage.TotalTokens,
		},
		StopReason: res.StopReason,
		Rounds:     res.Rounds,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleStream 处理 GET /api/agent/stream —— SSE 输出。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	sid := r.URL.Query().Get("session_id")
	if sid != "" {
		if err := s.acquire(sid); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		defer s.release(sid)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	if s.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, secondsToDuration(s.cfg.Timeout))
		defer cancel()
	}
	defer func() {
		_ = recover() // 工具 panic 安全兜底
	}()
	events, result := s.runner.RunStream(ctx, prompt, sid)
	// 记录是否已经转发过 loop_error：runner 在某些早退路径（如加载
	// 会话失败）只设置 RunResult.Error 而**不**发 LoopError 事件，
	// 而在 LLM 调用失败路径又会发 LoopError。这里做去重，保证客户端
	// 最多收到一条 loop_error。
	sawLoopError := false
	for ev := range events {
		// runner 的 LoopDone 事件不直接转发：它只带 rounds/stop_reason，
		// 缺少 session_id 与 usage。下面统一在拿到 RunResult 后发一个
		// 信息完整的 loop_done 帧，避免客户端收到两个语义重复的终止帧。
		if _, isDone := ev.(agent.LoopDone); isDone {
			continue
		}
		if _, isErr := ev.(agent.LoopError); isErr {
			sawLoopError = true
		}
		writeSSE(w, flusher, eventToFrame(ev))
	}
	res := <-result

	// runner 早退（未发 LoopError 事件）时，用 RunResult.Error 补一条
	// loop_error。否则前端只会看到 stop_reason="error"，却拿不到任何
	// 错误原因 —— 表现为"什么都没发生就结束了"。
	if res.Error != nil && !sawLoopError {
		writeSSE(w, flusher, sseFrame{
			Event: "loop_error",
			Data:  mustJSON(map[string]any{"err": res.Error.Error(), "phase": "error"}),
		})
	}

	// 发送终止的 usage + loop_done 帧。
	// session_id 必须带上：新建会话时（sid 为空）前端只有从这里才能
	// 拿到 id，后续轮次才能继续同一个会话。
	// error 字段在运行失败时携带原因（成功时为空串）。
	errMsg := ""
	if res.Error != nil {
		errMsg = res.Error.Error()
	}
	writeSSE(w, flusher, sseFrame{
		Event: "loop_done",
		Data: mustJSON(map[string]any{
			"rounds":      res.Rounds,
			"stop_reason": res.StopReason,
			"session_id":  res.SessionID,
			"error":       errMsg,
			"usage": map[string]any{
				"prompt_tokens":     res.Usage.PromptTokens,
				"completion_tokens": res.Usage.CompletionTokens,
				"total_tokens":      res.Usage.TotalTokens,
			},
		}),
	})
}

// handleListSessions 处理 GET /api/sessions。
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := s.store.List(r.Context(), 0, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type item struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		Rounds    int    `json:"rounds"`
		Preview   string `json:"preview"`
	}
	out := make([]item, len(list))
	for i, sess := range list {
		out[i] = item{
			ID:        sess.ID,
			CreatedAt: sess.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			UpdatedAt: sess.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Rounds:    sess.Rounds,
			Preview:   sess.Preview,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSession 处理 GET /api/sessions/{id}。
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	sess, err := s.store.Load(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 显式映射为 snake_case：直接序列化 store.Session 会输出 Go 的
	// 字段名（ID / CreatedAt / Messages…），与 /api/sessions 的
	// snake_case 契约不一致，会让客户端需要写两套解析逻辑。
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         sess.ID,
		"created_at": sess.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": sess.UpdatedAt.UTC().Format(time.RFC3339),
		"rounds":     sess.Rounds,
		"preview":    sess.Preview,
		"messages":   sess.Messages,
		"usage": map[string]any{
			"prompt_tokens":     sess.UsageTotal.PromptTokens,
			"completion_tokens": sess.UsageTotal.CompletionTokens,
			"total_tokens":      sess.UsageTotal.TotalTokens,
		},
	})
}
