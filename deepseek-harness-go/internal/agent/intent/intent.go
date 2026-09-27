// Package intent 提供 9 类意图分类（v7 P7-8）。
//
// 9 类（端口自 dsh-java 的 IntentRegex）：
//
//  1. CODE_GENERATION    生成 / 写代码
//  2. CODE_FIX           修 bug
//  3. CODE_REFACTOR      重构
//  4. CODE_EXPLAIN       解释代码
//  5. FILE_READ          读文件
//  6. FILE_WRITE         写文件
//  7. SHELL_EXEC         执行 shell
//  8. SEARCH             搜索 / 查找
//  9. CHAT               普通对话
//
// Classifier 是顺序匹配（first hit wins）；CHAT 为兜底。
package intent

import "regexp"

// Intent 是 9 类意图的枚举。
type Intent string

const (
	CodeGeneration Intent = "code_generation"
	CodeFix        Intent = "code_fix"
	CodeRefactor   Intent = "code_refactor"
	CodeExplain    Intent = "code_explain"
	FileRead       Intent = "file_read"
	FileWrite      Intent = "file_write"
	ShellExec      Intent = "shell_exec"
	Search         Intent = "search"
	Chat           Intent = "chat"
)

// Pattern 是 (意图, 正则) 二元组。
type Pattern struct {
	Intent Intent
	Regex  *regexp.Regexp
}

// Classifier 持有顺序正则列表。
type Classifier struct {
	patterns []Pattern
}

// DefaultClassifier 返回内置 9 类分类器。
func DefaultClassifier() *Classifier {
	return &Classifier{patterns: defaultPatterns()}
}

// Classify 返回首个匹配的 Intent；无匹配 → Chat。
func (c *Classifier) Classify(input string) Intent {
	for _, p := range c.patterns {
		if p.Regex.MatchString(input) {
			return p.Intent
		}
	}
	return Chat
}

// All 返回所有意图字符串（用于提示 / 状态报告）。
func All() []Intent {
	return []Intent{
		CodeGeneration, CodeFix, CodeRefactor, CodeExplain,
		FileRead, FileWrite, ShellExec, Search, Chat,
	}
}

func defaultPatterns() []Pattern {
	// 注：Go regexp 不支持 lookahead；用字符类替代。
	pairs := []struct {
		Intent Intent
		Regex  string
	}{
		{CodeFix, `(?i)(fix|debug|repair|patch)\b.{0,20}(bug|error|exception|crash)`},
		{CodeRefactor, `(?i)(refactor|restructure|reorganize|optimize).{0,30}code`},
		{CodeGeneration, `(?i)(write|generate|create|implement|add|build)\b.{0,30}(function|method|class|module|file|code|script|program|test|case)`},
		{CodeExplain, `(?i)(explain|describe|what does|how does|meaning of)\b.{0,40}(code|function|method|class|line|block)`},
		{ShellExec, `(?i)(run|execute|exec|invoke|launch|start)\b.{0,30}(command|shell|script|process|binary|cmd|powershell|bash)`},
		{FileWrite, `(?i)(write|save|create|overwrite|update)\b.{0,30}(file|path|\.go|\.py|\.js|\.ts|\.md|\.json)`},
		{FileRead, `(?i)(read|open|cat|view|show|display)\b.{0,30}(file|path|\.go|\.py|\.js|\.ts|\.md|\.json)`},
		{Search, `(?i)(search|find|grep|locate|lookup|query)\b.{0,40}(in|for|through|across)`},
	}
	out := make([]Pattern, 0, len(pairs)+1)
	for _, p := range pairs {
		out = append(out, Pattern{Intent: p.Intent, Regex: regexp.MustCompile(p.Regex)})
	}
	return out
}
