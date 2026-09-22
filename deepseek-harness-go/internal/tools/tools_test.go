package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"deepseek-harness-go/internal/tool"
)

func TestGreet_Basic(t *testing.T) {
	g := NewGreet()
	res, err := g.Execute(context.Background(), json.RawMessage(`{"name":"Ada"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false")
	}
	if !strings.Contains(res.Content, "Ada") {
		t.Errorf("Content = %q, missing name", res.Content)
	}
}

func TestGreet_MissingName(t *testing.T) {
	g := NewGreet()
	res, _ := g.Execute(context.Background(), json.RawMessage(`{}`))
	if !res.IsError {
		t.Errorf("expected IsError=true for missing name")
	}
	if !strings.Contains(res.Content, "required") {
		t.Errorf("Content = %q", res.Content)
	}
}

func TestGreet_InvalidArgs(t *testing.T) {
	g := NewGreet()
	res, _ := g.Execute(context.Background(), json.RawMessage(`not-json`))
	if !res.IsError {
		t.Errorf("expected IsError=true for invalid args")
	}
}

func TestFsRead_Normal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("你好，dsh！"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFsRead(dir)
	res, err := r.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true: %s", res.Content)
	}
	if res.Content != "你好，dsh！" {
		t.Errorf("Content = %q", res.Content)
	}
}

func TestFsRead_NotFound(t *testing.T) {
	dir := t.TempDir()
	r := NewFsRead(dir)
	res, _ := r.Execute(context.Background(), json.RawMessage(`{"path":"missing.txt"}`))
	if !res.IsError {
		t.Errorf("expected IsError=true")
	}
	if !strings.Contains(res.Content, "not found") {
		t.Errorf("Content = %q", res.Content)
	}
}

func TestFsRead_PathEscapesWorkspace(t *testing.T) {
	dir := t.TempDir()
	r := NewFsRead(dir)
	res, _ := r.Execute(context.Background(), json.RawMessage(`{"path":"../etc/passwd"}`))
	if !res.IsError {
		t.Errorf("expected IsError=true for ../")
	}
	if !strings.Contains(res.Content, "escapes") {
		t.Errorf("Content = %q", res.Content)
	}
}

func TestFsRead_AbsolutePathOutside(t *testing.T) {
	dir := t.TempDir()
	r := NewFsRead(dir)
	res, _ := r.Execute(context.Background(), json.RawMessage(`{"path":"C:\\Windows\\System32\\drivers\\etc\\hosts"}`))
	if !res.IsError {
		t.Errorf("expected IsError=true for absolute path outside")
	}
}

func TestFsWrite_Normal(t *testing.T) {
	dir := t.TempDir()
	w := NewFsWrite(dir)
	res, err := w.Execute(context.Background(), json.RawMessage(`{"path":"sub/note.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError=true: %s", res.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestFsWrite_PathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	w := NewFsWrite(dir)
	res, _ := w.Execute(context.Background(), json.RawMessage(`{"path":"../escape.txt","content":"x"}`))
	if !res.IsError {
		t.Errorf("expected IsError=true for ../")
	}
	// Confirm the file was NOT created outside.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Errorf("escape file was created outside workspace")
	}
}

func TestFsWrite_AbsolutePathOutsideRejected(t *testing.T) {
	dir := t.TempDir()
	w := NewFsWrite(dir)
	outside := filepath.Join(filepath.Dir(dir), "dsh-outside-test.txt")
	defer os.Remove(outside)
	res, _ := w.Execute(context.Background(), json.RawMessage(`{"path":"`+escapeJSON(outside)+`","content":"x"}`))
	if !res.IsError {
		t.Errorf("expected IsError=true for absolute path outside")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("outside file was created")
	}
}

func TestFsWrite_MkdirAllCreatesParents(t *testing.T) {
	dir := t.TempDir()
	// Remove it to simulate "workspace does not exist yet".
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	w := NewFsWrite(dir)
	res, _ := w.Execute(context.Background(), json.RawMessage(`{"path":"a/b/c/note.txt","content":"x"}`))
	if res.IsError {
		t.Errorf("IsError=true: %s (MkdirAll should have created workspace)", res.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c", "note.txt")); err != nil {
		t.Errorf("expected file at a/b/c/note.txt: %v", err)
	}
}

func TestFsWrite_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewFsWrite(dir)
	res, _ := w.Execute(context.Background(), json.RawMessage(`{"path":"note.txt","content":"new"}`))
	if res.IsError {
		t.Fatalf("IsError=true: %s", res.Content)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("after overwrite, content = %q", string(data))
	}
}

func TestMustRegisterBuiltin(t *testing.T) {
	r := tool.NewRegistry()
	MustRegisterBuiltin(r, t.TempDir())
	want := []string{"greet", "fs_read", "fs_write"}
	got := r.Names()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

// escapeJSON encodes s as a JSON string literal so it can be embedded in
// a json.RawMessage payload safely.
func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
