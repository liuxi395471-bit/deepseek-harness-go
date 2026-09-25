package obs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// OTelConfig 是 obs.provider=otel 时的设置（§C.4）。
type OTelConfig struct {
	Endpoint    string  // OTLP gRPC endpoint，例如 localhost:4317
	ServiceName string  // resource.service.name
	SampleRatio float64 // [0,1]；父采样优先
}

// OTelTracerHandle 同时是 Tracer 并提供 Close（flush + shutdown）。
// 持有 MeterProvider 句柄；Meter() 返回 OTel SDK 的 Meter 实现。
type OTelTracerHandle struct {
	Tracer
	tp    *sdktrace.TracerProvider
	mp    *sdkmetric.MeterProvider
	meter metric.Meter
	m     Meter
}

// Meter 返回 OTel SDK 的 Meter 实现（满足 obs.Meter 接口）。
// 调用方应通过 obs.Provider.M() 间接使用；本方法仅用于直测。
func (h *OTelTracerHandle) Meter() Meter { return h.m }

// NewOTelTracer 初始化 OTel Tracer + Meter provider（§C.2）。
// 进程退出前应调用 Close flush。
func NewOTelTracer(ctx context.Context, cfg OTelConfig) (*OTelTracerHandle, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "localhost:4317"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "dsh"
	}
	if cfg.SampleRatio <= 0 || cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("obs: otlp exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(cfg.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("obs: resource: %w", err)
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	tr := tp.Tracer("deepseek-harness-go/dsh")
	mt := mp.Meter("deepseek-harness-go/dsh")
	return &OTelTracerHandle{
		Tracer: &otelTracer{tr: tr},
		tp:     tp,
		mp:     mp,
		meter:  mt,
		m:      &otelMeter{meter: mt},
	}, nil
}

// Close flush 并关闭两个 provider；进程退出前应调用。
func (h *OTelTracerHandle) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	var firstErr error
	if h.tp != nil {
		if err := h.tp.ForceFlush(ctx); err != nil {
			firstErr = fmt.Errorf("obs: flush traces: %w", err)
		}
		if err := h.tp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if h.mp != nil {
		if err := h.mp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// otelTracer 用 OTel SDK 实现 Tracer。
type otelTracer struct {
	tr trace.Tracer
}

// Start 实现 Tracer。
func (t *otelTracer) Start(ctx context.Context, name string) (context.Context, Span) {
	ctx, sp := t.tr.Start(ctx, name)
	return ctx, &otelSpan{sp: sp}
}

// otelSpan 包装 trace.Span。
type otelSpan struct {
	sp trace.Span
}

func (s *otelSpan) End() { s.sp.End() }

func (s *otelSpan) SetAttr(k string, v any) {
	s.sp.SetAttributes(toKV([]Attr{{Key: k, Value: v}})...)
}

func (s *otelSpan) RecordError(err error) {
	if err != nil {
		s.sp.RecordError(err)
		s.sp.SetAttributes(attribute.Bool("error", true))
	}
}

// otelMeter 用 OTel metrics API 实现 Meter。
type otelMeter struct {
	meter metric.Meter
}

// Counter 返回（或惰性创建）名为 name 的计数器。
func (m *otelMeter) Counter(name string) Counter {
	inst, err := m.meter.Int64Counter(name)
	if err != nil {
		// instrument 创建失败（例如名称非法）时退化为 no-op，
		// 不让可观测性问题影响主流程。
		return NoopCounter{}
	}
	return &otelCounter{inst: inst}
}

// Histogram 返回（或惰性创建）名为 name 的直方图。
func (m *otelMeter) Histogram(name string) Histogram {
	inst, err := m.meter.Float64Histogram(name)
	if err != nil {
		return NoopHistogram{}
	}
	return &otelHist{inst: inst}
}

type otelCounter struct {
	inst metric.Int64Counter
}

func (c *otelCounter) Add(ctx context.Context, delta int64, attrs ...Attr) {
	c.inst.Add(ctx, delta, metric.WithAttributes(toKV(attrs)...))
}

type otelHist struct {
	inst metric.Float64Histogram
}

func (h *otelHist) Record(ctx context.Context, value float64, attrs ...Attr) {
	h.inst.Record(ctx, value, metric.WithAttributes(toKV(attrs)...))
}

// toKV 把通用 Attr 转成 OTel 属性。v1.46 的 attribute 包没有
// 泛型 Any 构造器，因此这里按常用类型显式分发，其余值退化为字符串。
func toKV(attrs []Attr) []attribute.KeyValue {
	kv := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		switch v := a.Value.(type) {
		case string:
			kv = append(kv, attribute.String(a.Key, v))
		case bool:
			kv = append(kv, attribute.Bool(a.Key, v))
		case int:
			kv = append(kv, attribute.Int(a.Key, v))
		case int64:
			kv = append(kv, attribute.Int64(a.Key, v))
		case float64:
			kv = append(kv, attribute.Float64(a.Key, v))
		case error:
			kv = append(kv, attribute.String(a.Key, v.Error()))
		default:
			kv = append(kv, attribute.String(a.Key, fmt.Sprint(v)))
		}
	}
	return kv
}
