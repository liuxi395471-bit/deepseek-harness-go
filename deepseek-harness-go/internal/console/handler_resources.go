package console

import (
	"net/http"
	"strings"
)

// handleListPlugins 处理 GET /api/v1/console/plugins。
func (s *ConsoleServer) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Plugins == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "plugins backend not configured")
		return
	}
	items, err := s.Deps.Plugins.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleEnablePlugin 处理 POST /api/v1/console/plugins/{name}/enable。
func (s *ConsoleServer) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Deps.Plugins.Enable(r.Context(), name); err != nil {
		if err == ErrPluginNotFound {
			writeErrorCode(w, http.StatusNotFound, CodePluginMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "state": "loaded"})
}

// handleDisablePlugin 处理 POST /api/v1/console/plugins/{name}/disable。
func (s *ConsoleServer) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Deps.Plugins.Disable(r.Context(), name); err != nil {
		if err == ErrPluginNotFound {
			writeErrorCode(w, http.StatusNotFound, CodePluginMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "state": "disabled"})
}

// handleListModels 处理 GET /api/v1/console/models。
func (s *ConsoleServer) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Models == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "models backend not configured")
		return
	}
	items, err := s.Deps.Models.List(r.Context())
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleUpdateModel 处理 PUT /api/v1/console/models/{channel}。
//
// 请求体：ModelItem 的子集；channel 不可变。
func (s *ConsoleServer) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
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
	var item ModelItem
	if !readJSON(r, &item) {
		writeErrorCode(w, http.StatusBadRequest, CodeBadRequest, "invalid json body")
		return
	}
	if err := s.Deps.Models.Update(r.Context(), channel, item); err != nil {
		if err == ErrModelNotFound {
			writeErrorCode(w, http.StatusNotFound, CodeModelMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": channel})
}

// handlePingModel 处理 POST /api/v1/console/models/{channel}/ping。
func (s *ConsoleServer) handlePingModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
	res, err := s.Deps.Models.Ping(r.Context(), channel)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	if !res.OK {
		writeErrorCode(w, http.StatusBadGateway, CodePingFailed, res.Error)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleListTasks 处理 GET /api/v1/console/tasks。
//
// 查询参数：state（可选；空表示全部）。
func (s *ConsoleServer) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Deps.Tasks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, CodeUnavailable, "tasks backend not configured")
		return
	}
	state := r.URL.Query().Get("state")
	items, err := s.Deps.Tasks.List(r.Context(), state)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	// v8.0 jobs 进度补充（可选）
	if s.Deps.Jobs != nil {
		for i := range items {
			if items[i].Progress == nil {
				continue
			}
			p, err := s.Deps.Jobs.Get(r.Context(), items[i].Progress.JobID)
			if err == nil {
				items[i].Progress = &p
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleCancelTask 处理 POST /api/v1/console/tasks/{id}/cancel。
func (s *ConsoleServer) handleCancelTask(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Deps.Tasks.Cancel(r.Context(), id); err != nil {
		if err == ErrTaskNotFound {
			writeErrorCode(w, http.StatusNotFound, CodeTaskMissing, err.Error())
			return
		}
		writeErrorCode(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// --- helpers ---

// extractName 从 path 中提取 prefix + suffix 之间的段。
// 例：extractName("/api/v1/console/plugins/foo/enable",
//
//	"/api/v1/console/plugins/", "/enable")
//
// → "foo"
func extractName(path, prefix, suffix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path {
		return ""
	}
	rest = strings.TrimSuffix(rest, suffix)
	return rest
}

// extractChannel 用于 /api/v1/console/models/{channel}/...。
func extractChannel(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path {
		return ""
	}
	// channel 段以 "/" 结尾或后续 path
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}
