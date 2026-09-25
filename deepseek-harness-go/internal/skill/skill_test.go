package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFullFrontmatter(t *testing.T) {
	text := "---\nname: code-review\ndescription: review 时注入\ntrigger: review\n---\n\n## 代码风格\n- 函数 < 50 行\n"
	s, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Name != "code-review" || s.Description != "review 时注入" || s.Trigger != "review" || s.Always {
		t.Fatalf("unexpected skill: %+v", s)
	}
	if !strings.Contains(s.Body, "函数 < 50 行") {
		t.Fatalf("body missing: %q", s.Body)
	}
}

func TestParseBodyFieldAndMarkdown(t *testing.T) {
	text := "---\nname: a\nbody: |\n  ## frontmatter body\n---\n\n# markdown body\n"
	s, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(s.Body, "frontmatter body") || !strings.Contains(s.Body, "markdown body") {
		t.Fatalf("body should contain both parts: %q", s.Body)
	}
}

func TestParseUnterminatedFrontmatter(t *testing.T) {
	if _, err := Parse("---\nname: a\nno end"); err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
}

func TestParseEmptyFrontmatter(t *testing.T) {
	// 完全空的 frontmatter 应报错而不是静默。
	if _, err := Parse("---\n---\nbody"); err == nil {
		t.Fatal("expected error for empty frontmatter")
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	s, err := Parse("# just markdown\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Body != "# just markdown\n" {
		t.Fatalf("body = %q", s.Body)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "review.md")
	f2 := filepath.Join(dir, "always.md")
	os.WriteFile(f1, []byte("---\nname: review\ndescription: d\ntrigger: review\n---\nreview body"), 0o644)
	os.WriteFile(f2, []byte("---\nname: always-on\ndescription: d\nalways: true\n---\nalways body"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := NewFileLoader().Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 skills, got %d: %+v", len(got), got)
	}
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if s := byName["review"]; s.Trigger != "review" || !strings.Contains(s.Body, "review body") {
		t.Fatalf("review skill wrong: %+v", s)
	}
	if s := byName["always-on"]; !s.Always {
		t.Fatalf("always-on skill wrong: %+v", s)
	}
}

func TestLoadMissingDirReturnsEmpty(t *testing.T) {
	got, err := NewFileLoader().Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestLoadBrokenFrontmatterFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("---\n:::not yaml:::\n---\nbody"), 0o644)
	if _, err := NewFileLoader().Load(dir); err == nil {
		t.Fatal("expected error for invalid frontmatter")
	}
}

func TestLoadFileDefaultsNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "my-skill.md")
	os.WriteFile(p, []byte("---\ndescription: d\ntrigger: x\n---\nbody"), 0o644)
	s, err := LoadFile(p)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if s.Name != "my-skill" {
		t.Fatalf("name = %q", s.Name)
	}
}

func TestMatcherMentionAndAlways(t *testing.T) {
	m := NewMatcher()
	review := Skill{Name: "review", Trigger: "REVIEW", Body: "b1"}
	always := Skill{Name: "always", Always: true, Body: "b2"}
	other := Skill{Name: "other", Trigger: "deploy", Body: "b3"}

	got := m.Match([]Skill{review, always, other}, "please review my code")
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %+v", got)
	}
	if got[0].Name != "review" || got[1].Name != "always" {
		t.Fatalf("order/content wrong: %+v", got)
	}

	// 大小写不敏感 + 无匹配。
	if got := m.Match([]Skill{review}, "REVIEW it"); len(got) != 1 {
		t.Fatalf("case-insensitive match failed: %+v", got)
	}
	if got := m.Match([]Skill{review}, "nothing here"); len(got) != 0 {
		t.Fatalf("unexpected match: %+v", got)
	}
	// always 即使 prompt 为空也命中。
	if got := m.Match([]Skill{always}, ""); len(got) != 1 {
		t.Fatalf("always should match empty prompt: %+v", got)
	}
}

func TestInject(t *testing.T) {
	sys := "base system"
	out := Inject(sys, nil)
	if out != sys {
		t.Fatalf("Inject(nil) changed system: %q", out)
	}
	out = Inject(sys, []Skill{{Name: "s1", Body: "body one"}})
	if !strings.HasPrefix(out, sys) || !strings.Contains(out, "body one") || !strings.Contains(out, `name="s1"`) {
		t.Fatalf("Inject result wrong: %q", out)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Skill{Name: ""}); err == nil {
		t.Fatal("empty name should error")
	}
	if err := r.Register(Skill{Name: "a"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_ = r.Register(Skill{Name: "a", Description: "updated"})
	if s, _ := r.Resolve("a"); s.Description != "updated" {
		t.Fatalf("overwrite failed: %+v", s)
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d", r.Len())
	}
	_ = r.Register(Skill{Name: "b"})
	names := r.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("Names = %v", names)
	}
	if len(r.List()) != 2 {
		t.Fatalf("List = %+v", r.List())
	}
	if _, ok := r.Resolve("zzz"); ok {
		t.Fatal("Resolve missing should be false")
	}
}
