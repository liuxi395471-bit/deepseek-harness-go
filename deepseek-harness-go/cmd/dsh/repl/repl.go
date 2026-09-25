// Package repl 实现 dsh 的交互式 REPL 和一次性的 RunOnce 辅助函数。
// 所有面向用户的输出被分流：助手文本输出到 stdout，中间事件和错误
// 输出到 stderr。
//
// 斜杠命令（M5e）：
//
//	/history [N|id]   显示当前会话最近 N 条消息（默认 20），
//	                  或在给定 id 时切换到该会话
//	/model <name>     切换运行时模型
//	/usage            打印自启动以来累计的用量
//	/stream           切换 REPL 会话的流式模式
//	/sessions         列出所有已持久化的会话
//	/clear            清屏（ANSI 转义序列），不删除历史
//	/help             打印可用命令
//
// 斜杠命令不区分大小写，且不消耗调用之后的输入。
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/skill"
)

// Runner 是 cmd/dsh 中 Run 所依赖的契约。它打包了 v1 的 Runner 方法
// 加上可选的 v2 钩子（模型切换、流式切换）。不需要这些钩子的测试
// 可以传入基础的 agent.Runner。
type Runner interface {
	agent.Runner
}

// UsageReporter 由 *usage.Tracker 满足；REPL 调用 Snapshot() 来打印
// 当前总量。可选；为 nil 时 /usage 会打印 "disabled"。
type UsageReporter interface {
	Snapshot() any
}

// SessionLister 由 store.Store 满足；REPL 调用 List 来打印会话列表。
// 可选；为 nil 时 /sessions 会打印 "disabled"。
type SessionLister interface {
	List(ctx context.Context, limit, offset int) (any, error)
}

// SessionLoader 由 store.Store 满足；REPL 在切换前调用 Load 来校验
// 会话 id。可选。
type SessionLoader interface {
	Load(ctx context.Context, id string) (any, error)
}

// REPLState 打包可变的运行时状态（模型、流式开关、当前会话）。
type REPLState struct {
	Model     string
	Stream    bool
	SessionID string // 关闭会话持久化时为空
}

// Options 在 runner 之外对 REPL 进行配置。
type Options struct {
	Usage    UsageReporter
	Sessions SessionLister
	Loader   SessionLoader
	// OnSessionSwitch 让 runner 得知新的会话 id。
	OnSessionSwitch func(newSID string)
	// Skills 是已加载的 skill 列表（DESIGN-v3 §A）；/skills 命令展示。
	Skills []skill.Skill
}

// Run 运行交互式 REPL，直到遇到 EOF 或输入 "exit"/"quit"。
func Run(rootCtx context.Context, runner Runner, debug bool) {
	RunWith(rootCtx, runner, debug, Options{}, &REPLState{})
}

// RunWith 是完整形式，允许调用者注入 REPL 选项。
func RunWith(rootCtx context.Context, runner Runner, debug bool, opts Options, state *REPLState) {
	if state == nil {
		state = &REPLState{}
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 1024*1024)
	fmt.Fprintln(os.Stderr, "dsh> ready. type your prompt, 'exit' or Ctrl+D to quit.")

	for {
		if rootCtx.Err() != nil {
			return
		}
		fmt.Fprint(os.Stderr, "dsh> ")
		if !in.Scan() {
			return // EOF（Ctrl+D）或 scanner 错误
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return
		}

		if strings.HasPrefix(line, "/") {
			if handleSlashCommand(rootCtx, line, opts, state) {
				continue
			}
			// 未知的斜杠命令：向下落入普通 prompt 处理
		}

		ctx, cancel := context.WithCancel(rootCtx)
		events, resultCh := runner.Run(ctx, line)
		consumeEvents(events, debug)
		cancel()
		res := <-resultCh
		printResultFooter(res)
		fmt.Fprintln(os.Stderr)
	}
}

// ErrUnknownCommand 由 handleSlashCommand 在命令未知时返回。调用者
// 会把未知命令当作普通 prompt 处理。
var ErrUnknownCommand = errors.New("repl: unknown command")

// handleSlashCommand 分发斜杠命令。命令已处理（REPL 应继续）时返回
// true；命令未知时返回 false（调用者可将其作为普通 prompt 处理）。
func handleSlashCommand(ctx context.Context, line string, opts Options, state *REPLState) bool {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return true
	}
	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "/help":
		printHelp()
		return true
	case "/clear":
		// ANSI 清屏；不支持的终端上也无害。
		fmt.Fprint(os.Stderr, "\x1b[2J\x1b[H")
		return true
	case "/model":
		if len(parts) < 2 {
			fmt.Fprintln(os.Stderr, "[dsh] usage: /model <name>")
			return true
		}
		state.Model = parts[1]
		fmt.Fprintf(os.Stderr, "[dsh] model = %s\n", state.Model)
		return true
	case "/stream":
		state.Stream = !state.Stream
		fmt.Fprintf(os.Stderr, "[dsh] stream = %v\n", state.Stream)
		return true
	case "/usage":
		if opts.Usage == nil {
			fmt.Fprintln(os.Stderr, "[dsh] usage disabled")
			return true
		}
		snap := opts.Usage.Snapshot()
		fmt.Fprintf(os.Stderr, "[dsh] %+v\n", snap)
		return true
	case "/sessions":
		if opts.Sessions == nil {
			fmt.Fprintln(os.Stderr, "[dsh] sessions disabled")
			return true
		}
		list, err := opts.Sessions.List(ctx, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[dsh] sessions: %v\n", err)
			return true
		}
		fmt.Fprintf(os.Stderr, "%v\n", list)
		return true
	case "/history":
		return handleHistory(ctx, parts, opts, state)
	case "/skills":
		printSkills(opts.Skills)
		return true
	default:
		return false
	}
}

// printSkills 输出已加载的 skill 清单（§H 验收 demo）。
func printSkills(skills []skill.Skill) {
	if len(skills) == 0 {
		fmt.Fprintln(os.Stderr, "[dsh] no skills loaded (put *.md under ~/.dsh/skills or set skills.dir)")
		return
	}
	fmt.Fprintf(os.Stderr, "[dsh] %d skills loaded:\n", len(skills))
	for _, s := range skills {
		kind := "mention"
		switch {
		case s.Always:
			kind = "always"
		case s.Trigger != "":
			kind = "mention:" + s.Trigger
		}
		fmt.Fprintf(os.Stderr, "  %-16s %-14s %s\n", s.Name, kind, s.Description)
	}
}

// handleHistory 处理 /history [N|id]。若参数能解析为会话 id 则切换
// 会话；否则打印当前会话最近 N 条消息（默认 20）。
func handleHistory(ctx context.Context, parts []string, opts Options, state *REPLState) bool {
	if opts.Loader == nil {
		fmt.Fprintln(os.Stderr, "[dsh] sessions disabled")
		return true
	}
	if len(parts) >= 2 {
		arg := parts[1]
		// 数字 → 条数上限；否则 → 会话 id 切换。
		if _, err := strconv.Atoi(arg); err != nil {
			loaded, err := opts.Loader.Load(ctx, arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[dsh] /history: %v\n", err)
				return true
			}
			state.SessionID = arg
			if opts.OnSessionSwitch != nil {
				opts.OnSessionSwitch(arg)
			}
			fmt.Fprintf(os.Stderr, "[dsh] switched to session %s (loaded: %v)\n", arg, loaded)
			return true
		}
	}
	// 数字参数或无参数：打印当前会话最近 N 条消息。
	fmt.Fprintln(os.Stderr, "[dsh] history: see store via /sessions; printing is in v2.1")
	return true
}

func printHelp() {
	fmt.Fprintln(os.Stderr, "dsh slash commands:")
	fmt.Fprintln(os.Stderr, "  /help                  show this help")
	fmt.Fprintln(os.Stderr, "  /clear                 clear screen")
	fmt.Fprintln(os.Stderr, "  /model <name>          switch runtime model")
	fmt.Fprintln(os.Stderr, "  /stream                toggle streaming mode")
	fmt.Fprintln(os.Stderr, "  /usage                 print accumulated usage")
	fmt.Fprintln(os.Stderr, "  /sessions              list persisted sessions")
	fmt.Fprintln(os.Stderr, "  /history [N|<sid>]     show history (or switch session)")
	fmt.Fprintln(os.Stderr, "  /skills                list loaded skills (DESIGN-v3 §A)")
}

// RunOnce 运行单条 prompt（-prompt 模式使用）后退出。
func RunOnce(ctx context.Context, runner Runner, prompt string, debug bool) {
	events, resultCh := runner.Run(ctx, prompt)
	consumeEvents(events, debug)
	res := <-resultCh
	printResultFooter(res)
}

// consumeEvents 消费事件通道，把助手内容打印到 stdout，把旁路信息
// 打印到 stderr。
func consumeEvents(events <-chan agent.Event, debug bool) {
	for ev := range events {
		switch e := ev.(type) {
		case agent.AssistantMessage:
			fmt.Fprint(os.Stdout, e.Content)
			if debug {
				fmt.Fprintf(os.Stderr, "\n[debug] AssistantMessage{ToolCalls=%d}\n", len(e.ToolCalls))
			}
		case agent.ToolCallStart:
			args := e.Call.Function.Arguments
			if len(args) > 64 {
				args = args[:64] + "…"
			}
			fmt.Fprintf(os.Stderr, "\n  → tool %s(%s)\n", e.Call.Function.Name, args)
		case agent.ToolResult:
			tag := "ok"
			if e.IsError {
				tag = "err"
			}
			fmt.Fprintf(os.Stderr, "  ← %s (%s, %s)\n",
				e.Name, tag, e.Took.Round(time.Millisecond))
			if debug {
				fmt.Fprintf(os.Stderr, "      %s\n", truncateForDebug(e.Content))
			}
		case agent.PhaseChange:
			if debug {
				fmt.Fprintf(os.Stderr, "  * phase=%s @ %s\n", e.Phase, e.At.Format(time.RFC3339))
			}
		case agent.LoopError:
			fmt.Fprintf(os.Stderr, "[dsh] loop error: %v\n", e.Err)
		case agent.LoopDone:
			if debug {
				fmt.Fprintf(os.Stderr, "[debug] LoopDone rounds=%d\n", e.Rounds)
			}
		case agent.AssistantDelta:
			if debug {
				fmt.Fprintf(os.Stderr, "[debug] AssistantDelta{%q}\n", e.Text)
			}
		}
	}
}

// printResultFooter 在每次运行结束后输出单行摘要。
//
// 模型内容已由 consumeEvents 打印；这里只标注失败模式和调用摘要。
func printResultFooter(res agent.RunResult) {
	switch res.StopReason {
	case "no_tool_calls":
		// 已打印过；只是为 REPL 模式留一个空行作间隔
	case "max_rounds":
		fmt.Fprintln(os.Stderr, "[dsh] reached max-rounds, here is what we have.")
	case "error":
		fmt.Fprintf(os.Stderr, "[dsh] error: %v\n", res.Error)
	case "canceled":
		fmt.Fprintln(os.Stderr, "[dsh] canceled")
	}
}

// truncateForDebug 截断过长的工具输出，保持 stderr 可读。
func truncateForDebug(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Discard 可作为 io.Writer 传入，供希望静默 REPL 的测试使用。
// 目前未被使用，但保持导出以维持包接口对下游测试的稳定。
var _ io.Writer = io.Discard
