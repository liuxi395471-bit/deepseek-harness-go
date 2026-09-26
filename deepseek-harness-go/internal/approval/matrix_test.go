package approval

import (
	"context"
	"errors"
	"testing"
)

// Policy / ParsePolicy 正反。
func TestPolicyParseRoundtrip(t *testing.T) {
	for _, p := range []Policy{PolicyAsk, PolicyAuto, PolicyDeny} {
		s := p.String()
		if s == "" {
			t.Fatalf("String 返回空: %v", p)
		}
		got, err := ParsePolicy(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if got != p {
			t.Fatalf("round-trip %v → %q → %v", p, s, got)
		}
	}
}

// 空字符串默认 PolicyAsk。
func TestParsePolicy_Default(t *testing.T) {
	p, err := ParsePolicy("")
	if err != nil || p != PolicyAsk {
		t.Fatalf("空串应得 PolicyAsk, got %v, err=%v", p, err)
	}
}

// 未知字符串返回错误。
func TestParsePolicy_Unknown(t *testing.T) {
	_, err := ParsePolicy("garbage")
	if err == nil {
		t.Fatalf("未知 policy 应报错")
	}
}

// Matrix 命中 allow。
func TestMatrix_GetPolicy_AllowListed(t *testing.T) {
	m := Matrix{
		Default: PolicyAsk,
		Profiles: []Profile{
			{
				Name: "dev",
				Rules: []Rule{
					{Tool: "shell", Policy: PolicyAuto},
					{Tool: "fs_write", Policy: PolicyAsk},
				},
			},
		},
	}
	if got := m.GetPolicy("dev", "shell"); got != PolicyAuto {
		t.Fatalf("dev/shell = %v, want PolicyAuto", got)
	}
	if got := m.GetPolicy("dev", "fs_write"); got != PolicyAsk {
		t.Fatalf("dev/fs_write = %v, want PolicyAsk", got)
	}
}

// Matrix 未命中工具回退到 Profile Default。
func TestMatrix_GetPolicy_FallsBackToProfileDefault(t *testing.T) {
	m := Matrix{
		Default: PolicyAsk,
		Profiles: []Profile{
			{Name: "dev", Rules: []Rule{{Tool: "shell", Policy: PolicyAuto}}},
		},
	}
	if got := m.GetPolicy("dev", "unknown-tool"); got != PolicyAsk {
		t.Fatalf("dev/unknown-tool = %v, want PolicyAsk (profile default)", got)
	}
}

// Matrix 未知 profile 回退到 Matrix.Default。
func TestMatrix_GetPolicy_ProfileNotFound(t *testing.T) {
	m := Matrix{
		Default: PolicyDeny,
		Profiles: []Profile{
			{Name: "dev", Rules: []Rule{{Tool: "shell", Policy: PolicyAuto}}},
		},
	}
	if got := m.GetPolicy("prod", "shell"); got != PolicyDeny {
		t.Fatalf("prod/shell = %v, want PolicyDeny (matrix default)", got)
	}
}

// MatrixApprover：PolicyAuto。
func TestMatrixApprover_Auto(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{{Name: "dev", Rules: []Rule{{Tool: "shell", Policy: PolicyAuto}}}},
	}
	a := NewMatrixApprover(m, "dev")
	dec, err := a.Approve(context.Background(), Request{Tool: "shell"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dec != ApproveSession {
		t.Fatalf("dec = %v, want ApproveSession", dec)
	}
}

// MatrixApprover：PolicyDeny 返回 Deny + 错误。
func TestMatrixApprover_Deny(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{{Name: "prod", Rules: []Rule{{Tool: "shell", Policy: PolicyDeny}}}},
	}
	a := NewMatrixApprover(m, "prod")
	dec, err := a.Approve(context.Background(), Request{Tool: "shell"})
	if dec != Deny {
		t.Fatalf("dec = %v, want Deny", dec)
	}
	if err == nil {
		t.Fatalf("Deny 必须返回非 nil err")
	}
}

// MatrixApprover：PolicyAsk → 透传 ApproveOnce，把决策留给下一层。
func TestMatrixApprover_Ask(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{{Name: "dev", Rules: []Rule{{Tool: "fs_write", Policy: PolicyAsk}}}},
	}
	a := NewMatrixApprover(m, "dev")
	dec, err := a.Approve(context.Background(), Request{Tool: "fs_write"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dec != ApproveOnce {
		t.Fatalf("dec = %v, want ApproveOnce", dec)
	}
}

// MatrixApprover 跨 profile 隔离。
func TestMatrixApprover_ProfileIsolation(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{
			{Name: "dev", Rules: []Rule{{Tool: "shell", Policy: PolicyAuto}}},
			{Name: "prod", Rules: []Rule{{Tool: "shell", Policy: PolicyDeny}}},
		},
	}
	devA := NewMatrixApprover(m, "dev")
	prodA := NewMatrixApprover(m, "prod")

	if dec, _ := devA.Approve(context.Background(), Request{Tool: "shell"}); dec != ApproveSession {
		t.Fatalf("dev shell 应 auto, got %v", dec)
	}
	if dec, _ := prodA.Approve(context.Background(), Request{Tool: "shell"}); dec != Deny {
		t.Fatalf("prod shell 应 deny, got %v", dec)
	}
}

// PolicyAsk 通过 errors.Is 链也可以分辨。
func TestMatrixApprover_DenyError(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{{Name: "prod", Rules: []Rule{{Tool: "shell", Policy: PolicyDeny}}}},
	}
	a := NewMatrixApprover(m, "prod")
	_, err := a.Approve(context.Background(), Request{Tool: "shell"})
	if !errors.Is(err, err) { // sanity check
		t.Fatalf("应是 error")
	}
}

// Chain 串联 Matrix + Noop：Matrix 的 PolicyAsk 经 chain 落到 Noop。
func TestChain_MatrixThenNoop(t *testing.T) {
	m := Matrix{
		Profiles: []Profile{{Name: "dev", Rules: []Rule{{Tool: "fs_write", Policy: PolicyAsk}}}},
	}
	mApprover := NewMatrixApprover(m, "dev")
	noop := NoopApprover{}

	c := NewChain(mApprover, noop)
	dec, err := c.Approve(context.Background(), Request{Tool: "fs_write"})
	if err != nil {
		t.Fatalf("chain err: %v", err)
	}
	if dec != ApproveOnce { // 来自 Noop
		t.Fatalf("dec = %v, want ApproveOnce (chain 走 noop)", dec)
	}
}
