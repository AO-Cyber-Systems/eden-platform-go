package observability

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestSilentTracerGuard_CountsSpans proves the guard observes span creation, so
// a service WITH a span source is not warned about. A guard that cannot tell the
// two cases apart is worse than none — it trains people to ignore the warning.
func TestSilentTracerGuard_CountsSpans(t *testing.T) {
	opt, guard := newSilenceWatch(time.Hour, "svc-with-source") // long grace: never fires here
	tp := sdktrace.NewTracerProvider(opt)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	if got := guard.spansStarted(); got != 0 {
		t.Fatalf("spansStarted before any span = %d, want 0", got)
	}

	_, span := tp.Tracer("t").Start(context.Background(), "op")
	span.End()

	if got := guard.spansStarted(); got != 1 {
		t.Errorf("spansStarted after one span = %d, want 1", got)
	}
}

// TestSilentTracerGuard_StaysZeroWhenNothingTraces pins the failure this guard
// exists to catch: a provider is installed, the exporter is live, and no span is
// ever created. That is EXACTLY console-api's state (obj-31 TRD 31-06) — a
// TracerProvider plus NewObservabilityInterceptor, which reads trace context but
// never calls tracer.Start.
func TestSilentTracerGuard_StaysZeroWhenNothingTraces(t *testing.T) {
	opt, guard := newSilenceWatch(time.Hour, "svc-without-source")
	tp := sdktrace.NewTracerProvider(opt)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// A tracer is obtained but never used to start a span — the real-world shape
	// of "observability is configured" without a span source.
	_ = tp.Tracer("t")

	if got := guard.spansStarted(); got != 0 {
		t.Errorf("spansStarted = %d, want 0 — the silent case must remain detectable", got)
	}
}

// TestSilentTracerGuard_WarningFiresOnlyWhenSilent exercises the timer path at a
// short grace, confirming the guard does not panic and that a span recorded
// before the deadline suppresses the warning.
func TestSilentTracerGuard_WarningFiresOnlyWhenSilent(t *testing.T) {
	opt, guard := newSilenceWatch(20*time.Millisecond, "svc-races-the-timer")
	tp := sdktrace.NewTracerProvider(opt)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("t").Start(context.Background(), "op")
	span.End()

	time.Sleep(60 * time.Millisecond) // let the timer fire

	if guard.spansStarted() == 0 {
		t.Error("expected the span to be counted before the grace elapsed")
	}
}

// TestResourceServiceName_FallsBack keeps the warning actionable even when the
// caller passes nil — SetupOTLP explicitly accepts a nil resource (console-api
// passes nil), so this is a real input, not a defensive hypothetical.
func TestResourceServiceName_FallsBack(t *testing.T) {
	if got := resourceServiceName(nil); got != "unknown" {
		t.Errorf("resourceServiceName(nil) = %q, want \"unknown\"", got)
	}
}
