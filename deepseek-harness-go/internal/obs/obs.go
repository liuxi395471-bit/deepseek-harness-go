// Package obs 是 v2 的统一可观测性接缝。所有 I/O 路径（HTTP、SSE、
// SQL、工具执行）都经由本包，以便调用方日后替换日志实现时无需改动
// 调用点。
//
// 默认实现是 no-op，既保留 v1 行为，又保持零依赖（DESIGN-v2 §0.1 P7）。
//
// 状态：v2 横切骨架。
package obs

// Logger 是调用方可使用的最小接口。实现包括：
//
//   - NoopLogger（默认，此处）：丢弃所有事件
//   - StderrLogger（T8 及之后）：向 stderr 写入类 JSON 行
//
// TODO：在后续里程碑中拆分为 Logger / Hook / Span。
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// Noop 是默认日志器；当 harness debug=true（v1 开关）或通过环境变量
// DSH_LOG=stderr（v2）时，在启动阶段替换为真正的实现。
var Noop Logger = noopLogger{}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
