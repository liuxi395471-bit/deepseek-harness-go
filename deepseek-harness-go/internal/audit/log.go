package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Logger 是审计写入契约（§E.3）。
type Logger interface {
	Log(ctx context.Context, ev Event)
	Close() error
}

// HashArgs 计算参数的 SHA-256 hex 摘要（脱敏模式使用）。
func HashArgs(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// FileLogger 把事件按 JSONL 追加到文件。
// 并发安全：内部互斥锁串行化写入（多 session 并发时 session_id
// 各自带在 Event 上，见 §E.5）。
type FileLogger struct {
	mu     sync.Mutex
	w      io.WriteCloser
	redact bool
}

// NewFileLogger 打开（或创建）path 并以追加模式写 JSONL。
// redact=true 时 Log 会剥除 ArgsRaw；调用方也可以直接传已脱敏的
// Event，两种写法都安全。
func NewFileLogger(path string, redact bool) (*FileLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return &FileLogger{w: f, redact: redact}, nil
}

// NewWriterLogger 写入任意 writer（测试 / 内存审查用）。
func NewWriterLogger(w io.WriteCloser, redact bool) *FileLogger {
	return &FileLogger{w: w, redact: redact}
}

// Log 写入一条事件。redact 模式剥除 ArgsRaw 并确保 ArgsHash 存在
// （若调用方未算，则由这里对 ArgsRaw 求值）。时间戳缺省补齐为当前
// UTC 时间。写入失败被静默吞掉：审计失败不应拖垮主流程。
func (l *FileLogger) Log(_ context.Context, ev Event) {
	if l == nil || l.w == nil {
		return
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	if l.redact {
		if ev.ArgsRaw != "" {
			if ev.ArgsHash == "" {
				ev.ArgsHash = HashArgs([]byte(ev.ArgsRaw))
			}
			ev.ArgsRaw = ""
		}
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(line, '\n'))
}

// Close 关闭底层文件。
func (l *FileLogger) Close() error {
	if l == nil || l.w == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.w.Close()
	l.w = nil
	return err
}

// Redact 编译期自检用的小工具：证明 HashArgs 是确定性 SHA-256。
func RedactSample(raw string) string { return HashArgs([]byte(raw)) }
