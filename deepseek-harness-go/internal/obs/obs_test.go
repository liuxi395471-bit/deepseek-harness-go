package obs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestNoopDefaults(t *testing.T) {
	p := Defaults()
	ctx := context.Background()
	p.Logger.Debug(ctx, "d", A("k", 1))
	p.Logger.Info(ctx, "i")
	p.Logger.Warn(ctx, "w")
	p.Logger.Error(ctx, "e")
	_, span := p.Tracer.Start(ctx, "x")
	span.SetAttr("a", "b")
	span.RecordError(errors.New("boom"))
	span.End()
	p.Meter.Counter("c").Add(ctx, 1, A("k", "v"))
	p.Meter.Histogram("h").Record(ctx, 1.5)
}

func TestStderrLogger(t *testing.T) {
	var buf strings.Builder
	l := NewWriterLogger(&buf)
	ctx := context.Background()
	l.Info(ctx, "hello", A("model", "m1"), A("n", 3))
	l.Error(ctx, "bad")
	out := buf.String()
	if !strings.Contains(out, "INFO hello model=m1 n=3") {
		t.Fatalf("missing info line: %q", out)
	}
	if !strings.Contains(out, "ERROR bad") {
		t.Fatalf("missing error line: %q", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("want 2 lines, got %q", out)
	}
	// nil 接收者安全。
	var nilLogger *StderrLogger
	nilLogger.Info(ctx, "no panic")
}

func TestNewOTelTracer(t *testing.T) {
	h, err := NewOTelTracer(context.Background(), OTelConfig{
		Endpoint:    "127.0.0.1:1", // 不可达即可：BatchSpanProcessor 异步，不会阻塞
		ServiceName: "test-dsh",
		SampleRatio: 0.5,
	})
	if err != nil {
		t.Fatalf("NewOTelTracer: %v", err)
	}
	defer func() { _ = h.Close(context.Background()) }()

	ctx, span := h.Start(context.Background(), "agent.run")
	if span == nil {
		t.Fatal("nil span")
	}
	span.SetAttr("model", "glm")
	span.RecordError(errors.New("x"))
	span.End()

	// SDK tracer 应真正注册了 span（非 no-op）。
	if _, ok := h.Tracer.(*otelTracer); !ok {
		t.Fatalf("want *otelTracer, got %T", h.Tracer)
	}
	_ = ctx
	// 关闭第二次安全。
	if err := h.Close(context.Background()); err != nil {
		t.Logf("second close: %v", err)
	}
}

func TestOTelSpanEndsAfterClose(t *testing.T) {
	h, err := NewOTelTracer(context.Background(), OTelConfig{Endpoint: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewOTelTracer: %v", err)
	}
	_ = h.Close(context.Background())
	// close 后再 start 不应 panic（provider 已 shutdown 的行为由 SDK 决定）。
	_, _ = h.Start(context.Background(), "after-close")
}

// 编译期保证接口实现。
var (
	_ Logger    = NoopLogger{}
	_ Tracer    = NoopTracer{}
	_ Span      = NoopSpan{}
	_ Meter     = NoopMeter{}
	_ Counter   = NoopCounter{}
	_ Histogram = NoopHistogram{}
	_ Logger    = (*StderrLogger)(nil)
	_ Tracer    = (*otelTracer)(nil)
	_ Span      = (*otelSpan)(nil)
	_           = trace.Span(nil)
)

func TestOTelMeterInstruments(t *testing.T) {
	h, err := NewOTelTracer(context.Background(), OTelConfig{Endpoint: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewOTelTracer: %v", err)
	}
	ctx := context.Background()
	// 同包访问私有 meter：经 otelMeter 适配层取 instrument。
	m := &otelMeter{meter: h.meter}
	c := m.Counter("dsh.test.counter")
	c.Add(ctx, 5, A("k", "v"))
	hs := m.Histogram("dsh.test.histogram")
	hs.Record(ctx, 1.25, A("k", "v"))

	// 非法 instrument 名 → 退化为 no-op 不 panic。
	bad := m.Counter("bad name with spaces!")
	bad.Add(ctx, 1)
	badH := m.Histogram("bad/hist name!")
	badH.Record(ctx, 2)

	// Attr 类型分发：string/bool/int/int64/float64/error/default。
	_, span := h.Start(ctx, "attrs")
	span.SetAttr("s", "str")
	span.SetAttr("b", true)
	span.SetAttr("i", 3)
	span.SetAttr("i64", int64(4))
	span.SetAttr("f", 5.5)
	span.SetAttr("e", errors.New("boom"))
	span.SetAttr("d", []int{1, 2})
	span.RecordError(nil) // nil 不 panic
	span.End()

	// double close 安全。
	if err := h.Close(ctx); err != nil {
		t.Logf("close: %v", err)
	}
	_ = h.Close(ctx)
}

func TestStderrLoggerNilWriterSafe(t *testing.T) {
	l := &StderrLogger{} // w = nil
	l.Info(context.Background(), "no panic")
}
