package observability

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	connect "connectrpc.com/connect"
	dto "github.com/prometheus/client_model/go"
)

func TestInitLogging_SetsLevel(t *testing.T) {
	InitLogging("debug", "text")
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be enabled after InitLogging(\"debug\", \"text\")")
	}

	InitLogging("error", "text")
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be disabled after InitLogging(\"error\", \"text\")")
	}
}

func TestInitLogging_JSONFormat(t *testing.T) {
	// Should not panic with JSON format.
	InitLogging("info", "json")
}

func TestNewMetrics_RegistersCollectors(t *testing.T) {
	m := NewMetrics()
	if m.Registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if m.RequestsTotal == nil {
		t.Fatal("expected non-nil RequestsTotal")
	}
	if m.RequestDuration == nil {
		t.Fatal("expected non-nil RequestDuration")
	}
	if m.ActiveConnections == nil {
		t.Fatal("expected non-nil ActiveConnections")
	}

	// Verify we can gather metrics (collectors are registered).
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	if len(families) == 0 {
		t.Error("expected at least one metric family from gather")
	}
}

func TestMetrics_RequestsTotal_Increments(t *testing.T) {
	m := NewMetrics()

	m.RequestsTotal.WithLabelValues("POST", "/test.v1.Svc/Method", "ok").Inc()
	m.RequestsTotal.WithLabelValues("POST", "/test.v1.Svc/Method", "ok").Inc()

	var metric dto.Metric
	err := m.RequestsTotal.WithLabelValues("POST", "/test.v1.Svc/Method", "ok").Write(&metric)
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if got := metric.GetCounter().GetValue(); got != 2 {
		t.Errorf("expected counter value 2, got %v", got)
	}
}

func TestMetrics_RequestDuration_Observes(t *testing.T) {
	m := NewMetrics()

	m.RequestDuration.WithLabelValues("POST", "/test.v1.Svc/Method").Observe(0.5)

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "http_request_duration_seconds" {
			found = true
			for _, met := range f.GetMetric() {
				if met.GetHistogram().GetSampleCount() != 1 {
					t.Errorf("expected 1 sample, got %d", met.GetHistogram().GetSampleCount())
				}
			}
		}
	}
	if !found {
		t.Error("http_request_duration_seconds metric family not found")
	}
}

func TestMetricsHandler_ReturnsHandler(t *testing.T) {
	m := NewMetrics()
	handler := m.MetricsHandler()
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestCodeFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"connect error", connect.NewError(connect.CodeNotFound, errors.New("not found")), "not_found"},
		{"plain error", errors.New("plain"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codeFromError(tt.err)
			if got == "" {
				t.Error("expected non-empty code string")
			}
			// Just verify it doesn't panic and returns something.
			t.Logf("codeFromError(%v) = %q", tt.err, got)
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"INFO", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewObservabilityInterceptor_NotNil(t *testing.T) {
	m := NewMetrics()
	interceptor := NewObservabilityInterceptor(m)
	if interceptor == nil {
		t.Fatal("expected non-nil interceptor")
	}
}

func TestInterceptor_MetricsDirectIncrement(t *testing.T) {
	// Verify the metrics used by the interceptor work correctly when
	// incremented directly (the interceptor calls these same methods).
	m := NewMetrics()

	m.RequestsTotal.WithLabelValues("POST", "/test.v1.Svc/Method", "ok").Inc()
	m.RequestDuration.WithLabelValues("POST", "/test.v1.Svc/Method").Observe(0.123)
	m.ActiveConnections.Inc()
	m.ActiveConnections.Dec()

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}

	foundCounter := false
	foundHistogram := false
	for _, f := range families {
		switch f.GetName() {
		case "http_requests_total":
			foundCounter = true
			for _, met := range f.GetMetric() {
				if met.GetCounter().GetValue() != 1 {
					t.Errorf("expected counter=1, got %v", met.GetCounter().GetValue())
				}
			}
		case "http_request_duration_seconds":
			foundHistogram = true
		}
	}
	if !foundCounter {
		t.Error("http_requests_total not found")
	}
	if !foundHistogram {
		t.Error("http_request_duration_seconds not found")
	}
}
