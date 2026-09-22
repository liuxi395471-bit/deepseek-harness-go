package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNoopApprover_Approves(t *testing.T) {
	dec, err := NoopApprover{}.Approve(context.Background(), Request{Tool: "greet"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != ApproveOnce {
		t.Errorf("dec = %v, want ApproveOnce", dec)
	}
}

func TestAllowListApprover_AllowedAndDenied(t *testing.T) {
	a := NewAllowListApprover([]string{"greet", "fs_read"})
	dec, err := a.Approve(context.Background(), Request{Tool: "greet"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != ApproveSession {
		t.Errorf("greet dec = %v, want ApproveSession", dec)
	}
	_, err = a.Approve(context.Background(), Request{Tool: "shell"})
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("shell err = %v, want allowlist error", err)
	}
}

func TestTerminalApprover_ApproveOnce(t *testing.T) {
	tr := &TerminalApprover{In: strings.NewReader("y\n"), Out: &bytes.Buffer{}}
	dec, err := tr.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != ApproveOnce {
		t.Errorf("dec = %v", dec)
	}
}

func TestTerminalApprover_Session(t *testing.T) {
	tr := &TerminalApprover{In: strings.NewReader("session\n"), Out: &bytes.Buffer{}}
	dec, _ := tr.Approve(context.Background(), Request{Tool: "shell"})
	if dec != ApproveSession {
		t.Errorf("dec = %v", dec)
	}
}

func TestTerminalApprover_Deny(t *testing.T) {
	tr := &TerminalApprover{In: strings.NewReader("no\n"), Out: &bytes.Buffer{}}
	dec, err := tr.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != Deny {
		t.Errorf("dec = %v", dec)
	}
}

func TestTerminalApprover_EmptyDefaultsToYes(t *testing.T) {
	tr := &TerminalApprover{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	dec, _ := tr.Approve(context.Background(), Request{Tool: "shell"})
	if dec != ApproveOnce {
		t.Errorf("dec = %v", dec)
	}
}

func TestTerminalApprover_EOFDefaultsToDeny(t *testing.T) {
	tr := &TerminalApprover{In: bytes.NewReader(nil), Out: &bytes.Buffer{}}
	dec, _ := tr.Approve(context.Background(), Request{Tool: "shell"})
	if dec != Deny {
		t.Errorf("dec = %v", dec)
	}
}

func TestTerminalApprover_CtxCancel(t *testing.T) {
	// 一个阻塞直到 ctx 被取消、然后返回 ctx.Err() 的 reader。
	tr := &TerminalApprover{In: ctxReader{ctx: context.Background()}, Out: &bytes.Buffer{}}
	ctx, cancel := context.WithCancel(context.Background())
	// 将 reader 的 ctx 接好，使其阻塞的 Read 在我们取消时返回。
	tr.In = ctxReader{ctx: ctx}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := tr.Approve(ctx, Request{Tool: "shell"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

type ctxReader struct {
	ctx context.Context
}

func (c ctxReader) Read(p []byte) (int, error) {
	<-c.ctx.Done()
	return 0, c.ctx.Err()
}

// fakeResolver 在前 N 次调用时返回 pending，之后返回固定裁决。
type fakeResolver struct {
	calls   int
	pending int
	final   Decision
}

func (f *fakeResolver) Resolve(_ context.Context, _ string) (Decision, error) {
	f.calls++
	if f.calls <= f.pending {
		return Deny, PendingError()
	}
	return f.final, nil
}

func TestHTTPPollApprover_PollsUntilResolved(t *testing.T) {
	r := &fakeResolver{pending: 3, final: ApproveOnce}
	pa := &HTTPPollApprover{ID: "abc", Resolver: r, PollEvery: 10 * time.Millisecond, Timeout: time.Second}
	dec, err := pa.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != ApproveOnce {
		t.Errorf("dec = %v", dec)
	}
	if r.calls < 4 {
		t.Errorf("calls = %d, want >= 4 (3 pending + 1 final)", r.calls)
	}
}

func TestHTTPPollApprover_Timeout(t *testing.T) {
	r := &fakeResolver{pending: 1000}
	pa := &HTTPPollApprover{ID: "abc", Resolver: r, PollEvery: 10 * time.Millisecond, Timeout: 50 * time.Millisecond}
	_, err := pa.Approve(context.Background(), Request{Tool: "shell"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestHTTPPollApprover_NilResolver(t *testing.T) {
	pa := &HTTPPollApprover{ID: "abc"}
	_, err := pa.Approve(context.Background(), Request{Tool: "x"})
	if err == nil {
		t.Error("nil resolver should error")
	}
}

func TestChain_FirstApproveWins(t *testing.T) {
	c := NewChain(
		NewAllowListApprover([]string{"x"}),
		NoopApprover{},
	)
	dec, err := c.Approve(context.Background(), Request{Tool: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if dec != ApproveSession {
		t.Errorf("dec = %v", dec)
	}
}

func TestChain_AllDeny(t *testing.T) {
	c := NewChain(
		NewAllowListApprover(nil),
	)
	_, err := c.Approve(context.Background(), Request{Tool: "x"})
	if err == nil {
		t.Error("chain should error when all reject")
	}
}

func TestCachingApprover_SessionCaches(t *testing.T) {
	// Inner：仅批准第一次调用（返回 ApproveSession 以写入缓存）。
	count := 0
	inner := ApproverFunc(func(_ context.Context, _ Request) (Decision, error) {
		count++
		if count == 1 {
			return ApproveSession, nil
		}
		return Deny, errors.New("should not be called again")
	})
	c := NewCachingApprover(inner)
	// 第一次调用：缓存未命中 → 调用 inner。
	dec, err := c.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil || dec != ApproveSession {
		t.Fatalf("first call: dec=%v err=%v", dec, err)
	}
	// 第二次调用：缓存命中 → 跳过 inner。
	dec, err = c.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil || dec != ApproveSession {
		t.Fatalf("second call: dec=%v err=%v", dec, err)
	}
	if count != 1 {
		t.Errorf("inner called %d times, want 1", count)
	}
}

// ApproverFunc 是供测试使用的函数适配器。
type ApproverFunc func(ctx context.Context, req Request) (Decision, error)

func (f ApproverFunc) Approve(ctx context.Context, req Request) (Decision, error) {
	return f(ctx, req)
}

// Decision.String 已在上面覆盖。此处确保 JSON 往返正常。
func TestDecisionJSON(t *testing.T) {
	for _, d := range []Decision{ApproveOnce, ApproveSession, Deny} {
		b, _ := json.Marshal(d)
		var back Decision
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back != d {
			t.Errorf("JSON round-trip failed: %v → %v", d, back)
		}
	}
}
