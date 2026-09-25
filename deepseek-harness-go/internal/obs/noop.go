package obs

import "context"

// NoopLogger 丢弃所有日志事件；默认实现（§C.5 "provider=noop 零开销"）。
type NoopLogger struct{}

func (NoopLogger) Debug(context.Context, string, ...Attr) {}
func (NoopLogger) Info(context.Context, string, ...Attr)  {}
func (NoopLogger) Warn(context.Context, string, ...Attr)  {}
func (NoopLogger) Error(context.Context, string, ...Attr) {}

// NoopTracer 产出 no-op Span。
type NoopTracer struct{}

// Start 返回原 ctx 和一个 no-op Span。
func (NoopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, NoopSpan{}
}

// NoopSpan 不记录任何东西。
type NoopSpan struct{}

func (NoopSpan) End()                    {}
func (NoopSpan) SetAttr(string, any)     {}
func (NoopSpan) RecordError(error)       {}

// NoopMeter 产出 no-op 指标。
type NoopMeter struct{}

// Counter 返回 no-op Counter。
func (NoopMeter) Counter(string) Counter { return NoopCounter{} }

// Histogram 返回 no-op Histogram。
func (NoopMeter) Histogram(string) Histogram { return NoopHistogram{} }

// NoopCounter 丢弃增量。
type NoopCounter struct{}

func (NoopCounter) Add(context.Context, int64, ...Attr) {}

// NoopHistogram 丢弃观测值。
type NoopHistogram struct{}

func (NoopHistogram) Record(context.Context, float64, ...Attr) {}
