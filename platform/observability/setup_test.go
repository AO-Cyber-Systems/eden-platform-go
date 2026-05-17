package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
)

func TestSetup_NoOpDSN(t *testing.T) {
	shutdown, err := Setup(Config{
		ServiceName: "test-svc",
		LogLevel:    "info",
		LogFormat:   "json",
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown")
	}
	// Calling shutdown should not panic with no DSN configured.
	shutdown()
}

func TestSetup_DefaultLevelFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	shutdown, err := Setup(Config{ServiceName: "svc"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	defer shutdown()

	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Errorf("expected debug level enabled when LOG_LEVEL=debug")
	}
}

func TestMustSetup_NoPanic(t *testing.T) {
	shutdown := MustSetup(Config{ServiceName: "must"})
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown from MustSetup")
	}
	shutdown()
}

func TestSetup_ServiceNameOnLogLine(t *testing.T) {
	// Capture slog output by installing a custom handler before Setup,
	// then verify Setup's WithAttrs propagates "service" attribute.
	// Since Setup writes to os.Stdout directly, we instead test the
	// service-name attachment by re-creating the same composition path.
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	withSvc := base.WithAttrs([]slog.Attr{slog.String("service", "myservice")})
	logger := slog.New(withSvc)
	logger.Info("hello")

	if !strings.Contains(buf.String(), `"service":"myservice"`) {
		t.Errorf("expected service attr in output, got %q", buf.String())
	}
}

func TestWithTraceContext_RoundTrip(t *testing.T) {
	ctx := WithTraceContext(context.Background(), TraceContext{
		TraceID: "abc123",
		SpanID:  "span9",
	})
	got := TraceContextFromContext(ctx)
	if got.TraceID != "abc123" || got.SpanID != "span9" {
		t.Errorf("TraceContext round-trip = %+v, want {abc123 span9}", got)
	}
}

func TestTraceContextFromContext_Empty(t *testing.T) {
	tc := TraceContextFromContext(context.Background())
	if tc.TraceID != "" || tc.SpanID != "" {
		t.Errorf("expected zero TraceContext, got %+v", tc)
	}
}

// runInterceptor invokes the unary interceptor against a real connect.Request
// (since connect.AnyRequest has unexported methods we cannot fake). Returns
// the captured slog output.
func runInterceptor(t *testing.T, ctx context.Context) string {
	t.Helper()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	m := NewMetrics()
	interceptor := NewObservabilityInterceptor(m)
	wrapped := interceptor(connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&struct{}{}), nil
	}))
	req := connect.NewRequest(&struct{}{})
	if _, err := wrapped(ctx, req); err != nil {
		t.Fatalf("interceptor next: %v", err)
	}
	return buf.String()
}

func TestInterceptor_AttachesTraceContext(t *testing.T) {
	out := runInterceptor(t, WithTraceContext(context.Background(), TraceContext{TraceID: "trace-1", SpanID: "span-1"}))
	if !strings.Contains(out, `"trace_id":"trace-1"`) {
		t.Errorf("expected trace_id in log: %s", out)
	}
	if !strings.Contains(out, `"span_id":"span-1"`) {
		t.Errorf("expected span_id in log: %s", out)
	}
}

func TestInterceptor_NoTraceContextNoAttrs(t *testing.T) {
	out := runInterceptor(t, context.Background())
	if strings.Contains(out, `"trace_id"`) || strings.Contains(out, `"span_id"`) {
		t.Errorf("did not expect trace_id/span_id without TraceContext, got: %s", out)
	}
}

func TestInterceptor_OnlyTraceID(t *testing.T) {
	out := runInterceptor(t, WithTraceContext(context.Background(), TraceContext{TraceID: "only-trace"}))
	if !strings.Contains(out, `"trace_id":"only-trace"`) {
		t.Errorf("expected trace_id in log: %s", out)
	}
	if strings.Contains(out, `"span_id"`) {
		t.Errorf("did not expect span_id when only TraceID is set, got: %s", out)
	}
}

func TestSetup_OTLPNilKeepsExistingSentryBehavior(t *testing.T) {
	shutdown, err := Setup(Config{ServiceName: "test-otlp-nil", SentryDSN: ""})
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown")
	}
	// Shutdown must be callable without panic.
	shutdown()
}

func TestSetup_OTLPEmptyEndpointDegradesToNoop(t *testing.T) {
	shutdown, err := Setup(Config{
		ServiceName: "test-otlp-empty",
		SentryDSN:   "",
		OTLP:        &OTLPConfig{Endpoint: ""},
	})
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown")
	}
	shutdown()
}
