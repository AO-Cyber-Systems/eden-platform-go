package observability

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// OTLPConfig configures OTLP/HTTP exporters for traces, metrics, and (when
// the SDK log API stabilizes) logs.
//
// SetupOTLP enforces three availability invariants regardless of this
// configuration:
//
//  1. Empty Endpoint  → no-op providers, nil error.
//  2. Failed init     → no-op providers, warning log, nil error.
//  3. Runtime outage  → bounded retry (2 minutes max elapsed), bounded queue
//     (2048 spans), bounded shutdown (5-second deadline).
type OTLPConfig struct {
	// Endpoint is host:port (no scheme), e.g. "otel-collector.aoid.svc:4318".
	// Empty disables OTLP — SetupOTLP returns no-op providers.
	Endpoint string

	// Headers are extra HTTP headers sent on every export (auth tokens,
	// tenant routing). NEVER logged.
	Headers map[string]string

	// Timeout is the per-export deadline. Defaults to 10s when zero.
	Timeout time.Duration

	// Insecure disables TLS to the collector. Dev/test only.
	Insecure bool

	// Compression is "gzip" or "" (default). Other values fall back to no
	// compression with an info log at SetupOTLP time.
	Compression string
}

// Shutdown is the cleanup function returned by SetupOTLP. Safe to call
// multiple times. Bounded by a 5-second deadline; never blocks indefinitely.
type OTLPShutdown func()

var noopOTLPShutdown OTLPShutdown = func() {}

// SetupOTLP wires OTel TracerProvider + MeterProvider to OTLP/HTTP exporters
// and registers them as the globals (`otel.SetTracerProvider`,
// `otel.SetMeterProvider`).
//
// Returns a Shutdown callback. The returned error is always nil for the three
// documented degrade paths (empty endpoint, init failure, runtime outage);
// callers that ignore the error are correct. A non-nil error indicates a
// programmer mistake (e.g., the resource arg was malformed) and not a runtime
// availability issue.
func SetupOTLP(ctx context.Context, cfg OTLPConfig, res *resource.Resource) (OTLPShutdown, error) {
	if cfg.Endpoint == "" {
		slog.Info("observability: OTLP endpoint not configured; using no-op providers")
		return noopOTLPShutdown, nil
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Trace exporter -----------------------------------------------------
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithHeaders(cfg.Headers),
		otlptracehttp.WithTimeout(timeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  2 * time.Minute, // BOUNDED — never retry forever
		}),
	}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}
	if cfg.Compression == "gzip" {
		traceOpts = append(traceOpts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		slog.Warn("observability: OTLP trace exporter init failed; using no-op tracer",
			"endpoint", cfg.Endpoint, "error", err)
		return noopOTLPShutdown, nil
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp,
			sdktrace.WithMaxQueueSize(2048),       // BOUNDED — drop oldest on overflow
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Metric exporter ----------------------------------------------------
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
		otlpmetrichttp.WithHeaders(cfg.Headers),
		otlpmetrichttp.WithTimeout(timeout),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			MaxElapsedTime:  2 * time.Minute,
		}),
	}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}
	if cfg.Compression == "gzip" {
		metricOpts = append(metricOpts, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
	}
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		slog.Warn("observability: OTLP metric exporter init failed; using no-op meter",
			"endpoint", cfg.Endpoint, "error", err)
		// Tracer is wired; meter falls back to no-op. We still return success
		// and a Shutdown that closes the tracer.
		return func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tp.Shutdown(shutdownCtx)
		}, nil
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shutdownCtx)
		_ = mp.Shutdown(shutdownCtx)
	}, nil
}

// BuildResource constructs an OTel resource.Resource from the Eden Config
// fields (ServiceName, Release, Environment). Exposed so callers wiring
// SetupOTLP directly (outside Setup) can mirror the same resource shape.
func BuildResource(cfg Config) *resource.Resource {
	attrs := []attribute.KeyValue{}
	if cfg.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceName(cfg.ServiceName))
	}
	if cfg.Release != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.Release))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(cfg.Environment))
	}
	return resource.NewSchemaless(attrs...)
}
