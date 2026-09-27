package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Spill handler（v8.1）

// handleSessionEvents 处理 GET /api/v1/console/sessions/{sid}/events?since=N。
//
// 返回 {"sid":..., "events":[...], "lastSeq":N}。
// 当 SessionBackend 未实现 EventStore（nil events）时返回 503。
func (s *ConsoleServer) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, "sid required")
		return
	}
	since := int64(-1)
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < -1 {
			writeError(w, http.StatusBadRequest, "invalid since")
			return
		}
		since = n
	}
	evs, lastSeq, err := s.Deps.Sessions.EventsSince(r.Context(), sid, since)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if evs == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "spill not supported")
		return
	}
	if evs == nil {
		evs = []SessionEvent{}
	}
	if lastSeq < since {
		lastSeq = since
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sid":     sid,
		"events":  evs,
		"lastSeq": lastSeq,
	})
}

// ---------------------------------------------------------------------------
// Schedule handlers（v8.1）
//
// 这些 handler 当前在 Deps.Schedules 为 nil 时返回 503。当 backend 实现
// 注入后（P3）即可工作。

func (s *ConsoleServer) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Schedules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "schedules backend not configured")
		return
	}
	items, err := s.Deps.Schedules.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *ConsoleServer) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Schedules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "schedules backend not configured")
		return
	}
	var req ScheduleItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := s.Deps.Schedules.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *ConsoleServer) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Schedules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "schedules backend not configured")
		return
	}
	id := r.PathValue("id")
	var req ScheduleItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := s.Deps.Schedules.Update(r.Context(), id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *ConsoleServer) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Schedules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "schedules backend not configured")
		return
	}
	id := r.PathValue("id")
	if err := s.Deps.Schedules.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *ConsoleServer) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Schedules == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "schedules backend not configured")
		return
	}
	id := r.PathValue("id")
	item, err := s.Deps.Schedules.Run(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

// ---------------------------------------------------------------------------
// Webhook handlers（v8.1）

func (s *ConsoleServer) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	items, err := s.Deps.Webhooks.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *ConsoleServer) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	var req WebhookItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := s.Deps.Webhooks.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *ConsoleServer) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	id := r.PathValue("id")
	var req WebhookItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	item, err := s.Deps.Webhooks.Update(r.Context(), id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *ConsoleServer) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	id := r.PathValue("id")
	if err := s.Deps.Webhooks.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// TestWebhookRequest is the optional payload for POST /webhooks/{id}/test.
type TestWebhookRequest struct {
	Payload string `json:"payload"`
}

func (s *ConsoleServer) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	id := r.PathValue("id")
	var req TestWebhookRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // optional
	if req.Payload == "" {
		req.Payload = `{"event":"webhook.test","ts":"` + nowRFC3339() + `"}`
	}
	del, err := s.Deps.Webhooks.Dispatch(r.Context(), id, req.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, del)
}

func (s *ConsoleServer) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Webhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "webhooks backend not configured")
		return
	}
	id := r.PathValue("id")
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	ds, err := s.Deps.Webhooks.ListDeliveries(r.Context(), id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

// ---------------------------------------------------------------------------
// helpers

// nowRFC3339 returns RFC3339 timestamp string.
func nowRFC3339() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// errStr 转换任意 error 为 string（空错误返回空串）。
func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// ctxVal reads context value with type assertion (avoids import cycle).
func ctxVal(ctx context.Context, key any) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(key)
}

// errIs reports whether err is or wraps target.
func errIs(err, target error) bool { return errors.Is(err, target) }
