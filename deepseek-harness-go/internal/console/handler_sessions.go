package console

import (
	"errors"
	"net/http"
	"strconv"
)

// handleHealth 永远返回 200 + 版本；不要求鉴权。
func (s *ConsoleServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": s.version,
		"module":  "console",
		// v8.1 P4: 暴露 no-auth 标志给前端；前端据此跳过 login
		"noAuth":  s.cfg.NoAuth,
	})
}

// handleListSessions 处理 GET /api/v1/console/sessions。
//
// 查询参数：limit (default 50, max 200) 与 cursor (上页最后一条 sid)。
func (s *ConsoleServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	cursor := r.URL.Query().Get("cursor")
	items, err := s.Deps.Sessions.List(r.Context(), limit, cursor)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"limit":  limit,
		"cursor": cursor,
	})
}

// handleCreateSession 处理 POST /api/v1/console/sessions。
//
// 请求体：{"title": "...", "model": "deepseek-chat"}。
// model 为空时 backend 用 default。
func (s *ConsoleServer) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	item, err := s.Deps.Sessions.Create(r.Context(), req.Title, req.Model)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleGetSession 处理 GET /api/v1/console/sessions/{sid}。
func (s *ConsoleServer) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	if sid == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "session id required")
		return
	}
	detail, err := s.Deps.Sessions.Get(r.Context(), sid)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleDeleteSession 处理 DELETE /api/v1/console/sessions/{sid}。
func (s *ConsoleServer) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	if sid == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "session id required")
		return
	}
	if err := s.Deps.Sessions.Delete(r.Context(), sid); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePostMessage 处理 POST /api/v1/console/sessions/{sid}/messages。
//
// 请求体：{"content": "..."}。
// 响应：text/event-stream，每帧 data: {"event":"...","data":{...}}。
func (s *ConsoleServer) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	if sid == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "session id required")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if req.Content == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "content required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	out := make(chan SessionFrame, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Deps.Sessions.SendStream(r.Context(), sid, req.Content, out)
		close(out)
	}()
	for frame := range out {
		writeSessionFrame(w, flusher, frame)
	}
	if err := <-errCh; err != nil {
		writeSessionFrame(w, flusher, SessionFrame{
			Event: "loop_error",
			Data:  map[string]any{"err": err.Error()},
		})
	}
}

// writeSessionFrame 写一帧 SSE 给前端。
func writeSessionFrame(w http.ResponseWriter, flusher http.Flusher, frame SessionFrame) {
	// 拼成 {"event":"...","data":{...}} 一行
	payload := map[string]any{
		"event": frame.Event,
		"data":  frame.Data,
	}
	b, err := marshalJSON(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

// handleMessageFeedback 处理 POST /sessions/{sid}/messages/{seq}/feedback
//
// 请求体：{"rating": -1|0|1, "comment": "..."}
//   - rating: +1 (good) / -1 (bad) / 0 (clear)
//   - 0 时 comment 会被丢弃（清除评分）
func (s *ConsoleServer) handleMessageFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	seqStr := r.PathValue("seq")
	if sid == "" || seqStr == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "sid and seq required")
		return
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil || seq < 0 {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid seq")
		return
	}
	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if err := s.Deps.Sessions.SetMessageRating(r.Context(), sid, seq, req.Rating, req.Comment); err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
		case errors.Is(err, ErrMessageNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeNotFound, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "seq": seq, "rating": req.Rating})
}

// handleMessageRegenerate 处理 POST /sessions/{sid}/messages/{seq}/regenerate
//
// 把 seq 的 user 消息之后的 assistant/tool 消息全部删除并重新跑。
// 响应：SSE stream（与 POST /messages 共享事件格式）。
func (s *ConsoleServer) handleMessageRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	seqStr := r.PathValue("seq")
	if sid == "" || seqStr == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "sid and seq required")
		return
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil || seq < 0 {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid seq")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	out := make(chan SessionFrame, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Deps.Sessions.Regenerate(r.Context(), sid, seq, out)
		close(out)
	}()
	for frame := range out {
		writeSessionFrame(w, flusher, frame)
	}
	if err := <-errCh; err != nil {
		writeSessionFrame(w, flusher, SessionFrame{
			Event: "loop_error",
			Data:  map[string]any{"err": err.Error()},
		})
	}
}

// handleSessionExport 处理 GET /sessions/{sid}/export?format=md|jsonl
//
// 响应：
//   - format=md    → text/markdown
//   - format=jsonl → application/x-ndjson
func (s *ConsoleServer) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Sessions == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "sessions backend not configured")
		return
	}
	sid := r.PathValue("sid")
	if sid == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "sid required")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}
	ctx := r.Context()
	var (
		body  string
		ctype string
	)
	switch format {
	case "md", "markdown":
		s, err := s.Deps.Sessions.ExportMarkdown(ctx, sid)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
			} else {
				writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
			}
			return
		}
		body = s
		ctype = "text/markdown; charset=utf-8"
	case "jsonl":
		s, err := s.Deps.Sessions.ExportJSONL(ctx, sid)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
			} else {
				writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
			}
			return
		}
		body = s
		ctype = "application/x-ndjson; charset=utf-8"
	default:
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "format must be md or jsonl")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+sid+"."+format+`"`)
	_, _ = w.Write([]byte(body))
}
