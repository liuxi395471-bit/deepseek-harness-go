package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// --- Session message edit/delete (P0) ---

func (s *ConsoleServer) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
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
	if err := s.Deps.Sessions.EditMessage(r.Context(), sid, seq, req.Content); err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeSessionMissing, err.Error())
		case errors.Is(err, ErrMessageNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeNotFound, err.Error())
		case errors.Is(err, ErrMessageNotEditable):
			writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sid": sid, "seq": seq})
}

func (s *ConsoleServer) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
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
	if err := s.Deps.Sessions.DeleteMessage(r.Context(), sid, seq); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sid": sid, "seq": seq})
}

// --- Plugins install/uninstall (P0) ---

func (s *ConsoleServer) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Plugins == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "plugins backend not configured")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "plugin name required")
		return
	}
	var req struct {
		Source string `json:"source"`
	}
	_ = readJSON(r, &req) // body 可选
	if err := s.Deps.Plugins.Install(r.Context(), name, req.Source); err != nil {
		switch {
		case errors.Is(err, ErrPluginExists):
			writeErrorCode(w, http.StatusConflict, CodeConflict, err.Error())
		case errors.Is(err, ErrPluginNotFound):
			writeErrorCode(w, http.StatusNotFound, CodePluginMissing, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
}

func (s *ConsoleServer) handleUninstallPlugin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Plugins == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "plugins backend not configured")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "plugin name required")
		return
	}
	if err := s.Deps.Plugins.Uninstall(r.Context(), name); err != nil {
		switch {
		case errors.Is(err, ErrPluginNotFound):
			writeErrorCode(w, http.StatusNotFound, CodePluginMissing, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
}

// --- Models create/remove (P0) ---

func (s *ConsoleServer) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Models == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "models backend not configured")
		return
	}
	var item ModelItem
	if !readJSON(r, &item) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if item.Channel == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "channel required")
		return
	}
	if item.Model == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "model required")
		return
	}
	if err := s.Deps.Models.Create(r.Context(), item); err != nil {
		switch {
		case errors.Is(err, ErrModelExists):
			writeErrorCode(w, http.StatusConflict, CodeConflict, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *ConsoleServer) handleRemoveModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Models == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "models backend not configured")
		return
	}
	channel := r.PathValue("channel")
	if channel == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "channel required")
		return
	}
	if err := s.Deps.Models.Remove(r.Context(), channel); err != nil {
		switch {
		case errors.Is(err, ErrModelNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeModelMissing, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": channel})
}

// --- Tasks submit + retry (P0) ---

func (s *ConsoleServer) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Tasks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "tasks backend not configured")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Input   string `json:"input"`
		Profile string `json:"profile"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if req.Input == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "input required")
		return
	}
	item, err := s.Deps.Tasks.Submit(r.Context(), req.Title, req.Input, req.Profile)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *ConsoleServer) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Tasks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "tasks backend not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "task id required")
		return
	}
	item, err := s.Deps.Tasks.Retry(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskNotFound):
			writeErrorCode(w, http.StatusNotFound, CodeTaskMissing, err.Error())
		case errors.Is(err, ErrTaskRunning):
			writeErrorCode(w, http.StatusConflict, CodeConflict, err.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// --- Approvals batch decide (P0) ---

func (s *ConsoleServer) handleDecideApprovalsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Approvals == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "approvals backend not configured")
		return
	}
	var req struct {
		IDs      []string `json:"ids"`
		Decision string   `json:"decision"`
	}
	if !readJSON(r, &req) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	switch decision {
	case "allow", "deny", "always":
	default:
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "decision must be allow/deny/always")
		return
	}
	if len(req.IDs) == 0 {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "ids required")
		return
	}
	failed := []map[string]string{}
	for _, id := range req.IDs {
		if err := s.Deps.Approvals.Decide(r.Context(), id, decision); err != nil {
			failed = append(failed, map[string]string{"id": id, "error": err.Error()})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"total":  len(req.IDs),
		"failed": failed,
	})
}

// --- Audit (P0) ---

func (s *ConsoleServer) handleQueryAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Audit == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "audit backend not configured")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	items, err := s.Deps.Audit.Query(r.Context(), limit)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit})
}

func (s *ConsoleServer) handleExportAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Audit == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "audit backend not configured")
		return
	}
	limit := 1000
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10000 {
			limit = n
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit.jsonl"`)
	if err := s.Deps.Audit.Export(r.Context(), w, limit); err != nil {
		// 已经写出去一部分头部；只能忽略。
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
}
