package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	connect "connectrpc.com/connect"
)

// traceContextKey is the context key for upstream-injected trace context. An
// upstream OTel middleware (or any equivalent) calls WithTraceContext to
// attach trace_id / span_id; the interceptor pulls them off and includes them
// on the per-request slog log line. No new SDK dependency — the package is
// SDK-agnostic.
type traceContextKey struct{}

// TraceContext carries the bare trace_id and span_id strings the interceptor
// emits on its per-RPC log line. Empty fields are skipped.
type TraceContext struct {
	TraceID string
	SpanID  string
}

// WithTraceContext returns a context carrying the given TraceContext.
// Upstream middleware (OTel, custom propagator) populates this so the
// interceptor can include trace IDs in structured logs without taking on the
// OTel dependency.
func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// TraceContextFromContext returns the TraceContext attached via
// WithTraceContext. The zero TraceContext is returned when none is set.
func TraceContextFromContext(ctx context.Context) TraceContext {
	tc, _ := ctx.Value(traceContextKey{}).(TraceContext)
	return tc
}

// NewObservabilityInterceptor creates a ConnectRPC unary interceptor that
// records request count, duration, and logs each request via slog. When the
// request context carries a TraceContext (set by upstream OTel middleware via
// WithTraceContext), trace_id and span_id are attached to the log line.
func NewObservabilityInterceptor(m *Metrics) connect.UnaryInterceptorFunc {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			procedure := req.Spec().Procedure
			method := req.HTTPMethod()
			start := time.Now()

			m.ActiveConnections.Inc()
			defer m.ActiveConnections.Dec()

			resp, err := next(ctx, req)

			duration := time.Since(start)
			status := "ok"
			if err != nil {
				status = codeFromError(err)
			}

			m.RequestsTotal.WithLabelValues(method, procedure, status).Inc()
			m.RequestDuration.WithLabelValues(method, procedure).Observe(duration.Seconds())

			attrs := []any{
				"procedure", procedure,
				"method", method,
				"status", status,
				"duration", duration.String(),
			}
			if tc := TraceContextFromContext(ctx); tc.TraceID != "" || tc.SpanID != "" {
				if tc.TraceID != "" {
					attrs = append(attrs, "trace_id", tc.TraceID)
				}
				if tc.SpanID != "" {
					attrs = append(attrs, "span_id", tc.SpanID)
				}
			}

			slog.Info("rpc", attrs...)

			return resp, err
		})
	})
}

// codeFromError extracts the Connect error code string from an error.
func codeFromError(err error) string {
	code := connect.CodeOf(err)
	return fmt.Sprintf("%d_%s", code, code.String())
}
