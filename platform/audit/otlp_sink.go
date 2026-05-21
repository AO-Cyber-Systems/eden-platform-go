package audit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	otelsdk "go.opentelemetry.io/otel/sdk/log"
)

// errCapturingExporter wraps an otelsdk.Exporter and records the first Export
// error seen since the last reset. It exists because the OTel log API's
// Logger.Emit has no error return and SimpleProcessor routes Export failures to
// the global error handler — so without this wrapper a transport error (server
// 5xx, network down, timeout) would never reach OTLPLogSink.Send, defeating the
// Forwarder's backoff/retry contract.
type errCapturingExporter struct {
	otelsdk.Exporter
	mu      sync.Mutex
	lastErr error
}

func (e *errCapturingExporter) Export(ctx context.Context, records []otelsdk.Record) error {
	err := e.Exporter.Export(ctx, records)
	if err != nil {
		e.mu.Lock()
		if e.lastErr == nil {
			e.lastErr = err
		}
		e.mu.Unlock()
	}
	return err
}

// reset clears any captured error. Called at the start of each Send so a
// failure on one batch does not leak into the next.
func (e *errCapturingExporter) reset() {
	e.mu.Lock()
	e.lastErr = nil
	e.mu.Unlock()
}

// err returns the first Export error captured since the last reset, or nil.
func (e *errCapturingExporter) err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastErr
}

// OTLPLogSink is the production Sink: it emits one OTLP log record per
// BufferedEvent to a downstream OTLP/HTTP receiver (typically the AOAudit
// service's /v1/logs ingest endpoint).
//
// The mapping is:
//
//	JWS Compact         -> attribute "aoid.audit.jws"
//	Schema version "1"  -> attribute "aoid.audit.schema_version"
//	jti                 -> attribute "aoid.audit.jti"
//	tenant_id           -> attribute "aoid.audit.tenant_id"
//	service.name        -> attribute "service.name" = "aoid"
//	Canonical payload   -> log record body (string)
//
// AOAudit (the verifier-side service) consumes these attributes:
//   - reads aoid.audit.jws,
//   - extracts the JWS header's kid,
//   - fetches the AOID JWKS endpoint,
//   - verifies signature via the SAME canonical_json package.
type OTLPLogSink struct {
	logger    otellog.Logger
	provider  *otelsdk.LoggerProvider
	exporter  *errCapturingExporter
	timeout   time.Duration
	scopeName string
}

// NewOTLPLogSink constructs an OTLPLogSink. endpoint is the full OTLP HTTP URL
// (e.g. https://aoaudit.example.com/v1/logs). headers are added to every
// request (use for Authorization tokens, mTLS pinning, etc.).
//
// Endpoint scheme is honored — http:// disables TLS (test environments), and
// https:// enables it (production). For mTLS or custom CA configuration,
// future work can extend this constructor with additional otlploghttp options.
func NewOTLPLogSink(ctx context.Context, endpoint string, headers map[string]string) (*OTLPLogSink, error) {
	if endpoint == "" {
		return nil, errors.New("audit otlp sink: empty endpoint")
	}
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(endpoint),
	}
	if len(headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(headers))
	}
	exporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("audit otlp sink: exporter init: %w", err)
	}
	// SimpleProcessor (not BatchProcessor): the Forwarder already batches at
	// the buffer-dequeue layer (default 100 rows per Send) and applies its own
	// backoff, so a synchronous, non-buffering processor is what we want.
	//
	// The OTel log SDK gives us no synchronous error path on its own —
	// Logger.Emit returns nothing and SimpleProcessor hands Export errors to
	// the global error handler. errCapturingExporter wraps the exporter so
	// Send can read back the transport error after flushing and propagate it
	// to the Forwarder for backoff.
	capExp := &errCapturingExporter{Exporter: exporter}
	provider := otelsdk.NewLoggerProvider(
		otelsdk.WithProcessor(otelsdk.NewSimpleProcessor(capExp)),
	)
	const scope = "aoid.audit"
	logger := provider.Logger(scope, otellog.WithInstrumentationVersion("1.0"))
	return &OTLPLogSink{
		logger:    logger,
		provider:  provider,
		exporter:  capExp,
		timeout:   30 * time.Second,
		scopeName: scope,
	}, nil
}

// SetTimeout overrides the default 30s per-batch timeout.
func (s *OTLPLogSink) SetTimeout(d time.Duration) { s.timeout = d }

// Send emits each event as a single OTLP log record + force-flushes so the
// caller (Forwarder) gets synchronous error feedback for backoff decisions.
//
// Returns an error if ForceFlush fails (network down, server 5xx, timeout).
// The Forwarder treats the whole batch as failed — downstream dedup is via
// jti UNIQUE on the receiving side.
func (s *OTLPLogSink) Send(ctx context.Context, events []BufferedEvent) error {
	if s.logger == nil || s.provider == nil {
		return errors.New("audit otlp sink: nil logger/provider")
	}
	if len(events) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	s.exporter.reset()
	for _, ev := range events {
		rec := otellog.Record{}
		rec.SetTimestamp(ev.EmittedAt)
		rec.SetObservedTimestamp(time.Now())
		rec.SetSeverity(otellog.SeverityInfo)
		rec.SetBody(otellog.StringValue(string(ev.PayloadCanon)))
		rec.AddAttributes(
			otellog.String("aoid.audit.jws", ev.JWSCompact),
			otellog.String("aoid.audit.schema_version", "1"),
			otellog.String("aoid.audit.jti", ev.JTI),
			otellog.String("aoid.audit.tenant_id", ev.TenantID.String()),
			otellog.String("service.name", "aoid"),
		)
		s.logger.Emit(ctx, rec)
	}
	// ForceFlush is a no-op for SimpleProcessor (records export inline during
	// Emit) but is kept for correctness if the processor ever changes.
	if err := s.provider.ForceFlush(ctx); err != nil {
		return fmt.Errorf("audit otlp sink: flush: %w", err)
	}
	// Surface any transport error captured during the inline exports above so
	// the Forwarder treats the whole batch as failed and applies backoff.
	if err := s.exporter.err(); err != nil {
		return fmt.Errorf("audit otlp sink: export: %w", err)
	}
	return nil
}

// Shutdown gracefully closes the underlying provider. Call on AOID shutdown
// (e.g. cmd/aoid/boot.go's cleanup path) to flush any remaining batched
// records.
func (s *OTLPLogSink) Shutdown(ctx context.Context) error {
	if s.provider == nil {
		return nil
	}
	return s.provider.Shutdown(ctx)
}

// Compile-time interface assertion.
var _ Sink = (*OTLPLogSink)(nil)
