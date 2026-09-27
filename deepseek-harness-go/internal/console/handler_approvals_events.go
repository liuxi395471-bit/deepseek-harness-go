package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleListApprovals 处理 GET /api/v1/console/approvals。
func (s *ConsoleServer) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Approvals == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "approvals backend not configured")
		return
	}
	items, err := s.Deps.Approvals.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleDecideApproval 处理 POST /api/v1/console/approvals/{id}/decide。
//
// 请求体：{"decision":"allow"|"deny"|"always"}。
func (s *ConsoleServer) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Approvals == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "approvals backend not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "approval id required")
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	req.Decision = strings.ToLower(strings.TrimSpace(req.Decision))
	switch req.Decision {
	case "allow", "deny", "always":
		// ok
	default:
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "decision must be allow/deny/always")
		return
	}
	if err := s.Deps.Approvals.Decide(r.Context(), id, req.Decision); err != nil {
		if err == ErrApprovalNotFound {
			writeErrorCode(w, http.StatusNotFound, CodeApprovalMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "decision": req.Decision})
}

// handleEvents 处理 GET /api/v1/console/events —— SSE 转发。
//
// 查询参数：sources（逗号分隔）。订阅 v4 Gateway SSE；ctx 取消时关闭。
func (s *ConsoleServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Events == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "events backend not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, "streaming unsupported")
		return
	}
	sourcesCSV := r.URL.Query().Get("sources")
	var sources []string
	if sourcesCSV != "" {
		for _, s := range strings.Split(sourcesCSV, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				sources = append(sources, s)
			}
		}
	}
	events, cancel, err := s.Deps.Events.Subscribe(r.Context(), sources)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for ev := range events {
		b, err := marshalJSON(ev)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
}

// handleGetState 处理 GET /api/v1/console/state?key=...。
func (s *ConsoleServer) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.ConsoleState == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "state backend not configured")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "key required")
		return
	}
	v, ok, err := s.Deps.ConsoleState.Get(r.Context(), key)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	if !ok {
		writeErrorCode(w, http.StatusNotFound, CodeNotFound, "key not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": v})
}

// handlePutState 处理 PUT /api/v1/console/state。
//
// 请求体：{"key":"...","value":"..."}。
func (s *ConsoleServer) handlePutState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.ConsoleState == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "state backend not configured")
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if req.Key == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "key required")
		return
	}
	if err := s.Deps.ConsoleState.Set(r.Context(), req.Key, req.Value); err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": req.Key})
}

// marshalJSON 是 errors.go 之外的小工具：避免 handler 多写 import。
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
