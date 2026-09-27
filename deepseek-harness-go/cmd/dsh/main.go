// Command dsh 是 DeepSeek Harness Go CLI 的入口。
//
// 它从 -config（默认 ./harness.yml）加载配置，按 llm.provider 装配
// LLM 客户端（DESIGN-v3 §G.3），注册内置工具，然后根据参数执行
// 以下之一：
//
//	-prompt <text>   非交互式运行单条 prompt 后退出
//	-serve           启动 HTTP+SSE 服务器（DESIGN-v2 §A.2）并阻塞
//	（默认）          进入交互式 REPL
//
// v3 新增（DESIGN-v3 §A/§B/§C/§D/§E/§F）：
//
//	skills 加载并注入 runner（mention / always-on 触发）
//	agent_spawn 子 agent 工具注册
//	obs.provider=otel 时启用 OTel tracer（默认 noop 零开销）
//	compaction.enabled 时挂载上下文压缩器
//	--audit / cfg.audit.path 打开 JSONL 审计日志
//	sandbox.provider 应用到 shell 工具子进程
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
	"crypto/rand"
	"encoding/hex"
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
	"deepseek-harness-go/internal/acp"
	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/audit"
	"deepseek-harness-go/internal/auth"
	"deepseek-harness-go/internal/compaction"
	"deepseek-harness-go/internal/config"
	"deepseek-harness-go/internal/console"
	"deepseek-harness-go/internal/credentials"
	"deepseek-harness-go/internal/goal"
	"deepseek-harness-go/internal/hook"
	"deepseek-harness-go/internal/jobs"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/obs"
	"deepseek-harness-go/internal/plugin"
	"deepseek-harness-go/internal/plugin/installer"
	"deepseek-harness-go/internal/runtime"
	"deepseek-harness-go/internal/sandbox"
	"deepseek-harness-go/internal/schedule"
	"deepseek-harness-go/internal/server"
	"deepseek-harness-go/internal/skill"
	"deepseek-harness-go/internal/storage"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/subagent"
	"deepseek-harness-go/internal/task"
	"deepseek-harness-go/internal/terminal"
	"deepseek-harness-go/internal/tool"
	"deepseek-harness-go/internal/tools"
	"deepseek-harness-go/internal/usage"
	"deepseek-harness-go/internal/webhook"
)

var (
	configPath = flag.String("config", "harness.yml", "path to harness.yml (empty for defaults + env only)")
	prompt     = flag.String("prompt", "", "if non-empty, run a single prompt and exit (no REPL)")
	serveFlag  = flag.Bool("serve", false, "start HTTP+SSE server (DESIGN-v2 §A.2) instead of REPL; cfg.server.enabled must be true")
	debug      = flag.Bool("debug", false, "print all agent events to stderr")
	auditPath  = flag.String("audit", "", "path to audit.jsonl (DESIGN-v3 §E); overrides cfg.audit.path. Empty disables auditing.")
	channelCode = flag.String("channel", "", "v5 P5-3: switch LLM channel by code; empty = use default (cfg.llm or first channel)")

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
	flag.Var(&plugins, "plugin", "path to plugin executable (DESIGN-v2 §B.2); repeatable. Spawns a child gRPC server per occurrence and merges its tools into the registry.")
}

// version 在发布时通过 -ldflags 注入。默认值让它在开发构建中
// 一目了然。
var version = "8.0.0-dev"

func main() {
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

	// v3 §C：可观测性。provider=noop（默认）零开销；otel 时构建
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

	// v5 P5-4: 凭据抽象。从 env + 可选 JSON 文件解析 cfg.LLM.APIKey 与
	// cfg.Channels[].APIKey。文件不存在时仍走 env 单源；所有来源都查不到
	// 也不致命（LLM Client 构造时 cfg.LLM.APIKey 仍可能是空字符串 → 后续 Validate）。
	resolveLLMAPIKey(&cfg.LLM)
	if cfg.Credentials.File != "" {
		credFile = cfg.Credentials.File
	}
	for i := range cfg.Channels {
		resolveChannelAPIKey(&cfg.Channels[i])
	}

	// v3 §G：provider 路由（openai | anthropic | ollama | gemini）。
	// v5 P5-3 起：先尝试用 cfg.Channels 构建多渠道注册表；为空时
	// 退化为单渠道（cfg.LLM 注册为 "default"）。
	channelReg, err := runtime.NewRegistry(cfg.Channels, cfg.LLM)
	if err != nil {
		log.Fatalf("[dsh] channel registry: %v", err)
	}
	if len(channelReg.Codes()) > 0 {
		log.Printf("[dsh] channels: %v", channelReg.Codes())
	}
	code := *channelCode
	if code == "" {
		code = "default"
	}
	llmClient, err := channelReg.Resolve(code)
	if err != nil {
		log.Fatalf("[dsh] channel %q: %v", code, err)
	}
	if model, mErr := channelReg.ResolveModel(code); mErr == nil {
		// 用 channel 绑定的模型名覆盖 cfg.LLM.Model；
		// 允许 cfg.LLM.Model 与 channel 模型不同。
		log.Printf("[dsh] channel %q → model=%s", code, model)
	}

	reg := tool.NewRegistry()
	tools.MustRegisterBuiltin(reg, cfg.Agent.WorkspaceRoot)

	// v6 P6-3: 注册 jobs_* 工具（共享全局 Registry holder；main 启动后
	// 通过 SetJobsRegistry 注入）。
	for _, t := range []tool.Tool{
		tools.NewJobsRunTool(),
		tools.NewJobsListTool(),
		tools.NewJobsOutputTool(),
		tools.NewJobsKillTool(),
	} {
		if err := reg.Register(t); err != nil {
			log.Fatalf("[dsh] register jobs tool: %v", err)
		}
	}

	// v6 P6-4: 注册 todo_* 工具。
	for _, t := range []tool.Tool{
		tools.NewTodoWriteTool(),
		tools.NewTodoReadTool(),
	} {
		if err := reg.Register(t); err != nil {
			log.Fatalf("[dsh] register todo tool: %v", err)
		}
	}
	goalStore := goal.NewStore()
	tools.SetGoalStore(goalStore)

	// v6 P6-5: 注册 terminal_* 工具（共享全局 Registry holder）。
	for _, t := range []tool.Tool{
		tools.NewTerminalRunTool(),
		tools.NewTerminalReadTool(),
		tools.NewTerminalKillTool(),
	} {
		if err := reg.Register(t); err != nil {
			log.Fatalf("[dsh] register terminal tool: %v", err)
		}
	}
	termReg := terminal.NewRegistry(1000)
	tools.SetTerminalRegistry(termReg)

	// v6 P6-6: KV 工具（共享全局 storage holder）。
	for _, t := range []tool.Tool{
		tools.NewKVSetTool(),
		tools.NewKVGetTool(),
		tools.NewKVDeleteTool(),
		tools.NewKVListTool(),
	} {
		if err := reg.Register(t); err != nil {
			log.Fatalf("[dsh] register kv tool: %v", err)
		}
	}

	// v6 P6-6: KV 存储（内存 + 文件链式）。
	storeDir := filepath.Join(cfg.Agent.WorkspaceRoot, "storage")
	fileStore, err := storage.NewFileStorage(storeDir)
	if err != nil {
		log.Fatalf("[dsh] storage: %v", err)
	}
	kv := storage.NewChainedStorage(storage.NewMemoryStorage(), fileStore)
	tools.SetKV(kv)
	log.Printf("[dsh] storage: active at %s", storeDir)

	// v6 P6-3: 创建 jobs registry（在持久化模式下用 SQLite；其他用内存）。
	var jobsReg *jobs.Registry
	if *serveFlag || cfg.Server.Enabled {
		path := filepath.Join(cfg.Agent.WorkspaceRoot, "jobs.db")
		// v6 内置 MemoryStore；SQLite 留 v6.1。共享内存即可。
		_ = path
		jobsReg = jobs.NewRegistry(jobs.NewMemoryStore(), jobs.NewShellRunner(), 1000)
		tools.SetJobsRegistry(jobsReg)
		log.Printf("[dsh] jobs: registry active (in-memory, 1000-line buffer)")
	}

	toolGlobal = reg // 暴露给 loadPlugins 填充

	// v3 §F：OS sandbox（默认 noop）。应用于 shell 工具的子进程。
	sb, err := sandbox.New(cfg.Sandbox.Provider)
	if err != nil {
		log.Fatalf("[dsh] sandbox: %v", err)
	}
	if sb.Name() != "noop" {
		log.Printf("[dsh] sandbox: %s", sb.Name())
	}

	// shell 工具装配（DESIGN-v2 §C.3 + v3 §F.4）。allowlist 为空时
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

	// v2 插件系统（DESIGN-v2 §B）。提供 --plugin 时，将每个插件作为
	// 子 gRPC 服务器启动，建立连接，并把其工具合并进注册表。失败即致命
	// —— 宁可拒绝启动，也不能静默丢弃用户明确要求的工具。
	var pluginClients []*plugin.Client
	if len(plugins) > 0 {
		ps := make([]*string, len(plugins))
		for i := range plugins {
			s := plugins[i]
			ps[i] = &s
		}
		hosts, clients := loadPlugins(rootCtx, ps)
		pluginClients = clients
		defer func() {
			for _, h := range hosts {
				_ = h.Close()
			}
		}()
	}

	// v3 §B：注册 agent_spawn 子 agent 工具。子 runner 共享 LLM
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

	// v3 §A：加载 skills（mention / always-on 触发注入）。
	loadedSkills, err := loadSkills(cfg.Skills.Dir)
	if err != nil {
		log.Fatalf("[dsh] skills: %v", err)
	}
	runner.Skills = loadedSkills
	if len(loadedSkills) > 0 {
		log.Printf("[dsh] skills: loaded %d from %s", len(loadedSkills), skillsDir(cfg.Skills.Dir))
	}

	// v3 §D：上下文压缩（默认关闭）。
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

	// v3 §E：审计日志。--audit 优先于 cfg.audit.path；两者都为空时关闭。
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

	// v2 用量聚合（DESIGN-v2 §A.4）。空的 PriceMap → 成本快照为 0，
	// 但 token 计数仍会记录。
	tracker := usage.NewTracker(usage.PriceMap{}, "USD")
	_ = tracker

	// v5 P5-2: 进程内 token 计量域（多 session 累计 + cache/reasoning）。
	meter := usage.NewMemoryMeter()
	runner.Meter = meter

	// v5 P5-6: 工具执行钩子（默认空 registry；外部可通过 cfg.hooks 扩展）。
	hookReg := hook.NewRegistry()
	runner.Hooks = hookReg

	// CLI 标志优先于配置；即使 cfg server.enabled 为 false，-serve 也
	// 隐含服务器模式（缺少认证配置时会给出警告）。
	if *serveFlag {
		if !cfg.Server.Enabled {
			log.Printf("[dsh] warning: -serve given but cfg.server.enabled=false; forcing on")
			cfg.Server.Enabled = true
		}
		// v6 P6-1: 打开 Task store（与 sessions 共享工作区目录）。
		taskStore, tClose, err := openTaskStore(rootCtx, cfg)
		if err != nil {
			log.Fatalf("[dsh] task store: %v", err)
		}
		if tClose != nil {
			defer tClose()
		}
		taskExec := task.NewLoopExecutor(taskStore, runner)
		defer func() {
			if jobsReg != nil {
				_ = jobsReg.Close(rootCtx)
			}
		}()
		runServer(rootCtx, runner, st, meter, cfg, pluginClients, taskExec, jobsReg)
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

// setupObs 按 cfg.Obs 构建 obs.Provider（§C.2 / §C.4）。
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

// loadSkills 读取 skill 目录（§A.1）。目录不存在 → 空列表不报错；
// 文件解析失败（frontmatter 缺字段等）→ 致命错误，不静默（§A.5）。
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

// openTaskStore 在 -serve 模式下打开任务存储；非 -serve 时返回内存。
//
// 设计：与 sessions 共享工作区（dsh.db），但用独立 SQLite 文件
// tasks.db；关闭函数由 main 在退出前 defer。
func openTaskStore(ctx context.Context, cfg config.Config) (task.Store, func(), error) {
	_ = ctx
	if !*serveFlag && !cfg.Server.Enabled {
		// 非服务器模式：内存存储足够；无关闭动作。
		return task.NewMemoryStore(), nil, nil
	}
	path := filepath.Join(cfg.Agent.WorkspaceRoot, "tasks.db")
	st, err := task.NewSQLiteStore(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open task sqlite %s: %w", path, err)
	}
	log.Printf("[dsh] task store: sqlite %s", path)
	closeFn := func() { _ = st.Close() }
	return st, closeFn, nil
}

// runServer 在 HTTP 监听器上阻塞，直到 ctx 被取消。
func runServer(ctx context.Context, runner *agent.LoopRunner, st store.Store, meter usage.Meter, cfg config.Config, pluginClients []*plugin.Client, taskExec task.Executor, jobsReg *jobs.Registry) {
	if strings.TrimSpace(cfg.Server.AuthToken) == "" {
		log.Fatalf("[dsh] server: cfg.server.auth-token (DSH_SERVER_AUTH_TOKEN) must be set before -serve")
	}
	srv := server.New(server.Config{
		Listen:      cfg.Server.Listen,
		AuthToken:   cfg.Server.AuthToken,
		Timeout:     int(cfg.Server.Timeout.Seconds()),
		MaxSessions: cfg.Server.MaxSessions,
	}, runner, st)

	// v4 P3: 注入 Gateway inventory（本地工具 + 远端 gRPC 插件）。
	combined := plugin.NewCombined()
	combined.Add(&plugin.LocalInventory{Name: "local", Reg: toolGlobal})
	if len(pluginClients) > 0 {
		grpcInv := plugin.NewGRPCInventory()
		for i, c := range pluginClients {
			grpcInv.Add(fmt.Sprintf("plugin-%d", i+1), c)
		}
		combined.Add(grpcInv)
	}
	srv.SetInventory(combined)
	srv.SetLLMClient(runner.Client)
	srv.SetMeter(meter)
	if taskExec != nil {
		srv.SetTasksExecutor(taskExec)
	}
	if jobsReg != nil {
		srv.SetJobsRegistry(jobsReg)
	}

	// v7 P7-3: MCP HTTP dispatcher — 直接桥接 Gateway router 的
	// tools.list / tools.call source，使 MCP 客户端可通过 HTTP 调用。
	srv.SetMCPDispatcher(server.MCPDispatcher(srv.Router()))

	// v7 P7-4: ACP 服务端 — 把 v6 Task 1:1 映射为 ACP session。
	acpSrv := acp.NewServer(
		acp.NewTaskExecutorAdapter(taskExec),
		nil, // shared KV via v6 storage 留 v7.1
		cfg.Server.AuthToken,
	)
	srv.SetACPServer(acpSrv)
	log.Printf("[dsh] serve: ACP /acp/* + MCP /mcp exposed")
	runner.Meter = meter

	// v8 P8-1: Web Console 后端（/console/* + /api/v1/console/*）。
	// 通过 server.SetExtraHandler 注入到同一顶层 mux，复用同一
	// listener 和 token。
	statusStore, err := installer.NewStatusStore(filepath.Join(cfg.Agent.WorkspaceRoot, "plugin_status.json"))
	if err != nil {
		log.Printf("[dsh] console: plugin status store unavailable: %v (continuing without)", err)
	}
	// v8.1：本地用户 / JWT 鉴权后端。
	authDBPath := filepath.Join(cfg.Agent.WorkspaceRoot, "users.db")
	authStore, err := auth.NewStore(authDBPath)
	if err != nil {
		log.Fatalf("[dsh] auth: open %s: %v", authDBPath, err)
	}
	defer authStore.Close()
	jwtSecret, err := loadOrCreateJWTSecret(cfg.Agent.WorkspaceRoot)
	if err != nil {
		log.Fatalf("[dsh] auth: jwt secret: %v", err)
	}
	bootstrapRootUser(authStore, os.Getenv("DSH_ADMIN_USER"), os.Getenv("DSH_ADMIN_PASSWORD"))
	authBackend := console.NewAuthAdapter(authStore, []byte(jwtSecret), 24*time.Hour)

	// v8.1：schedule + webhook 后端（内存存储；worker goroutine）。
	scheduleStore := schedule.NewStore()
	whDispatcher := webhook.NewDispatcher()
	defer whDispatcher.Close()
	scheduleWorker := schedule.NewWorker(scheduleStore, schedule.WebhookActionHandler(whDispatcher))
	go scheduleWorker.Run(ctx)
	consoleSrv := buildConsoleServer(cfg, runner, st, combined, taskExec, jobsReg, srv, statusStore, authBackend, scheduleStore, whDispatcher)
	srv.SetExtraHandler("/console/", consoleSrv.Handler())
	srv.SetExtraHandler("/api/v1/console/", consoleSrv.Handler())
	consoleSrv.SetVersion(version)
	consoleSrv.Blacklist = console.NewBlacklist()
	log.Printf("[dsh] serve: Web Console at http://%s/console/  (auth=JWT, ttl=24h, users=%s)", cfg.Server.Listen, authDBPath)

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
// 注册到进程内的 tool.Registry 中。返回宿主句柄 + 已拨号 client，供
// 调用者在关闭时清理。
//
// 失败即致命：如果用户显式传入 --plugin，静默丢弃工具比拒绝启动
// 更糟。
func loadPlugins(ctx context.Context, paths []*string) ([]*plugin.Host, []*plugin.Client) {
	hosts := make([]*plugin.Host, 0, len(paths))
	clients := make([]*plugin.Client, 0, len(paths))
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
		// 启动后台监视器，在插件崩溃时自动重启（DESIGN-v2 §B.5）。
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
			// （DESIGN-v2 §C）位于 tools/shell.go。
			return plugin.ApproveOnce, nil
		}))
		if err != nil {
			log.Fatalf("[dsh] plugin %s: register: %v", *p, err)
		}
		log.Printf("[dsh] plugin %s: registered %d tools: %v", *p, added, names)
		hosts = append(hosts, h)
		clients = append(clients, client)
	}
	return hosts, clients
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

// credFile 是全局缓存的凭据文件路径；首次 resolveChannelAPIKey 时初始化。
var credFile string

// initCredProvider 构造 Chained(Env + File)。
// 失败回退到只含 Env 的 Provider；零开销。
func initCredProvider() *credentials.Chained {
	providers := []credentials.Provider{credentials.NewEnvProvider()}
	if credFile != "" {
		if fp, err := credentials.NewFileProvider(credFile); err == nil {
			providers = append(providers, fp)
		} else {
			log.Printf("[dsh] credentials: file %s: %v (env-only)", credFile, err)
		}
	}
	return credentials.NewChained(providers...)
}

// resolveLLMAPIKey 把 cfg.LLM.APIKey 通过 Chained 解析；cfg.LLM.APIKey
// 已设置时跳过（保留用户显式覆盖）。解析到空则保留原值。
func resolveLLMAPIKey(cfg *config.LLMConfig) {
	if cfg.APIKey != "" {
		return
	}
	cp := initCredProvider()
	v, err := cp.Resolve(context.Background(), credentials.Ref{Name: "deepseek-api-key"})
	if err == nil {
		cfg.APIKey = v
	}
}

// resolveChannelAPIKey 用 channel code 推断 ref 名。
func resolveChannelAPIKey(ch *config.ChannelConfig) {
	if ch.APIKey != "" {
		return
	}
	cp := initCredProvider()
	name := ch.Code
	if name == "" {
		name = "channel"
	}
	v, err := cp.Resolve(context.Background(), credentials.Ref{
		Name: name + "-api-key",
		Env:  "", // 由 EnvProvider 自动 UPPER_SNAKE 拼接
	})
	if err == nil {
		ch.APIKey = v
	}
}

// buildConsoleServer 装配 v8 Console 后端（cmd/dsh 内的工厂函数）。
func buildConsoleServer(
	cfg config.Config,
	runner *agent.LoopRunner,
	st store.Store,
	inv plugin.Inventory,
	taskExec task.Executor,
	jobsReg *jobs.Registry,
	srv *server.Server,
	statusStore *installer.StatusStore,
	authBackend console.AuthBackend,
	scheduleStore *schedule.Store,
	whDispatcher *webhook.Dispatcher,
) *console.ConsoleServer {
	var sessions console.SessionBackend
	if runner != nil && st != nil {
		sessions = &console.SessionsAdapter{Store: st, Runner: runner}
	}
	var plugins console.PluginBackend
	if inv != nil {
		rawEntries, _ := inv.List(context.Background())
		entries := make([]console.InventoryEntry, len(rawEntries))
		for i, e := range rawEntries {
			tools := make([]string, len(e.Tools))
			for j, t := range e.Tools {
				tools[j] = t.Name
			}
			entries[i] = console.InventoryEntry{
				Name:    e.Name,
				Kind:    e.Kind,
				Source:  e.Source,
				Version: e.Version,
				Healthy: e.Healthy,
				Tools:   tools,
			}
		}
		plugins = &console.PluginsAdapter{
			Inventory: console.NewStaticPluginInventory(entries),
			Statuses:  statusStore,
		}
	}
	var models console.ModelBackend
	if runner != nil && runner.Client != nil {
		models = &singleChannelModelAdapter{client: runner.Client, channel: cfg.LLM.Model}
	}
	var tasks console.TaskBackend
	if taskExec != nil {
		tasks = &console.TasksAdapter{Executor: taskExec}
	}
	var jobsB console.JobsBackend
	if jobsReg != nil {
		jobsB = &console.JobsAdapter{Registry: jobsReg}
	}
	queue := console.NewApprovalQueue()
	approvals := &console.ApprovalsAdapter{Queue: queue}
	var events console.EventStream
	if srv != nil {
		events = &console.EventsAdapter{Router: srv.Router()}
	}
	statePath := filepath.Join(cfg.Agent.WorkspaceRoot, "console_state.json")
	stateStore, err := console.NewStateStore(statePath)
	if err != nil {
		log.Printf("[dsh] console: state store unavailable: %v (continuing in-memory only)", err)
		stateStore, _ = console.NewStateStore("")
	}
	return console.New(console.Config{AuthToken: cfg.Server.AuthToken}, console.Deps{
		Sessions:     sessions,
		Plugins:      plugins,
		Models:       models,
		Tasks:        tasks,
		Jobs:         jobsB,
		Approvals:    approvals,
		Events:       events,
		ConsoleState: stateStore,
		Auth:         authBackend,
		Schedules:    console.NewScheduleAdapter(scheduleStore),
		Webhooks:     console.NewWebhookAdapter(whDispatcher),
	})
}

// loadOrCreateJWTSecret 加载或创建 JWT 签名密钥。密钥持久化在
// <workspace>/jwt.key 文件（hex 编码 32 字节）；启动时若文件不存在
// 则随机生成并写入。返回 hex 字符串。
func loadOrCreateJWTSecret(workspace string) (string, error) {
	path := filepath.Join(workspace, "jwt.key")
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 32 {
			return s, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		return "", err
	}
	log.Printf("[dsh] auth: created new JWT signing key at %s", path)
	return s, nil
}

// bootstrapRootUser 创建首个 root 账号（v8.1）。
//
// 行为：
//   - 如果 users 表已存在 ≥1 行 → 不做任何事（用户已配置）；
//   - 否则按环境变量创建 root 账号：
//     * DSH_ADMIN_USER（默认 "root"）
//     * DSH_ADMIN_PASSWORD（默认：随机生成 16 字节 hex 并打印到 stderr）
func bootstrapRootUser(st *auth.Store, user, pass string) {
	if user == "" {
		user = "root"
	}
	n, err := st.Count()
	if err != nil {
		log.Printf("[dsh] auth: bootstrap: count: %v", err)
		return
	}
	if n > 0 {
		return
	}
	if pass == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		pass = hex.EncodeToString(b)
		log.Printf("[dsh] auth: bootstrap user %q with random password: %s", user, pass)
		log.Printf("[dsh] auth: >> save this password — set DSH_ADMIN_PASSWORD before next boot to override <<")
	}
	h, err := auth.HashPassword(pass)
	if err != nil {
		log.Printf("[dsh] auth: bootstrap: hash: %v", err)
		return
	}
	if _, err := st.Create(auth.User{Username: user, PasswordHash: h, Role: "admin"}); err != nil {
		log.Printf("[dsh] auth: bootstrap: create: %v", err)
		return
	}
	log.Printf("[dsh] auth: bootstrap: created admin user %q", user)
}

// singleChannelModelAdapter 是 v8.0 单渠道 ModelBackend 适配器。
type singleChannelModelAdapter struct {
	client  llm.Client
	channel string
}

func (a *singleChannelModelAdapter) List(_ context.Context) ([]console.ModelItem, error) {
	return []console.ModelItem{
		{Channel: a.channel, Model: a.channel, Protocol: "default", Active: true},
	}, nil
}
func (a *singleChannelModelAdapter) Update(_ context.Context, _ string, _ console.ModelItem) error {
	return nil
}
func (a *singleChannelModelAdapter) Create(_ context.Context, _ console.ModelItem) error {
	return console.ErrModelExists
}
func (a *singleChannelModelAdapter) Remove(_ context.Context, channel string) error {
	return console.ErrModelNotFound
}
func (a *singleChannelModelAdapter) Ping(ctx context.Context, channel string) (console.PingResult, error) {
	start := time.Now()
	resp, err := a.client.Chat(ctx, llm.ChatRequest{
		Model:    channel,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "ping"}},
		MaxTokens: 1,
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return console.PingResult{OK: false, LatencyMs: latency, Error: err.Error()}, nil
	}
	sample := ""
	if len(resp.Choices) > 0 {
		sample = resp.Choices[0].Message.Content
	}
	return console.PingResult{OK: true, LatencyMs: latency, Sample: sample}, nil
}
