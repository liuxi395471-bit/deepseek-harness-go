// Package console 为 ds-go 提供 Web Console 后端（v8 P8-1）。
//
// console.go 定义 ConsoleServer 结构 + 路由装配 + 鉴权中间件。
// 它不重复 server.Server 的工作，而是暴露一组独立的
// `/api/v1/console/*` 路径给 v8 SPA；同时把 `/console/*` 静态资源
// 由 embed.FS 提供。
//
// 设计要点：
//   - 路由：与 server.Server 同进程共存，复用其 http.Server；
//   - 鉴权：所有 /api/v1/console/* 要求 Bearer；/healthz 类不要求；
//   - 复用：store / plugin / runtime / approval / task / jobs
//     全部走既有接口，不修改既有 internal/* 包。
//   - 静态资源：//go:embed all:web/dist 提供 SPA；dev tag 下可替换为
//     本地磁盘（不在 v8.0 实现，留 v8.1）。
package console

import (
	"crypto/subtle"
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// FS 持有 SPA 静态资源（在 embed.go 用 //go:embed 注入）。ConsoleServer
// 通过该字段在 /console/* 路径提供文件服务。
type FS = embed.FS

// Config 是 ConsoleServer 启动所需的最小配置。
type Config struct {
	// AuthToken 用于校验 Bearer；空时 ConsoleServer 仍可启动，但
	// 所有 /api/v1/console/* 路径会 401。
	AuthToken string
}

// ConsoleServer 把 Console HTTP handler 聚合起来。Handler() 返回一个
// http.Handler，可与 server.Server.Handler() 在顶层 mux 中并列注册。
type ConsoleServer struct {
	cfg Config

	// Deps 是各域句柄；任一字段为 nil 时对应 endpoint 返回 503。
	Deps Deps

	// 嵌入的 SPA 静态资源；通过 setSPAFS 注入（embed.go）。
	spaFS fs.FS

	// 版本号（构建期注入）；空时使用 "dev"。
	version string
}

// Deps 聚合 ConsoleServer 依赖的全部上游服务。所有字段都是可选的，
// 缺省时相关 endpoint 返回 503 而不是 panic。
type Deps struct {
	// Sessions / messages
	Sessions SessionBackend // 必填；List / Get / Create / Delete

	// Plugins
	Plugins PluginBackend // 必填；List / Enable / Disable

	// Models
	Models ModelBackend // 必填；List / Update / Ping

	// Tasks
	Tasks TaskBackend // 必填；List / Cancel

	// Jobs（任务进度补充视图）
	Jobs JobsBackend // 可选

	// Approvals（v8 控制台独立维护的 pending 队列）
	Approvals ApprovalsBackend // 必填

	// Events：用于转发 /api/v1/console/events SSE 流。
	Events EventStream // 必填

	// ConsoleState：UI 偏好的 KV 持久化。
	ConsoleState StateBackend // 可选

	// Audit：审计日志查询 + 导出（v8 P0）。可选；缺省返回 503。
	Audit AuditBackend
}

// New 构造一个 ConsoleServer。cfg.AuthToken 必填（出于最小安全约束）。
//
// 同时把 embed.FS 注入到 spaFS（P8-2 替换 web_dist 内容后无需改动）。
func New(cfg Config, deps Deps) *ConsoleServer {
	s := &ConsoleServer{cfg: cfg, Deps: deps, version: "dev", spaFS: spaFS()}
	return s
}

// SetSPAFS 注入 embed.FS 子树（仅暴露 index.html 所在目录）。在
// embed.go 中由 //go:embed 指令产生的 FS 调用此方法。
//
// 传 nil 时不覆盖（New 已自动注入 spaFS）。
func (s *ConsoleServer) SetSPAFS(fsys fs.FS) {
	if fsys == nil {
		return
	}
	s.spaFS = fsys
}

// Version 返回当前版本字符串（构建期通过 -ldflags 注入）。
func (s *ConsoleServer) Version() string { return s.version }

// SetVersion 在 main 启动时调用，传入编译期版本号。
func (s *ConsoleServer) SetVersion(v string) { s.version = v }

// Handler 返回完整 Console HTTP handler：包含 /api/v1/console/* 与
// /console/* 两条路由树。鉴权统一由 internalAuth 中间件处理（除
// /healthz 之外）。
//
// 内部 mux 以 /health /sessions 等相对路径注册；外层用
// http.StripPrefix("/api/v1/console", ...) 把绝对路径转成相对路径。
func (s *ConsoleServer) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /health", s.handleHealth)
	api.HandleFunc("GET /sessions", s.handleListSessions)
	api.HandleFunc("POST /sessions", s.handleCreateSession)
	api.HandleFunc("GET /sessions/{sid}", s.handleGetSession)
	api.HandleFunc("DELETE /sessions/{sid}", s.handleDeleteSession)
	api.HandleFunc("POST /sessions/{sid}/messages", s.handlePostMessage)
	api.HandleFunc("PATCH /sessions/{sid}/messages/{seq}", s.handleEditMessage)
	api.HandleFunc("DELETE /sessions/{sid}/messages/{seq}", s.handleDeleteMessage)

	api.HandleFunc("GET /plugins", s.handleListPlugins)
	api.HandleFunc("POST /plugins/{name}/enable", s.handleEnablePlugin)
	api.HandleFunc("POST /plugins/{name}/disable", s.handleDisablePlugin)
	api.HandleFunc("POST /plugins/{name}/install", s.handleInstallPlugin)
	api.HandleFunc("POST /plugins/{name}/uninstall", s.handleUninstallPlugin)

	api.HandleFunc("GET /models", s.handleListModels)
	api.HandleFunc("POST /models", s.handleCreateModel)
	api.HandleFunc("PUT /models/{channel}", s.handleUpdateModel)
	api.HandleFunc("DELETE /models/{channel}", s.handleRemoveModel)
	api.HandleFunc("POST /models/{channel}/ping", s.handlePingModel)

	api.HandleFunc("GET /tasks", s.handleListTasks)
	api.HandleFunc("POST /tasks", s.handleSubmitTask)
	api.HandleFunc("POST /tasks/{id}/cancel", s.handleCancelTask)
	api.HandleFunc("POST /tasks/{id}/retry", s.handleRetryTask)

	api.HandleFunc("POST /approvals/decide-batch", s.handleDecideApprovalsBatch)

	api.HandleFunc("GET /approvals", s.handleListApprovals)
	api.HandleFunc("POST /approvals/{id}/decide", s.handleDecideApproval)

	api.HandleFunc("GET /events", s.handleEvents)
	api.HandleFunc("GET /state", s.handleGetState)
	api.HandleFunc("PUT /state", s.handlePutState)
	api.HandleFunc("GET /audit", s.handleQueryAudit)
	api.HandleFunc("GET /audit/export", s.handleExportAudit)

	// 内部 mux 用相对路径；外层加一层 normalize 让 path 去掉 trailing
	// slash（与 mux 注册风格一致）。
	authed := s.bearerAuth(api)
	stripped := http.StripPrefix("/api/v1/console", authed)
	top := http.NewServeMux()
	// 把 trailing-slash 注册到 stripped 上，让 mux 把 /x/ 也路由到 /x
	top.Handle("/api/v1/console/", trailingSlashStripper{stripped})
	top.Handle("/api/v1/console", trailingSlashStripper{stripped})
	top.Handle("/console/", s.spaHandler())
	top.Handle("/console", http.RedirectHandler("/console/", http.StatusMovedPermanently))
	return top
}

// trailingSlashStripper 把请求 path 的 trailing slash 去掉，再交给
// next。这样 mux 用无 trailing 注册的路由也能命中 /foo/。
type trailingSlashStripper struct{ next http.Handler }

func (t trailingSlashStripper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-1] == '/' {
		r2 := r.Clone(r.Context())
		r2.URL.Path = r.URL.Path[:len(r.URL.Path)-1]
		t.next.ServeHTTP(w, r2)
		return
	}
	t.next.ServeHTTP(w, r)
}

// bearerAuth 对 next 包装 Bearer 校验。/health 类不要求（StripPrefix
// 后）；空 token 时所有 endpoint 拒绝（防止误启动无鉴权 Console）。
func (s *ConsoleServer) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 判定 StripPrefix 后的相对路径
		path := r.URL.Path
		if i := strings.Index(path, "/api/v1/console"); i == 0 {
			path = strings.TrimPrefix(path, "/api/v1/console")
			if path == "" {
				path = "/"
			}
		}
		if strings.HasPrefix(path, "/health") {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.AuthToken == "" {
			writeError(w, http.StatusServiceUnavailable, "console: auth token not configured")
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AuthToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler 返回 SPA 文件 handler：/console/index.html 直接返回，
// 其他 /console/foo 当文件不存在时 fallback 到 index.html（前端路由
// 接管）。
func (s *ConsoleServer) spaHandler() http.Handler {
	if s.spaFS == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "console: spa assets not embedded", http.StatusNotFound)
		})
	}
	root := "/console/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, root) {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, root)
		if rel == "" {
			rel = "index.html"
		}
		// 探测：若文件不存在则 fallback 到 index.html（SPA fallback）
		if _, err := fs.Stat(s.spaFS, rel); err != nil {
			rel = "index.html"
		}
		// 用 http.ServeFileFS 直接 serve（避免 FileServer 的目录 301）
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.spaFS, rel)
	})
}
