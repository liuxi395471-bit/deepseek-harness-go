package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"deepseek-harness-go/internal/audit"
	"deepseek-harness-go/internal/compaction"
	"deepseek-harness-go/internal/hook"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/obs"
	"deepseek-harness-go/internal/skill"
	"deepseek-harness-go/internal/store"
	"deepseek-harness-go/internal/tool"
	usagemeter "deepseek-harness-go/internal/usage"
)

// RunResult 是一次 Run 调用结束时的最终状态。
//
// StopReason 取值：
//
//	"no_tool_calls" — 助手产出了最终的助手消息
//	"max_rounds"    — 超出 AgentConfig.MaxRounds
//	"error"         — Error 已设置；运行器中止
//	"canceled"      — context 被取消
//
// 当 Store 为 nil 或未启用会话持久化时，SessionID 为空；
// 否则它保存本次运行所用的会话 id，以便调用方后续 resume。
type RunResult struct {
	FinalMessages []llm.Message
	Rounds        int
	StopReason    string
	Error         error
	SessionID     string    // v2：当 Store==nil 时为空；启用会话后会被赋值
	Usage         llm.Usage // v2：本次运行所有轮次的聚合 usage
}

// Runner 是 cmd/dsh 依赖的契约。
type Runner interface {
	Run(ctx context.Context, prompt string) (<-chan Event, <-chan RunResult)
}

// StreamingRunner 是 v2 流式契约。仅实现 Runner 的 v1 消费者
// 仍可继续编译；LoopRunner 同时满足两者。
//
// RunStream 与 Run 发出同样的事件词汇，但额外在模型生成过程中
// 按文本 delta 发出 AssistantDelta。最终的 AssistantMessage（非流式摘要）
// 仍然在该轮末尾发出，因此只识别 AssistantMessage 的现有订阅者
// 仍可继续工作。
//
// sid 语义（DESIGN-v2 §A.3.4）：
//
//   - sid == "" 且 Store == nil：不持久化；v1 旧路径。
//   - sid == "" 且 Store != nil：开新会话；分配 sid。
//   - sid != ""：从 Store.Load(sid) 恢复。
type StreamingRunner interface {
	RunStream(ctx context.Context, prompt string, sid string) (<-chan Event, <-chan RunResult)
}

// LoopRunner 是默认的 Runner 实现。
//
// 初版不变量：
//
//   - 每次 Run 调用只处理一条用户 prompt，且不带任何历史。
//     会话记忆 / 压缩将在 M7 落地。
//   - 运行器保证所有事件都先于 RunResult 被投递，即使发生 panic 或 ctx 取消。
//   - 工具执行期间的错误会变成 Result.IsError=true 并回填给模型；
//     它们不会中止循环。
//
// 流式开关：
//
//   - Stream=false（默认）时，Run() 使用 Client.Chat。这与 v1.2 行为逐字节一致，
//     现有测试依赖于此。
//   - Stream=true 时，Run() 和 RunStream() 都使用 Client.ChatStream，
//     并在该模型轮次中发出 AssistantDelta。
//   - RunStream() 始终使用 ChatStream，不受 Stream 开关影响。
type LoopRunner struct {
	Client   llm.Client
	Registry *tool.Registry
	System   SystemPromptBuilder

	Model       string
	Temperature *float64
	MaxTokens   int
	MaxRounds   int
	Stream      bool

	Store store.Store // v2: optional. When nil, no persistence.

	// ---- v3 可选接入点（零值 = 关闭，行为与 v2 一致）----

	// Skills 是候选 skill 集合（§A.4）。每轮用户 prompt 到达时经
	// Matcher 匹配，命中的 body 注入 system prompt。
	Skills []skill.Skill
	// Matcher 决定触发逻辑；nil 时用 skill.KeywordMatcher。
	Matcher skill.Matcher
	// Compactor 非 nil 时在每轮 LLM 调用前检查上下文长度（§D）。
	Compactor *compaction.Compactor
	// Audit 非 nil 时记录 tool_call / llm_call 事件（§E）。
	Audit audit.Logger
	// Obs 注入可观测实现（§C）；零值 = 全 no-op。
	Obs obs.Provider
	// Meter 是 v5 P5-2 引入的 token 计量域；零值 = NoopMeter。
	Meter usagemeter.Meter
	// Hooks 是 v5 P5-6 引入的工具执行钩子；nil = NoopRegistry。
	Hooks *hook.Registry

	// baseSystem 在 run() 启动时被快照 System.Build(Registry) 的结果，
	// 用于每次重注入 skill 前重置 msgs[0].Content，保证 skill body
	// 不会因反复 inject 而累积（§A.4：loop 每轮都应重新评估）。
	baseSystem string

	bufSize int
}

// NewLoopRunner 用合理的默认值构造 LoopRunner。
// stream=false 保持 v1.2 行为；传 stream=true 即可在该轮开启流式，
// 或者直接调用 RunStream。
func NewLoopRunner(c llm.Client, reg *tool.Registry, sys SystemPromptBuilder, model string, maxRounds, maxTokens int, temp *float64) *LoopRunner {
	if maxRounds <= 0 {
		maxRounds = 16
	}
	return &LoopRunner{
		Client:      c,
		Registry:    reg,
		System:      sys,
		Model:       model,
		MaxTokens:   maxTokens,
		MaxRounds:   maxRounds,
		Temperature: temp,
		bufSize:     32,
	}
}

// Run 在 goroutine 中启动循环，并返回事件通道与最终结果通道。
// 两个通道都由运行器负责关闭。
//
// 旧版 2 参数形式不会启用会话持久化；如需启用，请向 RunStream 传入
// 非空的 sid（DESIGN-v2 §A.3.4）。
func (r *LoopRunner) Run(ctx context.Context, prompt string) (<-chan Event, <-chan RunResult) {
	out := make(chan Event, r.bufSize)
	res := make(chan RunResult, 1)
	go r.loop(ctx, prompt, out, res)
	return out, res
}

// loop 拥有生命周期：它始终先关闭 out，再发送 RunResult，最后关闭 res ——
// 即使发生 panic 也如此。调用方（REPL）以 for-range 消费事件后
// 再读取 RunResult；这保证了不会死锁的明确定义顺序。
func (r *LoopRunner) loop(ctx context.Context, prompt string, out chan<- Event, res chan<- RunResult) {
	r.run(ctx, prompt, "", out, res, r.Stream)
}

// run 是 Run（stream=r.Stream，sid=""）和 RunStream（stream=true，
// sid 由调用方提供）共用的执行体。
//
// 当 sid=="" 且 Store!=nil 时，通过 Store.Begin 创建新会话，并把
// 得到的 id 记录到 RunResult.SessionID。当 sid!="" 时，加载该会话，
// 并从 session.Messages 恢复循环（传入的 prompt 会被忽略 ——
// 由历史消息驱动循环）。
//
// 每轮的持久化副作用：
//
//   - 助手消息 → Store.Append
//   - 工具结果消息 → Store.Append（执行之后）
//   - 本轮 usage → Store.UpdateUsage
//
// stream 标志控制本轮是使用 Client.Chat（false）还是 Client.ChatStream（true）。
// 当启用流式时，AssistantDelta 事件会随文本 delta 实时发出，
// 然后在该轮末尾发出标准的 AssistantMessage。
func (r *LoopRunner) run(ctx context.Context, prompt string, sid string, out chan<- Event, res chan<- RunResult, stream bool) {
	stopReason := "error"
	var stopErr error
	var finalMsgs []llm.Message
	rounds := 0
	var totalUsage llm.Usage

	// v3 §C.3：整个 run 包在 "agent.run" span 中。
	runCtx, runSpan := r.Obs.T().Start(ctx, "agent.run")
	runSpan.SetAttr("agent.model", r.Model)
	runSpan.SetAttr("agent.stream", stream)
	ctx = runCtx
	defer func() {
		runSpan.SetAttr("agent.rounds", rounds)
		runSpan.SetAttr("agent.stop_reason", stopReason)
		if stopErr != nil {
			runSpan.RecordError(stopErr)
		}
		runSpan.End()
	}()

	// 外层 defer：捕获循环体执行过程中的任何 panic，将其归类为
	// 致命错误，然后进入下方的关闭序列。
	defer func() {
		if x := recover(); x != nil {
			stopReason = "error"
			stopErr = fmt.Errorf("runner panic: %v", x)
		}

		// 内层 defer：保护关闭流程本身。如果在向 res 写入时发生 panic
		// （例如有 bug 的调用方已经关闭了 res），运行器 goroutine
		// 会静默退出，而不是把整个进程拖崩。
		defer func() {
			_ = recover()
		}()

		close(out)
		res <- RunResult{
			FinalMessages: finalMsgs,
			Rounds:        rounds,
			StopReason:    stopReason,
			Error:         stopErr,
			SessionID:     sid,
			Usage:         totalUsage,
		}
		close(res)
	}()

	// 解析会话 + 初始消息列表。
	var msgs []llm.Message
	if r.Store != nil && sid != "" {
		// 恢复模式：加载已有消息。
		sess, err := r.Store.Load(ctx, sid)
		if err != nil {
			stopReason = "error"
			stopErr = fmt.Errorf("runner: load session: %w", err)
			return
		}
		msgs = append(msgs, sess.Messages...)
	} else if r.Store != nil && sid == "" {
		// 新建会话。
		sess, err := r.Store.Begin(ctx)
		if err != nil {
			stopReason = "error"
			stopErr = fmt.Errorf("runner: begin session: %w", err)
			return
		}
		sid = sess.ID
		msgs = []llm.Message{
			{Role: llm.RoleSystem, Content: r.System.Build(r.Registry)},
		}
		// 把 system 消息也持久化，保证 store 里始终有完整会话。
		if err := r.Store.Append(ctx, sid, msgs[0]); err != nil {
			stopReason = "error"
			stopErr = fmt.Errorf("runner: append system: %w", err)
			return
		}
	} else {
		msgs = []llm.Message{
			{Role: llm.RoleSystem, Content: r.System.Build(r.Registry)},
		}
	}

	// 记录 baseSystem：每轮 skill 注入前会用此值重置 msgs[0].Content，
	// 防止前一轮注入的 body 残留与新一轮叠加（§A.4）。
	// 恢复模式下 store 里的 system 与当前 Registry 推导的 base 不一定
	// 一致（工具注册表可能变化），用 base 覆盖保证语义统一。
	r.baseSystem = r.System.Build(r.Registry)
	if len(msgs) > 0 && msgs[0].Role == llm.RoleSystem {
		msgs[0].Content = r.baseSystem
	}

	// 追加用户 prompt，除非是恢复模式（恢复时没有新 prompt ——
	// 会话由其他途径延续；服务端通常会在调用 RunStream 前通过 Store.Append
	// 注入一条用户消息）。
	if prompt != "" {
		userMsg := llm.Message{Role: llm.RoleUser, Content: prompt}
		msgs = append(msgs, userMsg)
		// v3 §A.4：skill 注入。命中的 body 拼接到 system prompt 尾部。
		// 只改内存副本，不回写 Store——注入是运行时行为。
		r.injectSkills(msgs, prompt)
		if r.Store != nil && sid != "" {
			if err := r.Store.Append(ctx, sid, userMsg); err != nil {
				stopReason = "error"
				stopErr = fmt.Errorf("runner: append user msg: %w", err)
				return
			}
		}
	}

	// 如果没有注入任何用户消息（恢复但无 prompt 的边界场景），直接退出。
	if len(msgs) == 1 && msgs[0].Role == llm.RoleSystem {
		stopReason = "no_tool_calls"
		finalMsgs = msgs
		out <- LoopDone{Rounds: rounds, Messages: msgs}
		return
	}

	finalMsgs = msgs

	// 至少发一次 init，方便调试订阅者观察到循环已进入。
	select {
	case <-ctx.Done():
		stopReason = "canceled"
		return
	case out <- PhaseChange{Phase: PhaseInit, At: time.Now()}:
	}

	for {
		select {
		case <-ctx.Done():
			stopReason = "canceled"
			finalMsgs = msgs
			return
		case out <- PhaseChange{Phase: PhaseLLMCall, At: time.Now()}:
		}

		// v3 §D：每轮 LLM 调用前检查上下文长度；超阈值则压缩。
		// 压缩结果只影响发送给 LLM 的消息序列，Store 不动。
		if r.Compactor != nil {
			compacted, did, cerr := r.Compactor.Maybe(ctx, msgs)
			if cerr != nil {
				r.Obs.L().Warn(ctx, "compaction failed", obs.A("err", cerr.Error()))
			}
			if did {
				out <- Compacted{Before: len(msgs), After: len(compacted)}
				r.Obs.L().Info(ctx, "context compacted",
					obs.A("before", len(msgs)), obs.A("after", len(compacted)))
				msgs = compacted
			}
		}

		// v3 §A.4：每轮按当前 user prompt 重新评估 skill 注入。
		// injectSkills 内部会用 r.baseSystem 重置 msgs[0]，避免累积。
		if lastPrompt := lastUserContent(msgs); lastPrompt != "" {
			r.injectSkills(msgs, lastPrompt)
		}

		assistant, usage, done, err := r.doOneRound(ctx, msgs, out, stream)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				stopReason = "canceled"
				return
			}
			stopReason = "error"
			stopErr = err
			out <- LoopError{Err: err, Phase: PhaseLLMCall}
			return
		}
		if done {
			stopReason = "error"
			stopErr = errors.New("llm: empty choices")
			out <- LoopError{Err: stopErr, Phase: PhaseLLMCall}
			return
		}
		rounds++

		msgs = append(msgs, assistant)
		out <- AssistantMessage{Content: assistant.Content, ToolCalls: assistant.ToolCalls}
		r.persistAppend(ctx, sid, assistant)
		totalUsage.PromptTokens += usage.PromptTokens
		totalUsage.CompletionTokens += usage.CompletionTokens
		totalUsage.TotalTokens += usage.TotalTokens
		// v3 §E：llm_call 审计 + §C.3 span 属性。
		r.auditLog(ctx, audit.Event{
			SessionID:        sid,
			Event:            audit.EventLLMCall,
			Model:            r.Model,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
		})
		// v5 P5-2: 通知 Meter 累计本轮 usage。
		if r.Meter != nil {
			r.Meter.Account(sid, usagemeter.AccountRecord{
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				Model:            r.Model,
				At:               time.Now(),
			})
		}
		if r.Store != nil && sid != "" {
			_ = r.Store.UpdateUsage(ctx, sid, usage)
		}

		if len(assistant.ToolCalls) == 0 {
			stopReason = "no_tool_calls"
			finalMsgs = msgs
			out <- LoopDone{Rounds: rounds, Messages: msgs}
			return
		}

		// 按顺序执行每个工具调用；即使其中一个失败，循环也继续
		// （失败会被回填给模型）。
		for _, tc := range assistant.ToolCalls {
			select {
			case <-ctx.Done():
				stopReason = "canceled"
				finalMsgs = msgs
				return
			case out <- PhaseChange{Phase: PhaseToolExec, At: time.Now()}:
			}

			t, ok := r.Registry.Get(tc.Function.Name)
			if !ok {
				content := "[ERROR] unknown tool: " + tc.Function.Name
				toolMsg := llm.Message{
					Role: llm.RoleTool, Content: content, ToolCallID: tc.ID,
				}
				msgs = append(msgs, toolMsg)
				r.persistAppend(ctx, sid, toolMsg)
				out <- ToolResult{
					CallID: tc.ID, Name: tc.Function.Name,
					Content: content, IsError: true, Took: 0,
				}
				continue
			}

			out <- ToolCallStart{Call: tc}

			// 把空 / null 的参数归一为 {}，让工具可以放心 Unmarshal。
			// 其他情况原样透传；如果工具内部 Unmarshal 失败，工具
			// 会返回 Result{Err}，我们把它上报而不让进程崩溃。
			raw := json.RawMessage(tc.Function.Arguments)
			if len(raw) == 0 || string(raw) == "null" {
				raw = json.RawMessage("{}")
			}

			// v5 P5-6: PRE_TOOL_USE 钩子。可改写 raw / 拦截（返回 err）。
			if r.Hooks != nil {
				preReq := &hook.PreRequest{Tool: tc.Function.Name, Args: &raw}
				if hookErr := r.Hooks.Pre(ctx, preReq); hookErr != nil {
					r.Obs.L().Warn(ctx, "pre_tool_use hook denied", obs.A("err", hookErr.Error()))
					content := "[ERROR] " + hookErr.Error()
					toolMsg := llm.Message{Role: llm.RoleTool, Content: content, ToolCallID: tc.ID}
					msgs = append(msgs, toolMsg)
					r.persistAppend(ctx, sid, toolMsg)
					out <- ToolResult{
						CallID: tc.ID, Name: tc.Function.Name,
						Content: content, IsError: true, Took: 0,
					}
					continue
				}
			}

			start := time.Now()
			toolCtx, toolSpan := r.Obs.T().Start(ctx, "tool.execute")
			toolSpan.SetAttr("tool.name", tc.Function.Name)
			// v3 §E：tool_call 审计。FileLogger 按 redact 配置决定
			// 保留 hash 还是原文。
			r.auditLog(ctx, audit.Event{
				SessionID: sid,
				Event:     audit.EventToolCall,
				Round:     rounds,
				Tool:      tc.Function.Name,
				ArgsHash:  audit.HashArgs(raw),
				ArgsRaw:   string(raw),
			})
			result, execErr := safeExecute(t, toolCtx, raw)
			if execErr != nil {
				// safeExecute 只在工具 panic 时返回非 nil；
				// 我们附加 [ERROR] 前缀，确保语义明确。
				result = tool.Err(execErr.Error())
				toolSpan.RecordError(execErr)
			}
			if result.IsError {
				toolSpan.RecordError(errors.New(result.Content))
			}
			toolSpan.End()

			// v5 P5-6: POST_TOOL_USE 钩子（不改 raw，只改 result）。
			if r.Hooks != nil {
				postReq := &hook.PostRequest{Tool: tc.Function.Name, Args: raw, Result: result}
				_ = r.Hooks.Post(ctx, postReq)
				result = postReq.Result
			}

			took := time.Since(start)

			if result.IsError && !strings.HasPrefix(result.Content, "[ERROR] ") {
				result.Content = "[ERROR] " + result.Content
			}
			toolMsg := llm.Message{
				Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID,
			}
			msgs = append(msgs, toolMsg)
			r.persistAppend(ctx, sid, toolMsg)
			out <- ToolResult{
				CallID:  tc.ID,
				Name:    tc.Function.Name,
				Content: result.Content,
				IsError: result.IsError,
				Took:    took,
			}
		}

		// 预算检查：如果已经达到最大轮数，用 PhaseStopped 退出，
		// 让调试订阅者能看到退出原因。
		if rounds >= r.MaxRounds {
			out <- PhaseChange{Phase: PhaseStopped, At: time.Now()}
			stopReason = "max_rounds"
			finalMsgs = msgs
			return
		}
	}
}

// doOneRound 执行一次模型调用：流式关闭时走 Chat（stream=false），
// 流式开启时走 ChatStream（stream=true）。流式时，每来一个文本 delta
// 就发一次 AssistantDelta。返回的 assistant Message 包含最终累积的
// content 与 tool_calls。done=true 表示服务端返回为空
// （调用方应将其归类为致命错误）。
// 第二个返回值是本轮模型上报的 Usage（后端未上报时为零值）。
//
// v3 §C.3：整轮调用包在 "llm.chat" span 中，结束前记录 model 与
// token 属性。
//
// 并发性：必须与 run() 在同一 goroutine 中执行（它会写 out）。
func (r *LoopRunner) doOneRound(ctx context.Context, msgs []llm.Message, out chan<- Event, stream bool) (assistant llm.Message, usage llm.Usage, done bool, err error) {
	spanCtx, span := r.Obs.T().Start(ctx, "llm.chat")
	span.SetAttr("llm.model", r.Model)
	span.SetAttr("llm.messages", len(msgs))
	defer func() {
		span.SetAttr("llm.prompt_tokens", usage.PromptTokens)
		span.SetAttr("llm.completion_tokens", usage.CompletionTokens)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()
	ctx = spanCtx
	req := llm.ChatRequest{
		Model:       r.Model,
		Messages:    msgs,
		Tools:       r.Registry.Specs(),
		Temperature: r.Temperature,
		MaxTokens:   r.MaxTokens,
		Stream:      stream,
	}
	if !stream {
		resp, cerr := r.Client.Chat(ctx, req)
		if cerr != nil {
			return llm.Message{}, llm.Usage{}, false, cerr
		}
		var u llm.Usage
		if resp.Usage != nil {
			u = *resp.Usage
		}
		if len(resp.Choices) == 0 {
			return llm.Message{}, u, true, nil
		}
		return resp.Choices[0].Message, u, false, nil
	}
	// streaming path
	ch, errCh := r.Client.ChatStream(ctx, req)
	// 在后台排空 errCh，避免阻塞 ChatStream 内部运行的 goroutine。
	// 运行器把 errCh 上的任何值都视为致命错误。
	var streamErr error
	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		if e, ok := <-errCh; ok {
			streamErr = e
		}
	}()
	var (
		text      strings.Builder
		toolCalls []llm.ToolCall
		finish    string
		seenAny   bool
	)
	for chunk := range ch {
		seenAny = true
		if chunk.Text != "" {
			text.WriteString(chunk.Text)
			select {
			case <-ctx.Done():
				return llm.Message{}, llm.Usage{}, false, context.Canceled
			case out <- AssistantDelta{Text: chunk.Text}:
			}
		}
		for _, tc := range chunk.ToolCalls {
			toolCalls = append(toolCalls, tc)
		}
		if chunk.Finish != "" {
			finish = chunk.Finish
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
	}
	<-errDone
	if streamErr != nil {
		return llm.Message{}, llm.Usage{}, false, streamErr
	}
	if !seenAny {
		return llm.Message{}, usage, true, nil
	}
	assistantMsg := llm.Message{
		Role:      llm.RoleAssistant,
		Content:   text.String(),
		ToolCalls: toolCalls,
	}
	if finish != "" {
		// 把 finish 暂存在消息上；当前结构没有专门字段，因此下游循环代码
		// 只在 len(ToolCalls)==0 时才发出 LoopDone。对轮次计数而言，
		// "finish" 仅作信息用途；循环路径不变。
		_ = finish
	}
	return assistantMsg, usage, false, nil
}

// persistAppend 在 Store != nil 且 sid != "" 时把 msg 写入 Store。
// 持久化失败会被静默丢弃：内存中的循环是真值源；store 只是供 resume
// 使用的直写缓存。
func (r *LoopRunner) persistAppend(ctx context.Context, sid string, msg llm.Message) {
	if r.Store == nil || sid == "" {
		return
	}
	_ = r.Store.Append(ctx, sid, msg)
}

// RunStream 是 Run 的流式对应方法。它始终使用 Client.ChatStream，
// 不受 Stream 开关影响，并在模型生成过程中发出 AssistantDelta。
//
// sid 语义（DESIGN-v2 §A.3.4）：
//
//   - sid == "" 且 Store == nil：v1 旧路径；不持久化。
//   - sid == "" 且 Store != nil：开新会话；分配 sid；持久化用户消息（prompt）
//     以及后续所有轮次。
//   - sid != ""：从 Store.Load(sid) 恢复；传入的 prompt 会被作为新一轮用户消息
//     追加（即"继续对话"路径）。
func (r *LoopRunner) RunStream(ctx context.Context, prompt string, sid string) (<-chan Event, <-chan RunResult) {
	out := make(chan Event, r.bufSize)
	res := make(chan RunResult, 1)
	go r.run(ctx, prompt, sid, out, res, true)
	return out, res
}

// safeExecute 调用 t.Execute 时加了 panic-recovery 兜底，
// 防止行为不端的工具把整个循环搞崩。返回值 err 仅在 panic 时非 nil；
// 普通的"参数解析失败"由工具自身以 Result{IsError:true} 返回，
// 不会走到这个兜底。
func safeExecute(t tool.Tool, ctx context.Context, args json.RawMessage) (result tool.Result, err error) {
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("panic in tool %s: %v", t.Name(), x)
		}
	}()
	return t.Execute(ctx, args)
}

// injectSkills 按 §A.4 把命中的 skill body 注入 msgs[0]（system）。
// msgs 为空或首条不是 system 时静默跳过。
//
// 行为约定：每次注入前先把 msgs[0].Content 重置为 r.baseSystem
// （run() 启动时快照），避免前一轮注入的 skill body 残留在 system 里
// 与新一轮命中叠加。这样保证 loop 每轮按当前 user prompt 重新评估
// 触发，命中的 skill body 永远反映"最近一轮"的语义。
func (r *LoopRunner) injectSkills(msgs []llm.Message, prompt string) {
	if len(r.Skills) == 0 || len(msgs) == 0 || msgs[0].Role != llm.RoleSystem {
		return
	}
	// 还原 base（防止多轮累积）。
	msgs[0].Content = r.baseSystem
	matcher := r.Matcher
	if matcher == nil {
		matcher = skill.KeywordMatcher{}
	}
	matched := matcher.Match(r.Skills, prompt)
	if len(matched) == 0 {
		return
	}
	msgs[0].Content = skill.Inject(msgs[0].Content, matched)
}

// auditLog 在 Audit 非 nil 时写入事件；失败被忽略（审计不应拖垮
// 主流程）。Audit 为 nil 时是零开销 no-op。
func (r *LoopRunner) auditLog(ctx context.Context, ev audit.Event) {
	if r.Audit == nil {
		return
	}
	r.Audit.Log(ctx, ev)
}

// lastUserContent 返回 msgs 中最后一条 role=user 的 Content。
// 用于每轮 skill 注入时拿到"当前用户意图"作为触发依据。
// msgs 为空或无 user 消息时返回 ""（调用方应跳过注入）。
func lastUserContent(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
