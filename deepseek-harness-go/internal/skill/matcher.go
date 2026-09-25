package skill

import "strings"

// KeywordMatcher 按 mention 关键词 + always-on 匹配 skill（§A.2）。
type KeywordMatcher struct{}

// Matcher 是触发匹配契约（§A.3）。KeywordMatcher 为默认实现。
type Matcher interface {
	Match(skills []Skill, userPrompt string) []Skill
}

// compile-time: KeywordMatcher 满足 Matcher。
var _ Matcher = KeywordMatcher{}

// NewMatcher 构造默认 matcher。
func NewMatcher() KeywordMatcher { return KeywordMatcher{} }

// Match 返回应注入本轮 system prompt 的 skill：
//   - Always == true 的 skill 每轮都命中；
//   - Trigger 非空且作为子串出现在 userPrompt 中（大小写不敏感）的 skill 命中。
//
// 返回顺序与 skills 输入顺序一致；结果可能包含同一个 skill 一次
// （即使 always 且同时 mention 命中也不去重两次）。
func (KeywordMatcher) Match(skills []Skill, userPrompt string) []Skill {
	if len(skills) == 0 {
		return nil
	}
	lower := strings.ToLower(userPrompt)
	var out []Skill
	for _, s := range skills {
		if s.Always {
			out = append(out, s)
			continue
		}
		if s.Trigger != "" && strings.Contains(lower, strings.ToLower(s.Trigger)) {
			out = append(out, s)
		}
	}
	return out
}

// Inject 把命中的 skill body 拼接到 system prompt 之后（§A.4）。
// 命中列表为空时原样返回 system。
func Inject(system string, matched []Skill) string {
	if len(matched) == 0 {
		return system
	}
	var b strings.Builder
	b.WriteString(system)
	for _, s := range matched {
		if strings.TrimSpace(s.Body) == "" {
			continue
		}
		b.WriteString("\n\n")
		b.WriteString("<skill name=\"")
		b.WriteString(s.Name)
		b.WriteString("\">\n")
		b.WriteString(s.Body)
		b.WriteString("\n</skill>")
	}
	return b.String()
}
