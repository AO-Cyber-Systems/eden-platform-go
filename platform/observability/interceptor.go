package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	connect "connectrpc.com/connect"
)

// NewObservabilityInterceptor creates a ConnectRPC unary interceptor that
// records request count, duration, and logs each request via slog.
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

			slog.Info("rpc",
				"procedure", procedure,
				"method", method,
				"status", status,
				"duration", duration.String(),
			)

			return resp, err
		})
	})
}

// codeFromError extracts the Connect error code string from an error.
func codeFromError(err error) string {
	code := connect.CodeOf(err)
	return fmt.Sprintf("%d_%s", code, code.String())
}
