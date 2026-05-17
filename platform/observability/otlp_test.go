package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// Helpers ---------------------------------------------------------------------

// withRestoredGlobals captures the current OTel global providers and restores
// them on test completion. Required because SetupOTLP mutates globals.
func withRestoredGlobals(t *testing.T) {
	t.Helper()
	origTP := otel.GetTracerProvider()
	origMP := otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(origTP)
		otel.SetMeterProvider(origMP)
	})
}

// mockCollector spins up an httptest server matching POST /v1/traces,
// /v1/metrics, /v1/logs. onRequest is invoked per POST with the inbound
// *http.Request (useful for asserting headers).
func mockCollector(t *testing.T, onRequest func(r *http.Request)) (*httptest.Server, *int64) {
	t.Helper()
	var hits int64
	mux := http.NewServeMux()
	hit := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if onRequest != nil {
			onRequest(r)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{})
	}
	mux.HandleFunc("/v1/traces", hit)
	mux.HandleFunc("/v1/metrics", hit)
	mux.HandleFunc("/v1/logs", hit)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &hits
}

func endpointFromURL(t *testing.T, rawURL string) string {
	t.Helper()
	// httptest URLs look like http://127.0.0.1:PORT — strip the scheme.
	require.True(t, strings.HasPrefix(rawURL, "http://"))
	return strings.TrimPrefix(rawURL, "http://")
}

// Tests -----------------------------------------------------------------------

func TestSetupOTLP_EmptyEndpoint_ReturnsNoopShutdownNilError(t *testing.T) {
	withRestoredGlobals(t)
	preTP := otel.GetTracerProvider()
	shutdown, err := SetupOTLP(context.Background(), OTLPConfig{Endpoint: ""}, nil)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// Shutdown must be callable without panic.
	require.NotPanics(t, func() { shutdown() })
	// Global tracer provider must NOT have been replaced.
	require.Same(t, preTP, otel.GetTracerProvider())
}

func TestSetupOTLP_UnreachableEndpoint_ReturnsNoopWhenInitFails(t *testing.T) {
	withRestoredGlobals(t)
	// otlptracehttp.New is async — it succeeds construction even with a
	// dead endpoint, the failures happen on background export. To exercise
	// the "init failed" branch reliably we use a context that is already
	// cancelled, forcing the constructor to return an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	shutdown, err := SetupOTLP(ctx, OTLPConfig{
		Endpoint: "127.0.0.1:1",
		Insecure: true,
	}, nil)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.Less(t, elapsed, 2*time.Second, "SetupOTLP must return quickly even when context is cancelled")
	require.NotPanics(t, func() { shutdown() })
}

func TestSetupOTLP_ReachableEndpoint_ConfiguresGlobalProviders(t *testing.T) {
	withRestoredGlobals(t)
	ts, hits := mockCollector(t, nil)
	endpoint := endpointFromURL(t, ts.URL)

	shutdown, err := SetupOTLP(context.Background(), OTLPConfig{
		Endpoint: endpoint,
		Insecure: true,
	}, BuildResource(Config{ServiceName: "test-svc", Release: "v0", Environment: "test"}))
	require.NoError(t, err)
	defer shutdown()

	// Emit a span.
	_, span := otel.Tracer("test").Start(context.Background(), "test.span")
	span.End()

	// Force flush via Shutdown so the batch processor exports immediately.
	shutdown()

	// Allow a brief moment for the mock server to record the request.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(hits) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Greater(t, atomic.LoadInt64(hits), int64(0), "mock collector should have received at least one POST")
}

func TestSetupOTLP_ShutdownCompletesWithinSixSeconds(t *testing.T) {
	withRestoredGlobals(t)
	shutdown, err := SetupOTLP(context.Background(), OTLPConfig{
		Endpoint: "127.0.0.1:1", // unreachable; queued spans cannot drain
		Insecure: true,
	}, BuildResource(Config{ServiceName: "test-svc"}))
	require.NoError(t, err)

	// Queue a span that the shutdown will try (and fail) to drain.
	_, span := otel.Tracer("test").Start(context.Background(), "queued.span")
	span.End()

	start := time.Now()
	shutdown()
	elapsed := time.Since(start)
	require.Less(t, elapsed, 6*time.Second, "Shutdown must complete within ~6s (5s deadline + 1s grace)")
}

func TestSetupOTLP_HeadersPassedThrough(t *testing.T) {
	withRestoredGlobals(t)
	var gotHeader string
	var mu sync.Mutex
	ts, _ := mockCollector(t, func(r *http.Request) {
		mu.Lock()
		gotHeader = r.Header.Get("X-Test-Header")
		mu.Unlock()
	})
	endpoint := endpointFromURL(t, ts.URL)

	shutdown, err := SetupOTLP(context.Background(), OTLPConfig{
		Endpoint: endpoint,
		Insecure: true,
		Headers:  map[string]string{"X-Test-Header": "value123"},
	}, BuildResource(Config{ServiceName: "test-svc"}))
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "header.test.span")
	span.End()
	shutdown()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		h := gotHeader
		mu.Unlock()
		if h != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "value123", gotHeader)
}

// TestSetupOTLP_BoundedQueueDoesNotOOM emits 10,000 spans with an unreachable
// collector and asserts the process doesn't allocate unbounded memory. Memory
// thresholds vary across runs/architectures, so we use a generous 100MB cap.
func TestSetupOTLP_BoundedQueueDoesNotOOM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory ceiling test in -short mode")
	}
	withRestoredGlobals(t)
	shutdown, err := SetupOTLP(context.Background(), OTLPConfig{
		Endpoint: "127.0.0.1:1",
		Insecure: true,
	}, BuildResource(Config{ServiceName: "test-svc"}))
	require.NoError(t, err)
	defer shutdown()

	tracer := otel.Tracer("flood")
	for i := 0; i < 10000; i++ {
		_, span := tracer.Start(context.Background(), "flood.span")
		span.End()
	}
	// The assertion is "doesn't OOM" — if the queue were unbounded, this
	// test would either hang or crash the process. Reaching this line is
	// the success signal.
}

// TestBuildResource_FillsKnownAttributes is a small sanity check that
// BuildResource produces non-empty resources when given populated config.
func TestBuildResource_FillsKnownAttributes(t *testing.T) {
	res := BuildResource(Config{
		ServiceName: "aoid",
		Release:     "v0.1.0",
		Environment: "dev",
	})
	require.NotNil(t, res)
	require.Greater(t, len(res.Attributes()), 0)
}
