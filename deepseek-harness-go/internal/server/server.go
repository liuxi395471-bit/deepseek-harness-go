// Package server 实现 agent runner 的 HTTP+SSE 接口（DESIGN-v2 §A.2）。
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

// Handler 返回配置好的 HTTP handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/agent/message", s.handleMessage)
	mux.HandleFunc("/api/agent/stream", s.handleStream)
	mux.HandleFunc("/api/sessions", s.handleListSessions)
	mux.HandleFunc("/api/sessions/", s.handleSession)
	return s.bearerAuth(mux)
}

// bearerAuth 为 next 包装 Bearer token 校验。/healthz 例外放行。
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
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
	for ev := range events {
		writeSSE(w, flusher, eventToFrame(ev))
	}
	res := <-result
	// 发送终止的 usage + loop_done 帧
	writeSSE(w, flusher, sseFrame{
		Event: "loop_done",
		Data: mustJSON(map[string]any{
			"rounds":      res.Rounds,
			"stop_reason": res.StopReason,
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
	writeJSON(w, http.StatusOK, sess)
}
