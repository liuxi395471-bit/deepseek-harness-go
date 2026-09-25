package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileLoader 从目录加载 *.md skill 文件。
type FileLoader struct{}

// NewFileLoader 构造默认 loader。
func NewFileLoader() *FileLoader { return &FileLoader{} }

// Load 读取 dir 下所有 *.md 文件并解析 frontmatter。
//
// 行为约定（§A.5）：
//   - 目录不存在 → 返回空列表，不报错
//   - frontmatter 缺 name 等必需字段 → 报错（不静默）
//   - Body = frontmatter 的 body 字段（若有）+ 文件 frontmatter 之后的正文
func (FileLoader) Load(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill: read dir %s: %w", dir, err)
	}
	var out []Skill
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// LoadFile 解析单个 skill markdown 文件。
func LoadFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("skill: read %s: %w", path, err)
	}
	s, err := Parse(string(data))
	if err != nil {
		return Skill{}, fmt.Errorf("skill: %s: %w", path, err)
	}
	if s.Name == "" {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		s.Name = base
	}
	return s, nil
}

// Parse 解析 markdown 文本为 Skill。frontmatter 缺失时整篇文本作为
// Body，name 由调用方（文件名）补齐。
func Parse(text string) (Skill, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	trimmed := strings.TrimSpace(text)
	var fm string
	var body string
	if strings.HasPrefix(trimmed, "---") {
		rest := trimmed[3:]
		// 去掉起始行剩余部分（例如 "---yaml"）。
		if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
			rest = rest[idx+1:]
		} else {
			rest = ""
		}
		end := strings.Index(rest, "\n---")
		if end < 0 {
			return Skill{}, fmt.Errorf("frontmatter not terminated")
		}
		fm = rest[:end]
		after := rest[end+4:] // 跳过 "\n---"
		after = strings.TrimPrefix(after, "\n")
		body = after
	}

	var s Skill
	if fm != "" {
		if err := yaml.Unmarshal([]byte(fm), &s); err != nil {
			return Skill{}, fmt.Errorf("frontmatter: %w", err)
		}
		if s.Trigger == "" && !s.Always && s.Name == "" && s.Description == "" {
			// 完全空的 frontmatter 也算缺字段，避免静默注册无效 skill。
			return Skill{}, fmt.Errorf("frontmatter missing required fields (name/description/trigger/always)")
		}
		if s.Body != "" && body != "" {
			s.Body = s.Body + "\n\n" + body
		} else if body != "" {
			s.Body = body
		}
	} else {
		s.Body = text
	}
	return s, nil
}
