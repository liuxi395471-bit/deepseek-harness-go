// Package obs 是 harness 的统一可观测性接缝（DESIGN-v3 §C）。
//
// v3 将 v2 的 no-op Logger 升级为完整的 Logger / Tracer / Span / Meter
// 接口族；默认实现全部为 no-op，零外部依赖。当配置 obs.provider=otel
// 时（§C.2），由 otel.go 提供 OTel SDK 实现（opt-in）。
package obs

import "context"

// Attr 是结构化属性的键值对。
type Attr struct {
	Key   string
	Value any
}

// A 是 Attr 的便捷构造函数。
func A(key string, val any) Attr { return Attr{Key: key, Value: val} }

// Logger 是调用方可使用的最小日志接口。
type Logger interface {
	Debug(ctx context.Context, msg string, attrs ...Attr)
	Info(ctx context.Context, msg string, attrs ...Attr)
	Warn(ctx context.Context, msg string, attrs ...Attr)
	Error(ctx context.Context, msg string, attrs ...Attr)
}

// Tracer 创建 Span。
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// Span 是一次可观测的操作区间。
type Span interface {
	End()
	SetAttr(key string, val any)
	RecordError(err error)
}

// Meter 是指标工厂（§C.1）。
type Meter interface {
	Counter(name string) Counter
	Histogram(name string) Histogram
}

// Counter 是单调递增计数器。
type Counter interface {
	Add(ctx context.Context, delta int64, attrs ...Attr)
}

// Histogram 是数值分布。
type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...Attr)
}

// Provider 聚合三类可观测原语，供 Runner / 工具 / 客户端注入。
// 零值安全：通过 L/T/M 访问器取用，未注入的字段返回 no-op。
type Provider struct {
	Logger Logger
	Tracer Tracer
	Meter  Meter
}

// L 返回 Logger；未注入时返回 no-op。
func (p Provider) L() Logger {
	if p.Logger == nil {
		return NoopLogger{}
	}
	return p.Logger
}

// T 返回 Tracer；未注入时返回 no-op。
func (p Provider) T() Tracer {
	if p.Tracer == nil {
		return NoopTracer{}
	}
	return p.Tracer
}

// M 返回 Meter；未注入时返回 no-op。
func (p Provider) M() Meter {
	if p.Meter == nil {
		return NoopMeter{}
	}
	return p.Meter
}

// Defaults 返回全 no-op 的 Provider。
func Defaults() Provider {
	return Provider{
		Logger: NoopLogger{},
		Tracer: NoopTracer{},
		Meter:  NoopMeter{},
	}
}
