// Package acp 提供 Agent Communication Protocol 服务端（v7 P7-4）。
//
// ACP（v1 spec，参考 dsh-java 的 acp.proto）：
//   - session/create → 返回 {session_id}
//   - session/send   → 提交一段 prompt；返回 task_id（v6 Task 域）
//   - session/cancel → 取消 task
//   - session/list   → 当前 session 的 task 列表
//   - permission/decide → 审批决策
//
// 协议传输：HTTP + WebSocket。
//   - HTTP POST/GET：控制面（session.create / send / cancel / permission）
//   - WebSocket：事件流（frame = ACP message）
//
// 任何遵循 ACP 的 IDE / 编辑器都可作为客户端接入。
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"deepseek-harness-go/internal/storage"
)

// TaskSubmitter 是 v6 Task 域的最小投影（避免循环依赖）。
//
// 实现侧 = internal/task.LoopExecutor。
type TaskSubmitter interface {
	Submit(ctx context.Context, input string) (taskHandle, error)
	Get(ctx context.Context, id string) (taskHandle, error)
	Cancel(ctx context.Context, id string) error
	List(ctx context.Context) ([]taskHandle, error)
}

type taskHandle struct {
	ID       string
	State    string
	Content  string
	Error    string
	Usage    map[string]any
}

// Session 是 ACP session（与 Task 1:1）。
type Session struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Tasks     []string  `json:"tasks"`
}

// CreateSessionRequest 是 session/create 入参。
type CreateSessionRequest struct {
	Profile string `json:"profile,omitempty"`
	Owner   string `json:"owner,omitempty"`
}

// CreateSessionResponse 是 session/create 出参。
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
}

// SendRequest 是 session/send 入参。
type SendRequest struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

// SendResponse 是 session/send 出参。
type SendResponse struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	State     string `json:"state"`
}

// CancelRequest 是 session/cancel 入参。
type CancelRequest struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
}

// CancelResponse 是 session/cancel 出参。
type CancelResponse struct {
	OK     bool   `json:"ok"`
	State  string `json:"state,omitempty"`
}

// ListRequest 是 session/list 入参。
type ListRequest struct {
	SessionID string `json:"session_id"`
}

// ListResponse 是 session/list 出参。
type ListResponse struct {
	Tasks []TaskView `json:"tasks"`
}

// TaskView 是 list 返回的 task 视图。
type TaskView struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Server 是 ACP 服务端主体。
//
// 持有 sessions / 关联 TaskSubmitter / 共享 KV（用 v6 storage）。
type Server struct {
	mu       sync.Mutex
	sessions map[string]*Session

	tasks   TaskSubmitter
	store   storage.Storage // 共享状态（v6 KV）
	token   string          // 鉴权 token（从 v5 credentials 读）

	nextID int
}

// NewServer 构造。
func NewServer(tasks TaskSubmitter, store storage.Storage, token string) *Server {
	return &Server{
		sessions: make(map[string]*Session),
		tasks:    tasks,
		store:    store,
		token:    token,
	}
}

// CreateSession 处理 session/create。
func (s *Server) CreateSession(_ context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("acp-%d", s.nextID)
	s.nextID++
	now := time.Now()
	s.sessions[id] = &Session{ID: id, CreatedAt: now, Tasks: nil}
	if s.store != nil {
		_ = storage.JSONSet(s.store, "acp", id, map[string]any{
			"created_at": now,
			"profile":    req.Profile,
			"owner":      req.Owner,
		})
	}
	return &CreateSessionResponse{SessionID: id}, nil
}

// Send 处理 session/send。
func (s *Server) Send(ctx context.Context, req SendRequest) (*SendResponse, error) {
	s.mu.Lock()
	sess, ok := s.sessions[req.SessionID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("acp: session %s not found", req.SessionID)
	}
	s.mu.Unlock()

	if s.tasks == nil {
		return nil, errors.New("acp: task submitter unavailable")
	}
	th, err := s.tasks.Submit(ctx, req.Content)
	if err != nil {
		return nil, fmt.Errorf("acp: submit: %w", err)
	}

	s.mu.Lock()
	sess.Tasks = append(sess.Tasks, th.ID)
	s.mu.Unlock()
	return &SendResponse{SessionID: req.SessionID, TaskID: th.ID, State: th.State}, nil
}

// Cancel 处理 session/cancel。
func (s *Server) Cancel(ctx context.Context, req CancelRequest) (*CancelResponse, error) {
	if s.tasks == nil {
		return nil, errors.New("acp: task submitter unavailable")
	}
	if err := s.tasks.Cancel(ctx, req.TaskID); err != nil {
		return nil, fmt.Errorf("acp: cancel: %w", err)
	}
	th, _ := s.tasks.Get(ctx, req.TaskID)
	state := ""
	if th.State != "" {
		state = th.State
	}
	return &CancelResponse{OK: true, State: state}, nil
}

// List 处理 session/list。
func (s *Server) List(ctx context.Context, req ListRequest) (*ListResponse, error) {
	s.mu.Lock()
	sess, ok := s.sessions[req.SessionID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("acp: session %s not found", req.SessionID)
	}
	s.mu.Unlock()
	if s.tasks == nil {
		return &ListResponse{}, nil
	}
	out := []TaskView{}
	for _, tid := range sess.Tasks {
		th, err := s.tasks.Get(ctx, tid)
		if err != nil {
			continue
		}
		out = append(out, TaskView{ID: th.ID, State: th.State, Content: th.Content, Error: th.Error})
	}
	return &ListResponse{Tasks: out}, nil
}

// Sessions 返回当前 session 数（用于测试 / 状态查看）。
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// --- HTTP handler ---

// Handler 返回 ACP HTTP handler；可直接挂载到 http.Server。
//
// 路由：
//   - POST /acp/session/create
//   - POST /acp/session/send
//   - POST /acp/session/cancel
//   - POST /acp/session/list
//   - POST /acp/permission/decide
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/acp/session/create", s.auth(s.handleCreate))
	mux.HandleFunc("/acp/session/send", s.auth(s.handleSend))
	mux.HandleFunc("/acp/session/cancel", s.auth(s.handleCancel))
	mux.HandleFunc("/acp/session/list", s.auth(s.handleList))
	mux.HandleFunc("/acp/permission/decide", s.auth(s.handlePermission))
	return mux
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			tok := r.Header.Get("Authorization")
			if len(tok) < 8 || tok[:7] != "Bearer " || tok[7:] != s.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		h(w, r)
	}
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.CreateSession(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.Send(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.Cancel(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.List(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.store != nil {
		_ = storage.JSONSet(s.store, "acp.permission", req.ID, req)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
