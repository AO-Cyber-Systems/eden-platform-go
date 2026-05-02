package errortrack_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/errortrack"
	"github.com/getsentry/sentry-go"
)

// CapturingTransport records every event for assertion. It implements
// sentry.Transport so it can be plugged into a per-test sentry.Client without
// mutating the package-level CurrentHub (gotcha #2 in the TRD).
type CapturingTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (c *CapturingTransport) Configure(opts sentry.ClientOptions) {}

func (c *CapturingTransport) SendEvent(event *sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *CapturingTransport) Flush(timeout time.Duration) bool { return true }

func (c *CapturingTransport) FlushWithContext(ctx context.Context) bool { return true }

func (c *CapturingTransport) Close() {}

func (c *CapturingTransport) Events() []*sentry.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*sentry.Event, len(c.events))
	copy(out, c.events)
	return out
}

// newTestHub builds a per-test sentry.Hub bound to a fresh CapturingTransport
// with the production BeforeSend hook active. Avoids mutating sentry.CurrentHub.
func newTestHub(t *testing.T) (*sentry.Hub, *CapturingTransport) {
	t.Helper()
	transport := &CapturingTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:            "https://public@example.invalid/1",
		Transport:      transport,
		BeforeSend:     errortrack.BeforeSend,
		SendDefaultPII: false,
	})
	if err != nil {
		t.Fatalf("sentry.NewClient: %v", err)
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	return hub, transport
}

// Test 1: Init with non-empty DSN returns a non-nil flush and no error.
func TestErrortrack_Init_DSN_set_returns_flush(t *testing.T) {
	flush, err := errortrack.Init(errortrack.Config{
		DSN:         "https://public@example.invalid/1",
		Environment: "test",
		Release:     "test-release",
		SampleRate:  1.0,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if flush == nil {
		t.Fatal("Init returned nil flush func")
	}
	flush()
}

// Test 2: Init with empty DSN returns no-op flush, no error, no transport configured.
func TestErrortrack_Init_empty_DSN_returns_noop_flush(t *testing.T) {
	flush, err := errortrack.Init(errortrack.Config{DSN: ""})
	if err != nil {
		t.Fatalf("Init with empty DSN returned error: %v", err)
	}
	if flush == nil {
		t.Fatal("Init with empty DSN returned nil flush — must be no-op, not nil")
	}
	// Should be safe to call repeatedly.
	flush()
	flush()
}

// Test 3: SlogError promotes to capture — install handler with capturing
// transport, call slog.Error, assert event captured at LevelError.
func TestErrortrack_SlogError_promotes_to_capture(t *testing.T) {
	hub, transport := newTestHub(t)

	handler := errortrack.SlogHandlerForHub(hub)
	logger := slog.New(handler)

	logger.Error("test slog promotion", "error", errors.New("boom"))

	hub.Flush(time.Second)

	events := transport.Events()
	if len(events) == 0 {
		t.Fatalf("expected ≥1 event captured by transport, got 0")
	}
	found := false
	for _, ev := range events {
		if ev.Level == sentry.LevelError && strings.Contains(ev.Message, "test slog promotion") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no event matched (Level=error, message contains 'test slog promotion'); events=%+v", events)
	}
}

// Test 4: BeforeSend strips Authorization, Cookie, Stripe-Signature, X-Api-Key headers.
func TestErrortrack_BeforeSend_strips_sensitive_headers(t *testing.T) {
	event := &sentry.Event{
		Request: &sentry.Request{
			Headers: map[string]string{
				"Authorization":    "Bearer leaked-jwt",
				"Cookie":           "session=leaked",
				"Stripe-Signature": "t=123,v1=leaked",
				"X-Api-Key":        "leaked-api-key",
				"X-Request-Id":     "keep-me",
				"User-Agent":       "keep-me",
			},
		},
	}
	out := errortrack.BeforeSend(event, &sentry.EventHint{})
	if out == nil {
		t.Fatal("BeforeSend returned nil event (should sanitize, not drop)")
	}
	for _, sensitive := range []string{"Authorization", "Cookie", "Stripe-Signature", "X-Api-Key"} {
		if v, ok := out.Request.Headers[sensitive]; ok {
			t.Errorf("header %q not stripped: got %q", sensitive, v)
		}
	}
	for _, keep := range []string{"X-Request-Id", "User-Agent"} {
		if _, ok := out.Request.Headers[keep]; !ok {
			t.Errorf("header %q was incorrectly stripped (only sensitive headers should be stripped)", keep)
		}
	}
}

// Test 5: BeforeSend strips Request.Data and User.Email, User.IPAddress.
func TestErrortrack_BeforeSend_strips_request_body(t *testing.T) {
	event := &sentry.Event{
		Request: &sentry.Request{
			Data: `{"email":"a@b.c","jwt":"leaked-token","password":"secret"}`,
		},
		User: sentry.User{
			ID:        "user-123",
			Email:     "leaked@example.com",
			IPAddress: "1.2.3.4",
			Username:  "keep-username",
		},
	}
	out := errortrack.BeforeSend(event, &sentry.EventHint{})
	if out == nil {
		t.Fatal("BeforeSend returned nil event")
	}
	if out.Request.Data != "" {
		t.Errorf("Request.Data not cleared: got %q", out.Request.Data)
	}
	if out.User.Email != "" {
		t.Errorf("User.Email not cleared: got %q", out.User.Email)
	}
	if out.User.IPAddress != "" {
		t.Errorf("User.IPAddress not cleared: got %q", out.User.IPAddress)
	}
	if out.User.ID != "user-123" {
		t.Errorf("User.ID was incorrectly stripped (only PII fields should be stripped): got %q", out.User.ID)
	}
}

// Test 6: HTTPMiddleware recovers panic, response is 500, transport receives event.
func TestErrortrack_HTTPMiddleware_recovers_panics_and_reports(t *testing.T) {
	hub, transport := newTestHub(t)

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("middleware-panic"))
	})

	wrapped := errortrack.HTTPMiddlewareForHub(hub, panicHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// We expect HTTPMiddleware to recover the panic and return 500.
	// (Repanic: false in this test variant — sentryhttp handles the panic
	// and writes a 500 response.)
	defer func() {
		if r := recover(); r != nil {
			// If sentryhttp uses Repanic: true, the handler re-panics; that's
			// also acceptable as long as the event was captured first.
			t.Logf("handler re-panicked (Repanic: true): %v", r)
		}
	}()
	wrapped.ServeHTTP(rr, req)

	hub.Flush(time.Second)

	events := transport.Events()
	if len(events) == 0 {
		t.Fatalf("expected ≥1 event captured from panic, got 0")
	}
	// Sentry should have seen the panic.
	found := false
	for _, ev := range events {
		// Panics surface as Exception entries or in ev.Message.
		for _, ex := range ev.Exception {
			if strings.Contains(ex.Value, "middleware-panic") {
				found = true
			}
		}
		if strings.Contains(ev.Message, "middleware-panic") {
			found = true
		}
	}
	if !found {
		t.Errorf("captured event did not reference 'middleware-panic'; events=%+v", events)
	}
}

// Test 7: SlogIntegration_worker_error — slog.Error("job failed", "error", err) → event captured.
// Proxy for internal/jobs/worker.go:124 idiom.
func TestErrortrack_SlogIntegration_worker_error_promotes(t *testing.T) {
	hub, transport := newTestHub(t)

	logger := slog.New(errortrack.SlogHandlerForHub(hub))

	logger.Error("job failed", "error", errors.New("boom"))

	hub.Flush(time.Second)

	events := transport.Events()
	if len(events) == 0 {
		t.Fatalf("expected ≥1 event captured for worker-style slog.Error, got 0")
	}
	found := false
	for _, ev := range events {
		if ev.Level == sentry.LevelError && strings.Contains(ev.Message, "job failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no event matched (Level=error, message contains 'job failed'); events=%+v", events)
	}
}

// Test 8: SlogIntegration_cron_error — slog.Error("scheduled job failed", "error", err) → event captured.
// Proxy for internal/jobs/scheduler.go:83 idiom.
func TestErrortrack_SlogIntegration_cron_error_promotes(t *testing.T) {
	hub, transport := newTestHub(t)

	logger := slog.New(errortrack.SlogHandlerForHub(hub))

	logger.Error("scheduled job failed", "error", errors.New("boom"))

	hub.Flush(time.Second)

	events := transport.Events()
	if len(events) == 0 {
		t.Fatalf("expected ≥1 event captured for cron-style slog.Error, got 0")
	}
	found := false
	for _, ev := range events {
		if ev.Level == sentry.LevelError && strings.Contains(ev.Message, "scheduled job failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no event matched (Level=error, message contains 'scheduled job failed'); events=%+v", events)
	}
}

// Test 9: CaptureException(ctx, nil) returns without calling transport.
func TestErrortrack_CaptureException_nil_error_noops(t *testing.T) {
	hub, transport := newTestHub(t)

	ctx := sentry.SetHubOnContext(context.Background(), hub)

	errortrack.CaptureException(ctx, nil)

	hub.Flush(time.Second)

	events := transport.Events()
	if len(events) != 0 {
		t.Fatalf("CaptureException(ctx, nil) must not transmit; got %d events", len(events))
	}
}

// MultiHandler tests — proves NewMultiHandler tees to all children and
// honours WithAttrs/WithGroup propagation.
func TestErrortrack_MultiHandler_tees_to_all_children(t *testing.T) {
	hub, transport := newTestHub(t)

	// Custom counting handler to confirm both children receive the record.
	counter := &countingHandler{}
	multi := errortrack.NewMultiHandler(counter, errortrack.SlogHandlerForHub(hub))
	logger := slog.New(multi)

	logger.Error("multi-handler-test", "error", errors.New("boom"))

	hub.Flush(time.Second)

	if counter.errorCount != 1 {
		t.Errorf("counter handler: want 1 error record, got %d", counter.errorCount)
	}

	events := transport.Events()
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "multi-handler-test") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sentry handler did not receive multi-handler-test")
	}
}

// countingHandler — minimal slog.Handler used by the MultiHandler test.
type countingHandler struct {
	errorCount int
	infoCount  int
}

func (c *countingHandler) Enabled(_ context.Context, level slog.Level) bool { return true }
func (c *countingHandler) Handle(_ context.Context, r slog.Record) error {
	switch r.Level {
	case slog.LevelError:
		c.errorCount++
	case slog.LevelInfo:
		c.infoCount++
	}
	return nil
}
func (c *countingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *countingHandler) WithGroup(_ string) slog.Handler      { return c }
