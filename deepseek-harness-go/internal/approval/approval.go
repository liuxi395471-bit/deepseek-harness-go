// Package approval 实现危险工具调用的人工确认门（DESIGN-v2 §C.2）。
//
// v2 提供四种 Approver 实现：
//
//   - NoopApprover:        总是批准；用于测试和可信工具。
//   - AllowListApprover:   当工具名在固定集合中时批准。
//   - TerminalApprover:    在 stderr 上提示用户；从 stdin 读取 y/n/s/q。
//   - HTTPPollApprover:    阻塞直到远程 /api/approval/{id} 给出结论。
//
// Approver 可以组合：运行时传入一条链，第一个返回非 NoOp 决策的
// Approve 生效。
package approval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Decision 是 Approver 返回的裁决结果。
type Decision int

const (
	// ApproveOnce 仅允许本次调用。
	ApproveOnce Decision = iota
	// ApproveSession 允许当前会话中后续所有同名 Tool 的调用。
	ApproveSession
	// Deny 拒绝该调用。
	Deny
)

// String 将 Decision 渲染为便于日志输出的形式。
func (d Decision) String() string {
	switch d {
	case ApproveOnce:
		return "approve-once"
	case ApproveSession:
		return "approve-session"
	case Deny:
		return "deny"
	default:
		return fmt.Sprintf("Decision(%d)", int(d))
	}
}

// Request 是工具向审批者询问的内容。
type Request struct {
	Tool   string          // 被调用的工具名
	Args   json.RawMessage // 原始参数；审批者可检查
	Reason string          // 工具提供的可选人类可读理由
}

// Approver 是接口契约。实现必须返回非零 Decision 或错误，也可以监听
// ctx.Done() 并返回与 context 相关的错误。
type Approver interface {
	Approve(ctx context.Context, req Request) (Decision, error)
}

// --- NoopApprover ---

// NoopApprover 批准一切。用于关闭审批或测试中嫌门碍事的场景。
type NoopApprover struct{}

// Approve 无条件返回 ApproveOnce。
func (NoopApprover) Approve(_ context.Context, _ Request) (Decision, error) {
	return ApproveOnce, nil
}

// --- AllowListApprover ---

// AllowListApprover 批准名称在给定集合中的工具，并同时对这
// 些名称授予会话级批准。
type AllowListApprover struct {
	allowed map[string]struct{}
}

// NewAllowListApprover 根据工具名列表构造 AllowListApprover。
// 空字符串和纯空白条目会被忽略。
func NewAllowListApprover(names []string) *AllowListApprover {
	m := make(map[string]struct{})
	for _, n := range names {
		if s := strings.TrimSpace(n); s != "" {
			m[s] = struct{}{}
		}
	}
	return &AllowListApprover{allowed: m}
}

// AllowListDenyAll 返回一个拒绝所有工具的 Approver。
func AllowListDenyAll() Approver {
	return NewAllowListApprover(nil)
}

// Approve 当工具在列表中时返回 ApproveSession，否则返回 Deny。
func (a *AllowListApprover) Approve(_ context.Context, req Request) (Decision, error) {
	if _, ok := a.allowed[req.Tool]; ok {
		return ApproveSession, nil
	}
	return Deny, fmt.Errorf("approval: tool %q not in allowlist", req.Tool)
}

// --- TerminalApprover ---

// TerminalApprover 在 Prompter（默认 os.Stderr）上提示用户，并从
// Reader（默认 os.Stdin）读取一行。用户可以回答：
//
//	y / yes / 回车  → ApproveOnce
//	s / session     → ApproveSession
//	n / no / deny   → Deny
//
// 遇到 EOF 或读取错误时决策为 Deny（故障安全）。
type TerminalApprover struct {
	Prompt string // 每次提示前显示的横幅
	In     io.Reader
	Out    io.Writer
}

// NewTerminalApprover 返回连接到 stdin/stderr 的 TerminalApprover。
func NewTerminalApprover() *TerminalApprover {
	return &TerminalApprover{}
}

// Approve 进行提示并读取输入。
func (t *TerminalApprover) Approve(ctx context.Context, req Request) (Decision, error) {
	prompt := t.Prompt
	if prompt == "" {
		prompt = "[dsh-approval]"
	}
	out := t.Out
	if out == nil {
		out = defaultOut{}
	}
	fmt.Fprintf(out, "%s tool=%q args=%s reason=%q\n", prompt, req.Tool, string(req.Args), req.Reason)
	fmt.Fprintf(out, "%s allow once (y), session (s), deny (n)? ", prompt)

	in := t.In
	if in == nil {
		in = defaultIn{}
	}
	// bufio.Scanner 处理部分读取，并允许我们在 EOF 之后检查 Err()。
	// 我们在 goroutine 中运行它，以便响应 ctx 的取消。
	type result struct {
		line string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		if scanner.Scan() {
			resCh <- result{line: strings.ToLower(strings.TrimSpace(scanner.Text())), err: nil}
			return
		}
		// 没有读到内容（EOF 或读取错误）。视为 Deny —— 但仅当 ctx
		// 尚未取消时。由于 goroutine 可能与 ctx 竞争，我们只上报
		// 错误，让 select 决定哪个先触发。
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		resCh <- result{err: err}
	}()
	select {
	case <-ctx.Done():
		return Deny, ctx.Err()
	case r := <-resCh:
		// EOF 或读取错误 → 故障安全地拒绝。
		if r.err != nil {
			return Deny, r.err
		}
		switch r.line {
		case "y", "yes", "":
			return ApproveOnce, nil
		case "s", "session":
			return ApproveSession, nil
		default:
			return Deny, nil
		}
	}
}

// defaultIn / defaultOut 使单元测试无需显式接线 IO 即可创建
// TerminalApprover。生产调用方应设置 In/Out。
type defaultIn struct{}

func (defaultIn) Read(p []byte) (int, error) { return 0, io.EOF }

type defaultOut struct{}

func (defaultOut) Write(p []byte) (int, error) { return len(p), nil }

// --- HTTPPollApprover ---

// Resolver 是 HTTPPollApprover 用于查询远程决策的契约。生产环境
// 接入 HTTP 调用；测试提供假实现。
type Resolver interface {
	Resolve(ctx context.Context, id string) (Decision, error)
}

// HTTPPollApprover 阻塞直到 resolver 返回裁决。调用方传入 id
// （通常是会话 id + 工具名），resolver 负责到远程 sidecar 上查询。
//
// context 取消时返回 Deny + ctx.Err()。
type HTTPPollApprover struct {
	ID       string
	Resolver Resolver
	PollEvery time.Duration // 默认 1s
	Timeout  time.Duration   // 默认 60s
}

// Approve 轮询直到得到裁决。
func (h *HTTPPollApprover) Approve(ctx context.Context, _ Request) (Decision, error) {
	if h.Resolver == nil {
		return Deny, errors.New("approval: HTTPPollApprover has no resolver")
	}
	poll := h.PollEvery
	if poll <= 0 {
		poll = time.Second
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		dec, err := h.Resolver.Resolve(ctx, h.ID)
		if err == nil {
			return dec, nil
		}
		if !errors.Is(err, errPending) {
			return Deny, err
		}
		select {
		case <-ctx.Done():
			return Deny, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// ErrPending 是"尚未作出决策"的标准哨兵错误，由 Resolver 在远程
// 侧尚未表决时返回。
var errPending = errors.New("approval: pending")

// PendingError 返回一个 HTTPPollApprover 可识别为"尚未决策"的错误。
func PendingError() error { return errPending }

// --- chain ---

// Chain 是一个依次咨询一组 Approver 并返回第一个非 pending 决策的
// Approver。如果全部返回 pending（或错误），则返回 Deny。
type Chain struct {
	approvers []Approver
}

// NewChain 从一组 Approver 构建链式审批者。
func NewChain(approvers ...Approver) *Chain {
	return &Chain{approvers: approvers}
}

// Approve 沿链条依次执行。
func (c *Chain) Approve(ctx context.Context, req Request) (Decision, error) {
	for _, a := range c.approvers {
		dec, err := a.Approve(ctx, req)
		if err == nil {
			return dec, nil
		}
		if errors.Is(err, errPending) {
			continue
		}
		// 该审批者发生硬错误：终止链条。
		return dec, err
	}
	return Deny, errors.New("approval: chain exhausted without decision")
}

// sessionCache 在实例生命周期内缓存 ApproveSession 的裁决。
// 会话内已批准过的工具将跳过审批者。
type sessionCache struct {
	mu    sync.Mutex
	cache map[string]struct{} // 工具名 → 会话内已批准
}

// newSessionCache 返回一个新缓存。
func newSessionCache() *sessionCache {
	return &sessionCache{cache: make(map[string]struct{})}
}

// check 若工具已获得会话级批准则返回 true。
func (s *sessionCache) check(tool string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.cache[tool]
	return ok
}

// mark 将工具加入会话缓存。
func (s *sessionCache) mark(tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[tool] = struct{}{}
}

// CachingApprover 用会话级缓存包装内部 Approver，缓存 ApproveSession
// 的裁决。对某个工具的首次 Approve 调用会咨询内部审批者；之后返回
// ApproveSession 的调用立即返回。
type CachingApprover struct {
	Inner Approver
	Cache *sessionCache
}

// NewCachingApprover 用新的会话缓存包装 inner。
func NewCachingApprover(inner Approver) *CachingApprover {
	return &CachingApprover{Inner: inner, Cache: newSessionCache()}
}

// Approve 在工具未被缓存时委托给 Inner。
func (c *CachingApprover) Approve(ctx context.Context, req Request) (Decision, error) {
	if c.Cache.check(req.Tool) {
		return ApproveSession, nil
	}
	dec, err := c.Inner.Approve(ctx, req)
	if err == nil && dec == ApproveSession {
		c.Cache.mark(req.Tool)
	}
	return dec, err
}
