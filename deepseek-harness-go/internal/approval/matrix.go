// Package approval — 审批矩阵（v5 P5-5）。
//
// matrix.go 提供 profile × toolName 二维矩阵：每条规则描述一个
// (profile, toolName) 的最终裁决策略（auto / ask / deny）。
//
// 与 v2 单维 AllowList / Terminal / Chain 相比，矩阵把"每个 profile
// 下哪些工具被自动批准、哪些要被人工拦、哪些被直接拒"集中表达，
// 让 agent_runner 在跨环境（dev / staging / prod）时只切 profile
// 即可生效。
//
// 设计要点：
//   - 数据：Matrix{Default, Profiles[Name, Rules[Tool, Policy]]}；
//   - 策略：PolicyAsk / PolicyAuto / PolicyDeny；
//   - 行为：缺失 profile / tool 时回退 Default；
//   - 接口：MatrixApprover 把 Matrix 包装为 approval.Approver。
package approval

import (
	"context"
	"fmt"
)

// Policy 是单条 (profile, tool) 规则的最终裁决策略。
type Policy int

const (
	// PolicyAsk：必须经过人工 / 终端 / http-poll 等下游审批者。
	PolicyAsk Policy = iota
	// PolicyAuto：自动批准。
	PolicyAuto
	// PolicyDeny：直接拒绝。
	PolicyDeny
)

// String 渲染为 YAML 友好字符串。
func (p Policy) String() string {
	switch p {
	case PolicyAsk:
		return "ask"
	case PolicyAuto:
		return "auto"
	case PolicyDeny:
		return "deny"
	default:
		return fmt.Sprintf("policy(%d)", int(p))
	}
}

// ParsePolicy 反向解析 YAML 字符串。
func ParsePolicy(s string) (Policy, error) {
	switch s {
	case "ask", "":
		return PolicyAsk, nil
	case "auto":
		return PolicyAuto, nil
	case "deny":
		return PolicyDeny, nil
	default:
		return PolicyAsk, fmt.Errorf("approval: unknown policy %q", s)
	}
}

// Rule 是 (profile, tool) → Policy 的单元。
type Rule struct {
	Tool   string `yaml:"tool"   json:"tool"`
	Policy Policy `yaml:"policy" json:"policy"`
}

// Profile 是一组规则；按 Rule 在 Profile.Rules 中的位置（first match wins）匹配。
type Profile struct {
	Name  string `yaml:"name"  json:"name"`
	Rules []Rule `yaml:"rules" json:"rules"`
}

// Matrix 是完整的二维矩阵定义。
type Matrix struct {
	Default  Policy    `yaml:"default"  json:"default"`
	Profiles []Profile `yaml:"profiles" json:"profiles"`
}

// GetPolicy 返回 (profile, tool) 的裁决策略。
//
//   - profile 不存在：返回 Default；
//   - 存在但无匹配 tool：返回 Default；
//   - 第一个匹配 Rule.Tool == toolName 的 Rule.Policy 返回。
func (m *Matrix) GetPolicy(profile, toolName string) Policy {
	if m == nil {
		return PolicyAsk
	}
	for _, p := range m.Profiles {
		if p.Name != profile {
			continue
		}
		for _, r := range p.Rules {
			if r.Tool == toolName {
				return r.Policy
			}
		}
		return m.Default
	}
	return m.Default
}

// MatrixApprover 把 Matrix 包装为 Approver。
//
// 语义：
//   - PolicyAuto：返回 (ApproveSession, nil)；
//   - PolicyDeny：返回 (Deny, nil) + 错误；
//   - PolicyAsk：返回 (ApproveOnce, nil)（向调用方提示"请人工处理"；
//     MatrixApprover 自身不阻塞，让下游 Chain 中的 Terminal/HTTP
//     等真正的交互者继续判定）。
type MatrixApprover struct {
	Matrix  Matrix
	Profile string
}

// NewMatrixApprover 构造。
func NewMatrixApprover(m Matrix, profile string) *MatrixApprover {
	return &MatrixApprover{Matrix: m, Profile: profile}
}

// Approve 是 Approver 接口实现。
func (a *MatrixApprover) Approve(_ context.Context, req Request) (Decision, error) {
	policy := a.Matrix.GetPolicy(a.Profile, req.Tool)
	switch policy {
	case PolicyAuto:
		return ApproveSession, nil
	case PolicyDeny:
		return Deny, fmt.Errorf("approval: matrix deny %s/%s", a.Profile, req.Tool)
	default:
		// PolicyAsk：透传 — 让下一条 Approver 接手（terminal / http）。
		return ApproveOnce, nil
	}
}
