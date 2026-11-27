// Command dsh 是 DeepSeek Harness Go CLI 的入口。
//
// 它从 -config（默认 ./harness.yml）加载配置，按 llm.provider 装配
// LLM 客户端（ARCHITECTURE.md §6），注册内置工具，然后根据参数执行
// 以下之一：
//
//	-prompt <text>   非交互式运行单条 prompt 后退出
//	-serve           启动 HTTP+SSE 服务器（ARCHITECTURE.md §13）并阻塞
//	（默认）          进入交互式 REPL
//
// v3 新增能力（详见 ARCHITECTURE.md）：
//
//	skills 加载并注入 runner（mention / always-on 触发）—— §9
//	agent_spawn 子 agent 工具注册 —— §8
//	obs.provider=otel 时启用 OTel tracer（默认 noop 零开销）—— §11
//	compaction.enabled 时挂载上下文压缩器 —— §10
//	--audit / cfg.audit.path 打开 JSONL 审计日志 —— §11
//	sandbox.provider 应用到 shell 工具子进程 —— §12
//
// 信号处理：
//
//	-os.Interrupt、-syscall.SIGTERM  → 取消当前 REPL 轮次
//	                                    （REPL 本身继续运行）
//	-stdin EOF（Ctrl+D）              → 干净地退出 REPL
//	-stdin "exit" / "quit"            → 干净地退出 REPL
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"deepseek-harness-go/cmd/dsh/repl"
	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/audit"
	"deepseek-harness-go/internal/compaction"
	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/llm/provider"
	"deepseek-harness-go/internal/obs"
	"deepseek-harness-go/internal/plugin"
	"deepseek-harness-go/internal/sandbox"
	"deepseek-harness-go/internal/server"
	"deepseek-harness-go/internal/skill"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/subagent"
	"deepseek-harness-go/internal/tool"
	"deepseek-harness-go/internal/tools"
	"deepseek-harness-go/internal/usage"
	"deepseek-harness-go/internal/webui"
)

var (
	configPath = flag.String("config", "harness.yml", "harness.yml 的路径（留空则只用默认值 + 环境变量）")
	prompt     = flag.String("prompt", "", "非空时只执行这一条 prompt 后退出（不进入 REPL）")
	serveFlag  = flag.Bool("serve", false, "启动 HTTP+SSE 服务（ARCHITECTURE.md §13）而不是 REPL；要求 cfg.server.enabled=true")
	debug      = flag.Bool("debug", false, "把所有 agent 事件打印到 stderr")
	auditPath  = flag.String("audit", "", "audit.jsonl 的路径（ARCHITECTURE.md §11）；优先于 cfg.audit.path，留空则关闭审计")

	// --plugin 可重复使用：每次出现追加一个路径。flag.StringVar 做不到
	// 这一点，因此我们注册一个自定义函数来向 plugins 追加。
	plugins pluginPaths
)

// pluginPaths 是 --plugin（可重复）背后的累加器。
type pluginPaths []string

func (p *pluginPaths) String() string { return strings.Join(*p, ",") }

func (p *pluginPaths) Set(v string) error {
	*p = append(*p, v)
	return nil
}

func init() {
	flag.Var(&plugins, "plugin", "插件可执行文件路径（ARCHITECTURE.md §14）；可重复。每出现一次就拉起一个 gRPC 子进程，并把它的工具合并进注册表。")
}

// version 在发布时通过 -ldflags 注入。默认值让它在开发构建中
// 一目了然。
var version = "3.0.0-dev"

func main() {
	// flag.Parse() 解析命令行参数，把上面用 flag.String / flag.Bool /
	// flag.Var 注册的 -config、-prompt、-serve、-debug、-audit、-plugin
	// 写进各自的变量里。此后所有代码都通过解引用（如 *configPath、
	// *serveFlag）读取解析结果。
	//
	// 它必须出现在读取这些变量的任何代码之前，所以放在 main 的第一行。
	// 出错行为：遇到未定义的参数会打印用法并以退出码 2 退出；
	// -h / --help 则打印用法后以退出码 0 退出。
	flag.Parse()

	// rootCtx 是长期存活的 context。SIGINT/SIGTERM 会取消它；
	// REPL 也会从它派生每轮次专用的子 context。
	rootCtx, stop := signal.NotifyContext(rootContext(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[dsh] config: %v", err)
	}

	// 确保工作区存在，供文件系统工具使用。
	if err := os.MkdirAll(cfg.Agent.WorkspaceRoot, 0o755); err != nil {
		log.Fatalf("[dsh] mkdir workspace: %v", err)
	}

	// ARCHITECTURE.md §11：可观测性。provider=noop（默认）零开销；otel 时构建
	// OTLP tracer；debug 时日志走 stderr logger。
	obsProvider, otelHandle := setupObs(rootCtx, cfg, *debug || cfg.Agent.Debug)
	if otelHandle != nil {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelHandle.Close(closeCtx); err != nil {
				log.Printf("[dsh] obs close: %v", err)
			}
		}()
	}

	// ARCHITECTURE.md §6：provider 路由（openai | anthropic | ollama | gemini）。
	llmClient, err := provider.NewClient(cfg.LLM)
	if err != nil {
		log.Fatalf("[dsh] llm: %v", err)
	}

	reg := tool.NewRegistry()
	tools.MustRegisterBuiltin(reg, cfg.Agent.WorkspaceRoot)
	toolGlobal = reg // 暴露给 loadPlugins 填充

	// ARCHITECTURE.md §12：OS sandbox（默认 noop）。应用于 shell 工具的子进程。
	sb, err := sandbox.New(cfg.Sandbox.Provider)
	if err != nil {
		log.Fatalf("[dsh] sandbox: %v", err)
	}
	if sb.Name() != "noop" {
		log.Printf("[dsh] sandbox: %s", sb.Name())
	}

	// shell 工具装配（ARCHITECTURE.md §7 + v3 ARCHITECTURE.md §12）。allowlist 为空时
	// 工具默认拒绝所有命令。
	if cfg.Shell.Enabled {
		shellTool := tools.NewShellTool(tools.ShellConfig{
			Enabled:        true,
			AllowList:      cfg.Shell.AllowList,
			Timeout:        cfg.Shell.Timeout,
			MaxOutputBytes: 1 << 20,
			WorkspaceRoot:  cfg.Agent.WorkspaceRoot,
		}, nil)
		shellTool.Sandbox = sb
		if err := reg.Register(shellTool); err != nil {
			log.Fatalf("[dsh] register shell: %v", err)
		}
	}

	// v2 插件系统（ARCHITECTURE.md §14）。提供 --plugin 时，将每个插件作为
	// 子 gRPC 服务器启动，建立连接，并把其工具合并进注册表。失败即致命
	// —— 宁可拒绝启动，也不能静默丢弃用户明确要求的工具。
	if len(plugins) > 0 {
		ps := make([]*string, len(plugins))
		for i := range plugins {
			s := plugins[i]
			ps[i] = &s
		}
		hosts := loadPlugins(rootCtx, ps)
		defer func() {
			for _, h := range hosts {
				_ = h.Close()
			}
		}()
	}

	// ARCHITECTURE.md §8：注册 agent_spawn 子 agent 工具。子 runner 共享 LLM
	// client 与工具注册表（不含 agent_spawn 自身——禁止嵌套）。
	// 必须在插件加载之后调用，子 agent 才能继承插件工具。
	if _, err := subagent.Attach(reg, llmClient, cfg.LLM.Model,
		cfg.Agent.MaxRounds, cfg.LLM.MaxTokens, cfg.Agent.Temperature); err != nil {
		log.Fatalf("[dsh] subagent: %v", err)
	}

	sys := agent.NewDefaultSystemPrompt(cfg.Agent.SystemPrompt)
	runner := agent.NewLoopRunner(
		llmClient, reg, sys,
		cfg.LLM.Model, cfg.Agent.MaxRounds,
		cfg.LLM.MaxTokens, cfg.Agent.Temperature,
	)
	runner.Obs = obsProvider

	// ARCHITECTURE.md §9：加载 skills（mention / always-on 触发注入）。
	loadedSkills, err := loadSkills(cfg.Skills.Dir)
	if err != nil {
		log.Fatalf("[dsh] skills: %v", err)
	}
	runner.Skills = loadedSkills
	if len(loadedSkills) > 0 {
		log.Printf("[dsh] skills: loaded %d from %s", len(loadedSkills), skillsDir(cfg.Skills.Dir))
	}

	// ARCHITECTURE.md §10：上下文压缩（默认关闭）。
	if cfg.Compaction.Enabled {
		var strat compaction.Strategy
		switch cfg.Compaction.Strategy {
		case "llm-summary":
			strat = compaction.LLMSummaryStrategy{
				Client:     llmClient,
				Model:      cfg.LLM.Model,
				KeepRecent: cfg.Compaction.KeepRecent,
			}
		default:
			strat = compaction.TruncateStrategy{KeepRecent: cfg.Compaction.KeepRecent}
		}
		runner.Compactor = &compaction.Compactor{
			Strategy:   strat,
			Trigger:    cfg.Compaction.TriggerTokens,
			KeepRecent: cfg.Compaction.KeepRecent,
		}
		log.Printf("[dsh] compaction: strategy=%s trigger=%d keep-recent=%d",
			cfg.Compaction.Strategy, cfg.Compaction.TriggerTokens, cfg.Compaction.KeepRecent)
	}

	// ARCHITECTURE.md §11：审计日志。--audit 优先于 cfg.audit.path；两者都为空时关闭。
	if path := resolveAuditPath(cfg); path != "" {
		al, err := audit.NewFileLogger(path, !cfg.Audit.Full)
		if err != nil {
			log.Fatalf("[dsh] audit: %v", err)
		}
		runner.Audit = al
		defer func() { _ = al.Close() }()
		log.Printf("[dsh] audit: %s (redact=%v)", path, !cfg.Audit.Full)
	}

	// v2 持久化 + 用量统计：在此接线，使 REPL 和 -serve 都能受益。
	st, err := openStore(rootCtx, cfg)
	if err != nil {
		log.Fatalf("[dsh] store: %v", err)
	}
	if st != nil {
		runner.Store = st
		defer st.Close()
	}

	// v2 用量聚合（ARCHITECTURE.md §5.1）。空的 PriceMap → 成本快照为 0，
	// 但 token 计数仍会记录。
	tracker := usage.NewTracker(usage.PriceMap{}, "USD")
	_ = tracker

	// CLI 标志优先于配置；即使 cfg server.enabled 为 false，-serve 也
	// 隐含服务器模式（缺少认证配置时会给出警告）。
	if *serveFlag {
		if !cfg.Server.Enabled {
			log.Printf("[dsh] warning: -serve given but cfg.server.enabled=false; forcing on")
			cfg.Server.Enabled = true
		}
		runServer(rootCtx, runner, st, cfg)
		return
	}

	if *prompt != "" {
		useDebug := *debug || cfg.Agent.Debug
		repl.RunOnce(rootCtx, runner, *prompt, useDebug)
		return
	}
	fmt.Fprintf(os.Stderr, "dsh %s — workspace=%s model=%s provider=%s\n",
		version, absPath(cfg.Agent.WorkspaceRoot), cfg.LLM.Model, providerName(cfg))
	useDebug := *debug || cfg.Agent.Debug
	repl.RunWith(rootCtx, runner, useDebug, repl.Options{
		Skills: loadedSkills,
	}, &repl.REPLState{})
}

// setupObs 按 cfg.Obs 构建 obs.Provider（ARCHITECTURE.md §11 / ARCHITECTURE.md §11）。
// 返回的 handle 非 nil 时（otel 模式），调用方负责在退出前 Close。
func setupObs(ctx context.Context, cfg config.Config, debug bool) (obs.Provider, *obs.OTelTracerHandle) {
	p := obs.Defaults()
	if debug {
		p.Logger = obs.NewStderrLogger()
	}
	if cfg.Obs.Provider != "otel" {
		return p, nil
	}
	handle, err := obs.NewOTelTracer(ctx, obs.OTelConfig{
		Endpoint:    cfg.Obs.Endpoint,
		ServiceName: cfg.Obs.ServiceName,
		SampleRatio: cfg.Obs.SampleRatio,
	})
	if err != nil {
		log.Fatalf("[dsh] obs otel: %v", err)
	}
	log.Printf("[dsh] obs: otel endpoint=%s service=%s", cfg.Obs.Endpoint, cfg.Obs.ServiceName)
	p.Tracer = handle
	p.Meter = handle.Meter()
	return p, handle
}

// loadSkills 读取 skill 目录（ARCHITECTURE.md §6）。目录不存在 → 空列表不报错；
// 文件解析失败（frontmatter 缺字段等）→ 致命错误，不静默（ARCHITECTURE.md §9）。
func loadSkills(dir string) ([]skill.Skill, error) {
	path := skillsDir(dir)
	return skill.NewFileLoader().Load(path)
}

// skillsDir 解析 skills 目录：显式配置优先；空值回退 ~/.dsh/skills。
func skillsDir(dir string) string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", "skills")
}

// resolveAuditPath 返回审计日志路径；未启用时返回空串。
func resolveAuditPath(cfg config.Config) string {
	if *auditPath != "" {
		return *auditPath
	}
	return cfg.Audit.Path
}

// providerName 返回展示用的 provider 标识。
func providerName(cfg config.Config) string {
	if cfg.LLM.Provider == "" {
		return "openai"
	}
	return cfg.LLM.Provider
}

// openStore 返回配置好的 store.Store；未要求持久化时返回 nil。
// 仅 -prompt 模式可通过保持 server.enabled 为 false 来关闭持久化；
// 显式 REPL 模式默认仍使用内存存储，除非 cfg.Agent.WorkspaceRoot
// 中存在同级的 dsh.db 文件。
//
// 为简化接线，REPL 模式下我们总是尝试内存 MapStore（使 /resume 在
// 没有 SQLite 时也能工作）；当 cfg.Server.Enabled 或
// cfg.Agent.Persistence == "sqlite" 时则使用 SQLite 文件。
func openStore(ctx context.Context, cfg config.Config) (store.Store, error) {
	if *serveFlag || cfg.Server.Enabled {
		// 服务器模式：使用 SQLite，使会话在重启后仍可保留。
		path := filepath.Join(cfg.Agent.WorkspaceRoot, "dsh.db")
		st, err := store.NewSQLiteStore(path)
		if err != nil {
			return nil, fmt.Errorf("open sqlite %s: %w", path, err)
		}
		log.Printf("[dsh] store: sqlite %s", path)
		return st, nil
	}
	// REPL/prompt 模式：使用内存存储，使 /resume 和 Append 在进程存续
	// 期间可用。SQLite 会锁定工作区目录，给用户带来困扰；如需可
	// 通过 cfg 显式开启。
	_ = ctx
	return store.NewMapStore(), nil
}

// runServer 在 HTTP 监听器上阻塞，直到 ctx 被取消。
func runServer(ctx context.Context, runner *agent.LoopRunner, st store.Store, cfg config.Config) {
	if strings.TrimSpace(cfg.Server.AuthToken) == "" {
		log.Fatalf("[dsh] server: cfg.server.auth-token (DSH_SERVER_AUTH_TOKEN) must be set before -serve")
	}
	srv := server.New(server.Config{
		Listen:      cfg.Server.Listen,
		AuthToken:   cfg.Server.AuthToken,
		Timeout:     int(cfg.Server.Timeout.Seconds()),
		MaxSessions: cfg.Server.MaxSessions,
	}, runner, st)

	// 挂载前端 SPA（内嵌的 web/dist）。未执行 `npm run build` 时
	// Available() 返回 false —— 此时退化为纯 API 模式，不影响使用。
	if webui.Available() {
		srv.SetUI(webui.Handler())
		log.Printf("[dsh] serve: web console at http://%s/", cfg.Server.Listen)
	} else {
		log.Printf("[dsh] serve: web console not built " +
			"(run `npm run build` in deepseek-harness-go/web); serving API only")
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("[dsh] serve: listening on %s (model=%s)", cfg.Server.Listen, cfg.LLM.Model)

	// 在 goroutine 中运行；通过 ctx 取消。
	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Printf("[dsh] serve: shutting down (%v)", ctx.Err())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("[dsh] serve: shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("[dsh] serve: %v", err)
		}
	}
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// rootContext 单独拆出，方便测试日后覆盖父 context。
func rootContext() context.Context { return context.Background() }

// loadPlugins 启动每个插件二进制，连接其 gRPC 服务器，并把它的工具
// 注册到进程内的 tool.Registry 中。返回宿主句柄，供调用者在关闭时
// 清理。
//
// 失败即致命：如果用户显式传入 --plugin，静默丢弃工具比拒绝启动
// 更糟。
func loadPlugins(ctx context.Context, paths []*string) []*plugin.Host {
	hosts := make([]*plugin.Host, 0, len(paths))
	for _, p := range paths {
		if *p == "" {
			continue
		}
		h := plugin.NewHost(plugin.Config{
			Command:     []string{*p},
			MaxRestarts: 3,
			StartupWait: 5 * time.Second,
		})
		if err := h.Start(ctx); err != nil {
			log.Fatalf("[dsh] plugin %s: start: %v", *p, err)
		}
		// 启动后台监视器，在插件崩溃时自动重启（ARCHITECTURE.md §14）。
		// Start 已在上面调用过；Watch 只是添加监视 goroutine，
		// 监视器本身出错不会导致致命失败。
		go func() { _ = h.Watch(ctx) }()
		// 等待插件的 gRPC 服务器接受连接。Start 只是 fork 进程；
		// 插件二进制需要一点时间才能调用 ServeForever。我们采用轮询
		// 而非固定的 StartupWait，因为那是针对每个二进制可调的。
		if err := waitForPlugin(ctx, h.Addr(), 10*time.Second); err != nil {
			log.Fatalf("[dsh] plugin %s: not ready: %v", *p, err)
		}
		// 建立 gRPC 客户端连接。这同时会获取 Specs()，以确认插件
		// 是否健康。
		client, err := plugin.NewClient(ctx, h.Addr(), plugin.WithDialTimeout(10*time.Second))
		if err != nil {
			log.Fatalf("[dsh] plugin %s: dial: %v", *p, err)
		}
		added, names, err := client.Register(toolGlobal, plugin.ApproverFunc(func(_ context.Context, _ plugin.Request) (plugin.Decision, error) {
			// CLI 场景下我们直接自动批准；面向模型的审批系统
			// （ARCHITECTURE.md §7）位于 tools/shell.go。
			return plugin.ApproveOnce, nil
		}))
		if err != nil {
			log.Fatalf("[dsh] plugin %s: register: %v", *p, err)
		}
		log.Printf("[dsh] plugin %s: registered %d tools: %v", *p, added, names)
		hosts = append(hosts, h)
	}
	return hosts
}

// toolGlobal 是 loadPlugins 填充的全局 Registry。它在 main() 中、
// loadPlugins 被调用之前完成设置。
var toolGlobal *tool.Registry

// waitForPlugin 通过 TCP 拨号轮询插件的 gRPC 端口，直到其接受连接
// 或超时。这里不使用 gRPC 协议，因为我们希望对死端口快速失败，
// 而不必为一次性的探测引入 gRPC 客户端。
func waitForPlugin(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("plugin %s not ready after %s", addr, timeout)
}
