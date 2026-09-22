package repl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
)

// fakeRunner 满足 Runner 接口（agent.Runner）。
type fakeRunner struct {
	calls    int
	lastCtx  context.Context
	lastLine string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) (<-chan agent.Event, <-chan agent.RunResult) {
	f.calls++
	f.lastCtx = ctx
	f.lastLine = prompt
	ev := make(chan agent.Event, 1)
	res := make(chan agent.RunResult, 1)
	ev <- agent.AssistantMessage{Content: "echo:" + prompt}
	close(ev)
	res <- agent.RunResult{
		FinalMessages: []llm.Message{{Role: llm.RoleAssistant, Content: "echo:" + prompt}},
		Rounds:        1,
		StopReason:    "no_tool_calls",
	}
	close(res)
	return ev, res
}

// captureStderr 将 os.Stderr 替换为缓冲区，并在测试结束后恢复。
// 在执行完被测代码后，请调用返回的 flush() 回调，以确保所有已写入
// 的数据都被排空到 buf 中。
func captureStderr(t *testing.T) (buf *bytes.Buffer, flush func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	var bb bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&bb, r)
	}()
	flushed := false
	flush = func() {
		if flushed {
			return
		}
		flushed = true
		// 关闭 writer → io.Copy 看到 EOF → goroutine 退出 → wg.Wait 返回。
		_ = w.Close()
		wg.Wait()
	}
	t.Cleanup(func() {
		if !flushed {
			flush()
		}
		os.Stderr = orig
		_ = r.Close()
	})
	return &bb, flush
}

// 1. /help 打印斜杠命令摘要。
func TestSlashCommand_Help(t *testing.T) {
	buf, flush := captureStderr(t)
	st := &REPLState{}
	handleSlashCommand(context.Background(), "/help", Options{}, st)
	flush()
	out := buf.String()
	if !strings.Contains(out, "/history") || !strings.Contains(out, "/model") {
		t.Errorf("help missing commands: %q", out)
	}
}

// 2. /model 设置 state.Model。
func TestSlashCommand_Model(t *testing.T) {
	buf, flush := captureStderr(t)
	st := &REPLState{}
	if !handleSlashCommand(context.Background(), "/model gpt-x", Options{}, st) {
		t.Errorf("model not handled")
	}
	flush()
	if st.Model != "gpt-x" {
		t.Errorf("Model = %q, want gpt-x", st.Model)
	}
	if !strings.Contains(buf.String(), "gpt-x") {
		t.Errorf("output missing model name: %q", buf.String())
	}
}

// 3. /stream 切换 state.Stream。
func TestSlashCommand_StreamToggle(t *testing.T) {
	buf, flush := captureStderr(t)
	st := &REPLState{}
	handleSlashCommand(context.Background(), "/stream", Options{}, st)
	flush()
	if !st.Stream {
		t.Errorf("Stream = false, want true after toggle")
	}
	if !strings.Contains(buf.String(), "true") {
		t.Errorf("output missing toggle result: %q", buf.String())
	}
	// 第二次切换：变为 false。
	buf2, flush2 := captureStderr(t)
	handleSlashCommand(context.Background(), "/stream", Options{}, st)
	flush2()
	if st.Stream {
		t.Errorf("Stream = true, want false after second toggle")
	}
	_ = buf2
}

// 4. 未配置 UsageReporter 时 /usage 打印 "disabled"。
func TestSlashCommand_Usage_Disabled(t *testing.T) {
	buf, flush := captureStderr(t)
	handleSlashCommand(context.Background(), "/usage", Options{}, &REPLState{})
	flush()
	if !strings.Contains(buf.String(), "disabled") {
		t.Errorf("output = %q, want 'disabled'", buf.String())
	}
}

// 5. 配置了 UsageReporter 时 /usage 打印快照。
func TestSlashCommand_Usage_WithReporter(t *testing.T) {
	buf, flush := captureStderr(t)
	rep := &fakeUsage{prompt: 12, completion: 5}
	handleSlashCommand(context.Background(), "/usage", Options{Usage: rep}, &REPLState{})
	flush()
	if !strings.Contains(buf.String(), "12") || !strings.Contains(buf.String(), "5") {
		t.Errorf("output missing tokens: %q", buf.String())
	}
}

// 6. 没有 lister 时 /sessions → "disabled"。
func TestSlashCommand_Sessions_Disabled(t *testing.T) {
	buf, flush := captureStderr(t)
	handleSlashCommand(context.Background(), "/sessions", Options{}, &REPLState{})
	flush()
	if !strings.Contains(buf.String(), "disabled") {
		t.Errorf("output = %q", buf.String())
	}
}

// 7. /history 传入非数字 id 时切换会话。
func TestSlashCommand_History_SwitchSession(t *testing.T) {
	buf, flush := captureStderr(t)
	loader := &fakeLoader{err: nil, payload: "loaded-payload"}
	var switchedTo string
	opts := Options{
		Loader: loader,
		OnSessionSwitch: func(s string) {
			switchedTo = s
		},
	}
	st := &REPLState{}
	handleSlashCommand(context.Background(), "/history abc123", opts, st)
	flush()
	if st.SessionID != "abc123" {
		t.Errorf("SessionID = %q, want abc123", st.SessionID)
	}
	if switchedTo != "abc123" {
		t.Errorf("OnSessionSwitch not called: %q", switchedTo)
	}
	if !strings.Contains(buf.String(), "abc123") {
		t.Errorf("output missing id: %q", buf.String())
	}
}

// 8. /history 传入未知 id 时打印错误。
func TestSlashCommand_History_UnknownID(t *testing.T) {
	buf, flush := captureStderr(t)
	loader := &fakeLoader{err: errors.New("not found")}
	opts := Options{Loader: loader}
	st := &REPLState{}
	handleSlashCommand(context.Background(), "/history unknown", opts, st)
	flush()
	if st.SessionID != "" {
		t.Errorf("SessionID should not change on error: %q", st.SessionID)
	}
	if !strings.Contains(buf.String(), "not found") {
		t.Errorf("output = %q", buf.String())
	}
}

// 9. 未知的斜杠命令返回 false（调用者当作普通 prompt 处理）。
func TestSlashCommand_Unknown(t *testing.T) {
	if handleSlashCommand(context.Background(), "/whatever", Options{}, &REPLState{}) {
		t.Errorf("unknown command returned true (should be false)")
	}
}

// 10. 端到端：REPL 从伪造的 stdin 依次运行 /help 和 /exit。
func TestREPL_E2E_HelpThenExit(t *testing.T) {
	buf, flush := captureStderr(t)
	in := strings.NewReader("/help\n/exit\n")
	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, _ = io.Copy(w, in)
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; _ = r.Close() })
	Run(context.Background(), &fakeRunner{}, false)
	flush()
	out := buf.String()
	if !strings.Contains(out, "/history") {
		t.Errorf("output missing /history: %q", out)
	}
}

// --- 辅助工具 ---

type fakeUsage struct {
	prompt     int
	completion int
}

func (f *fakeUsage) Snapshot() any {
	return fmt.Sprintf("Usage{prompt:%d completion:%d}", f.prompt, f.completion)
}

type fakeLoader struct {
	err     error
	payload any
}

func (f *fakeLoader) Load(_ context.Context, _ string) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.payload, nil
}
