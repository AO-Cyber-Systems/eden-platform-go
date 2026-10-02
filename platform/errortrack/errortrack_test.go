package errortrack_test

import (
	"context"
	"errors"
	"io"
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

// Test 6: HTTPMiddleware recovers the panic and the transport receives an
// event. The response status is untouched by sentryhttp on recover (Repanic:
// false in this variant, so ServeHTTP just returns) -- httptest.Recorder's
// zero-value 200, since panicHandler never calls WriteHeader itself.
func TestErrortrack_HTTPMiddleware_recovers_panics_and_reports(t *testing.T) {
	hub, transport := newTestHub(t)

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("middleware-panic"))
	})

	wrapped := errortrack.HTTPMiddlewareForHub(hub, panicHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// We expect HTTPMiddlewareForHub to recover the panic without writing a
	// response itself (Repanic: false in this test variant -- sentryhttp
	// recovers and simply returns; it never writes a 500 on the handler's
	// behalf).
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

// serveAndRecover sends a GET /test request through wrapped, bound to hub via
// request context, and reports whatever value (if any) escaped ServeHTTP as
// a panic.
func serveAndRecover(t *testing.T, hub *sentry.Hub, wrapped http.Handler) (rr *httptest.ResponseRecorder, recovered interface{}) {
	t.Helper()
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := sentry.SetHubOnContext(req.Context(), hub)
	req = req.WithContext(ctx)

	func() {
		defer func() { recovered = recover() }()
		wrapped.ServeHTTP(rr, req)
	}()
	return rr, recovered
}

// Test (a): HTTPMiddleware must not report http.ErrAbortHandler -- net/http's
// own sentinel for a handler that deliberately aborted the response, which
// net/http itself does not log -- but the panic must still propagate so
// net/http (or an outer middleware) observes it exactly as if errortrack
// were not installed (Repanic: true, unchanged).
func TestErrortrack_HTTPMiddleware_ErrAbortHandler_no_event_but_repanics(t *testing.T) {
	hub, transport := newTestHub(t)

	abortHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	_, recovered := serveAndRecover(t, hub, errortrack.HTTPMiddleware(abortHandler))

	if recovered != http.ErrAbortHandler {
		t.Fatalf("panic did not propagate as http.ErrAbortHandler through HTTPMiddleware; got %v", recovered)
	}

	hub.Flush(time.Second)
	if events := transport.Events(); len(events) != 0 {
		t.Fatalf("expected 0 events for http.ErrAbortHandler panic, got %d: %+v", len(events), events)
	}
}

// Test (b): HTTPMiddleware's existing behavior for any other panic is
// unchanged -- it is reported and still re-panics -- AND, critically, the
// event's stack trace is untouched: the top (innermost) frame of the
// exception is the handler's own panic site, not anything from errortrack.
// This pins the regression a prior recover-and-repanic wrapper design
// introduced (recovering and re-panicking discards the original panic's
// stack and starts a new one at the repanic call site, corrupting Sentry's
// culprit/fingerprint for every existing issue).
func TestErrortrack_HTTPMiddleware_other_panic_preserves_stacktrace(t *testing.T) {
	hub, transport := newTestHub(t)

	otherHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("unexpected failure"))
	})

	_, recovered := serveAndRecover(t, hub, errortrack.HTTPMiddleware(otherHandler))
	if recovered == nil {
		t.Fatal("expected panic to propagate for a non-ErrAbortHandler panic (Repanic: true)")
	}

	hub.Flush(time.Second)
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event captured for non-ErrAbortHandler panic, got %d", len(events))
	}

	ev := events[0]
	if len(ev.Exception) == 0 {
		t.Fatal("captured event has no Exception entries")
	}
	exc := ev.Exception[len(ev.Exception)-1]
	if !strings.Contains(exc.Value, "unexpected failure") {
		t.Errorf("captured exception did not reference 'unexpected failure'; got %q", exc.Value)
	}
	if exc.Stacktrace == nil || len(exc.Stacktrace.Frames) == 0 {
		t.Fatal("captured exception has no stacktrace frames")
	}

	frames := exc.Stacktrace.Frames
	top := frames[len(frames)-1]
	if !strings.Contains(top.Module, "errortrack_test") {
		t.Errorf("expected the crash's top in-app frame to be the handler's own panic site (errortrack_test package), got module=%q function=%q", top.Module, top.Function)
	}
	for _, fr := range frames {
		if fr.Module == "github.com/aocybersystems/eden-platform-go/platform/errortrack" {
			t.Errorf("stacktrace contains an errortrack-package frame (function=%q); the panic's original stack must be untouched", fr.Function)
		}
	}
}

// Test (c): errortrack.Recover -- the non-HTTP (goroutine) crash-reporting
// path -- is untouched. A panic(http.ErrAbortHandler) there is a real crash
// (there is no HTTP response to abort, and no request attached to the
// event), and must still be reported.
func TestErrortrack_Recover_still_reports_ErrAbortHandler(t *testing.T) {
	hub, transport := newTestHub(t)
	ctx := sentry.SetHubOnContext(context.Background(), hub)

	func() {
		defer func() { recover() }()
		defer errortrack.Recover(ctx)
		panic(http.ErrAbortHandler)
	}()

	hub.Flush(time.Second)
	events := transport.Events()
	if len(events) == 0 {
		t.Fatal("expected errortrack.Recover to report a plain-goroutine http.ErrAbortHandler panic, got 0 events")
	}
}

// Test (d): an explicit errortrack.CaptureException(ctx, http.ErrAbortHandler)
// call is honored, not silently dropped -- BeforeSend only special-cases a
// *recovered* panic from an HTTP request, never an explicit capture.
func TestErrortrack_CaptureException_still_reports_ErrAbortHandler(t *testing.T) {
	hub, transport := newTestHub(t)
	ctx := sentry.SetHubOnContext(context.Background(), hub)

	errortrack.CaptureException(ctx, http.ErrAbortHandler)

	hub.Flush(time.Second)
	events := transport.Events()
	if len(events) == 0 {
		t.Fatal("expected errortrack.CaptureException(ctx, http.ErrAbortHandler) to report, got 0 events")
	}
}

// Test (e): end-to-end over a real net/http server. The handler flushes a
// partial body onto the wire -- so the client is guaranteed to receive
// headers and part of the body before the abort -- then aborts. The client
// must observe an incomplete/aborted response (io.ErrUnexpectedEOF reading
// the body, since the flush started chunked encoding without a known final
// length), never a clean one, and no event must be captured.
func TestErrortrack_HTTPMiddleware_real_server_aborts_connection_no_event(t *testing.T) {
	hub, transport := newTestHub(t)

	abortHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush() // put headers + partial body on the wire before the abort
		panic(http.ErrAbortHandler)
	})

	withHub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := sentry.SetHubOnContext(r.Context(), hub)
		errortrack.HTTPMiddleware(abortHandler).ServeHTTP(w, r.WithContext(ctx))
	})

	server := httptest.NewServer(withHub)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error from Get (headers were flushed before the abort): %v", err)
	}
	defer resp.Body.Close()

	_, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatal("expected an incomplete/aborted response body, got a complete one with no error")
	}
	if !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Errorf("expected io.ErrUnexpectedEOF (or a wrapping error) reading the aborted body, got %v", readErr)
	}

	hub.Flush(time.Second)
	if events := transport.Events(); len(events) != 0 {
		t.Fatalf("expected 0 events for http.ErrAbortHandler through a real server, got %d: %+v", len(events), events)
	}
}

// Test (f): HTTPMiddlewareForHub (Repanic: false) keeps its documented
// "never re-panic" behavior unchanged for both http.ErrAbortHandler and any
// other panic. For http.ErrAbortHandler specifically, ServeHTTP must still
// return normally with zero events (there is nothing left to re-throw, since
// this variant never re-panics in the first place). For any other panic, it
// keeps reporting exactly as before.
//
// Note this means HTTPMiddlewareForHub does NOT preserve real abort
// semantics: a genuine deliberate response abort would need to keep
// propagating to net/http to actually close the connection, but this
// variant swallows every panic unconditionally. That is fine here because
// HTTPMiddlewareForHub is test-only tooling (see its doc comment) -- it
// exists so tests can assert on captured events with a controlled hub, not
// to be used in a real request path. Production code uses HTTPMiddleware,
// which does preserve the abort (see test (a)).
func TestErrortrack_HTTPMiddlewareForHub_ErrAbortHandler_no_event_no_propagation(t *testing.T) {
	hub, transport := newTestHub(t)

	abortHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	_, recovered := serveAndRecover(t, hub, errortrack.HTTPMiddlewareForHub(hub, abortHandler))
	if recovered != nil {
		t.Fatalf("expected no propagation through HTTPMiddlewareForHub (Repanic: false, documented behavior); got %v", recovered)
	}

	hub.Flush(time.Second)
	if events := transport.Events(); len(events) != 0 {
		t.Fatalf("expected 0 events for http.ErrAbortHandler panic, got %d: %+v", len(events), events)
	}
}

func TestErrortrack_HTTPMiddlewareForHub_other_panic_still_reports(t *testing.T) {
	hub, transport := newTestHub(t)

	otherHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("unexpected failure"))
	})

	_, recovered := serveAndRecover(t, hub, errortrack.HTTPMiddlewareForHub(hub, otherHandler))
	if recovered != nil {
		t.Fatalf("expected no propagation through HTTPMiddlewareForHub (Repanic: false); got %v", recovered)
	}

	hub.Flush(time.Second)
	events := transport.Events()
	if len(events) == 0 {
		t.Fatal("expected >=1 event captured for non-ErrAbortHandler panic, got 0")
	}
}

// Test (g): nested HTTPMiddleware(HTTPMiddleware(h)) -- a plausible
// accidental double-wrap -- must still drop http.ErrAbortHandler at every
// layer and still let the panic propagate all the way out.
func TestErrortrack_HTTPMiddleware_nested_ErrAbortHandler_no_event_but_repanics(t *testing.T) {
	hub, transport := newTestHub(t)

	abortHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	nested := errortrack.HTTPMiddleware(errortrack.HTTPMiddleware(abortHandler))

	_, recovered := serveAndRecover(t, hub, nested)
	if recovered != http.ErrAbortHandler {
		t.Fatalf("panic did not propagate as http.ErrAbortHandler through nested HTTPMiddleware; got %v", recovered)
	}

	hub.Flush(time.Second)
	if events := transport.Events(); len(events) != 0 {
		t.Fatalf("expected 0 events for http.ErrAbortHandler panic through nested HTTPMiddleware, got %d: %+v", len(events), events)
	}
}

// recordingLogHandler captures every emitted slog.Record for assertion. It
// implements slog.Handler so it can be installed as the process-wide default
// logger for the duration of a single test.
type recordingLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordingLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingLogHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingLogHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

// Test: BeforeSend keeps the event (does not treat it as an HTTP-recovered
// abort) when hint.Context carries no sentry.RequestContextKey value at
// all -- e.g. a context that never passed through sentryhttp's recover
// path. Pins that the check looks for the specific key, not merely a
// non-nil Context.
func TestErrortrack_BeforeSend_keeps_event_when_context_has_no_request_key(t *testing.T) {
	event := &sentry.Event{Message: "panic recovered"}
	hint := &sentry.EventHint{
		RecoveredException: http.ErrAbortHandler,
		Context:            context.Background(),
	}

	out := errortrack.BeforeSend(event, hint)
	if out == nil {
		t.Fatal("BeforeSend dropped an event whose hint.Context carries no sentry.RequestContextKey; want it kept -- a non-nil Context alone must not be treated as sentryhttp's recover path")
	}
}

// Test: BeforeSend keeps the event when sentry.RequestContextKey is present
// but holds something other than a *http.Request -- e.g. a non-net/http
// integration (such as fasthttp, which stores a *fasthttp.RequestCtx under
// the same key) or a hand-built hint like this one. Pins that the type
// assertion, not just the key's presence, is what matters.
func TestErrortrack_BeforeSend_keeps_event_when_request_key_holds_wrong_type(t *testing.T) {
	event := &sentry.Event{Message: "panic recovered"}
	ctx := context.WithValue(context.Background(), sentry.RequestContextKey, "not a request")
	hint := &sentry.EventHint{
		RecoveredException: http.ErrAbortHandler,
		Context:            ctx,
	}

	out := errortrack.BeforeSend(event, hint)
	if out == nil {
		t.Fatal("BeforeSend dropped an event whose RequestContextKey value is not a *http.Request; want it kept")
	}
}

// Test: dropping an abort emits exactly one slog.Warn carrying only the
// request method and path -- never headers, the query string, or any other
// potentially sensitive request data -- so the drop is still observable
// operationally without reintroducing a PII leak.
func TestErrortrack_BeforeSend_warns_once_on_dropped_abort(t *testing.T) {
	rec := &recordingLogHandler{}
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(rec))
	defer slog.SetDefault(prevDefault)

	event := &sentry.Event{Message: "panic recovered"}
	req := httptest.NewRequest(http.MethodPost, "/widgets/42?token=leak-me", nil)
	ctx := context.WithValue(context.Background(), sentry.RequestContextKey, req)
	hint := &sentry.EventHint{RecoveredException: http.ErrAbortHandler, Context: ctx}

	out := errortrack.BeforeSend(event, hint)
	if out != nil {
		t.Fatalf("expected the event to be dropped, got %+v", out)
	}

	records := rec.Records()
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 log record for the dropped abort, got %d", len(records))
	}

	r := records[0]
	if r.Level != slog.LevelWarn {
		t.Errorf("expected LevelWarn, got %v", r.Level)
	}

	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	if attrs["method"] != http.MethodPost {
		t.Errorf("expected method attr %q, got %q", http.MethodPost, attrs["method"])
	}
	if attrs["path"] != "/widgets/42" {
		t.Errorf("expected path attr %q, got %q", "/widgets/42", attrs["path"])
	}
	for key, val := range attrs {
		if key != "method" && key != "path" {
			t.Errorf("unexpected log attr %q=%q; only method and path are allowed (no headers, query, or PII)", key, val)
		}
	}
	if strings.Contains(r.Message, "token") || strings.Contains(r.Message, "leak-me") {
		t.Errorf("log message unexpectedly contains query-string content: %q", r.Message)
	}
}
