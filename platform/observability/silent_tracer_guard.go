package observability

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// defaultSilenceGrace is how long a process may run without creating a span
// before the guard warns. Long enough that a slow boot or an idle-at-startup
// worker does not trip it; short enough that the warning lands in the same log
// window an operator is already looking at.
const defaultSilenceGrace = 2 * time.Minute

// silentTracerGuard detects the "configured but dark" failure: a TracerProvider
// is installed and exporting correctly, NOTHING in the process ever starts a
// span, so zero telemetry arrives and every health signal stays green.
//
// opsCluster obj-31 TRD 31-13. This is not hypothetical — it shipped twice:
//
//   - console-api (TRD 31-06) wired SetupOTLP with a correct endpoint and
//     Insecure:true, plus NewObservabilityInterceptor, and exported ZERO spans
//     for its entire life. The interceptor only READS trace context to decorate
//     log lines; it never calls tracer.Start. That pairing LOOKS like a complete
//     tracing setup and is not — it is the specific trap this guard exists for.
//   - aocore (TRD 31-03) had a working exporter whose otelhttp middleware was
//     stranded on an unmerged branch — same symptom, different cause.
//
// Neither was detectable by reading config: the env was right, the provider was
// live, the code compiled. Only "is this service actually emitting?" revealed
// them, and nobody asks that until something else breaks. This turns that
// question into a startup warning.
//
// Cost: one atomic increment per span start, and one timer per process.
type silentTracerGuard struct {
	started atomic.Int64
}

// OnStart counts span creations — deliberately the cheapest possible hook.
func (g *silentTracerGuard) OnStart(context.Context, sdktrace.ReadWriteSpan) {
	g.started.Add(1)
}

func (g *silentTracerGuard) OnEnd(sdktrace.ReadOnlySpan)      {}
func (g *silentTracerGuard) Shutdown(context.Context) error   { return nil }
func (g *silentTracerGuard) ForceFlush(context.Context) error { return nil }

// spansStarted reports how many spans this provider has begun. Exported for
// tests; not part of the public API surface.
func (g *silentTracerGuard) spansStarted() int64 { return g.started.Load() }

// newSilenceWatch returns a TracerProvider option that installs the guard, plus
// the guard itself. After grace elapses it logs ONE warning if no span has been
// started.
//
// It warns exactly once and never repeats. A service that is legitimately idle
// at boot (a worker with an empty queue, a controller with nothing to
// reconcile) would otherwise spam its logs into uselessness. One clear line is
// enough to be found by grep or the log pipeline — the durable guard is the
// collector-zero-spans-per-service alert, not this.
func newSilenceWatch(grace time.Duration, serviceName string) (sdktrace.TracerProviderOption, *silentTracerGuard) {
	g := &silentTracerGuard{}
	if grace <= 0 {
		grace = defaultSilenceGrace
	}
	// AfterFunc's timer is intentionally not retained: it fires at most once and
	// must not keep the process alive or require cleanup on shutdown.
	time.AfterFunc(grace, func() {
		if g.spansStarted() > 0 {
			return
		}
		slog.Warn("observability: OTLP is configured but NO spans have been created — "+
			"the exporter is running and will ship nothing. A TracerProvider is not a span "+
			"source: wrap the HTTP handler with otelhttp.NewHandler, or call tracer.Start "+
			"explicitly. NOTE: NewObservabilityInterceptor does NOT create spans — it only "+
			"reads trace context to decorate log lines.",
			"service", serviceName,
			"grace", grace.String(),
		)
	})
	return sdktrace.WithSpanProcessor(g), g
}

// resourceServiceName extracts service.name from a resource for the warning
// message, falling back to "unknown" so the log line is still actionable when
// the caller passed a nil or nameless resource (SetupOTLP accepts nil).
func resourceServiceName(res *resource.Resource) string {
	if res == nil {
		return "unknown"
	}
	for _, kv := range res.Attributes() {
		if string(kv.Key) == "service.name" {
			return kv.Value.AsString()
		}
	}
	return "unknown"
}
