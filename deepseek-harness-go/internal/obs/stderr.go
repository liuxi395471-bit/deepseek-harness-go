package obs

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// StderrLogger 向指定 io.Writer（默认 os.Stderr）写入人类可读的
// 类 JSON 行。dsh 在 debug=true 时用它替换 no-op。
type StderrLogger struct {
	w  io.Writer
	mu sync.Mutex
}

// NewStderrLogger 构造向 os.Stderr 写入的 Logger。
func NewStderrLogger() *StderrLogger { return &StderrLogger{w: os.Stderr} }

// NewWriterLogger 构造向任意 writer 写入的 Logger（测试用）。
func NewWriterLogger(w io.Writer) *StderrLogger { return &StderrLogger{w: w} }

func (l *StderrLogger) log(_ context.Context, level, msg string, attrs ...Attr) {
	if l == nil || l.w == nil {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString(" ")
	b.WriteString(level)
	b.WriteString(" ")
	b.WriteString(msg)
	for _, a := range attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
	}
	b.WriteString("\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, b.String())
}

func (l *StderrLogger) Debug(ctx context.Context, msg string, attrs ...Attr) { l.log(ctx, "DEBUG", msg, attrs...) }
func (l *StderrLogger) Info(ctx context.Context, msg string, attrs ...Attr)  { l.log(ctx, "INFO", msg, attrs...) }
func (l *StderrLogger) Warn(ctx context.Context, msg string, attrs ...Attr)  { l.log(ctx, "WARN", msg, attrs...) }
func (l *StderrLogger) Error(ctx context.Context, msg string, attrs ...Attr) { l.log(ctx, "ERROR", msg, attrs...) }
