// Package errortrack is a thin, opinionated wrapper over getsentry/sentry-go
// that:
//
//   - Treats an empty DSN as a no-op (CI-safe: no events shipped, no panic).
//   - Always installs a BeforeSend PII scrubber so JWTs/cookies/Stripe
//     signatures and request bodies cannot accidentally ship to Sentry.
//   - Provides an HTTP middleware that recovers and reports panics.
//   - Exposes a slog.Handler so existing slog.Error calls auto-promote to
//     Sentry events without per-call-site changes (sentryslog under the hood).
//
// This package is the canonical error-tracking transport for all eden-platform
// services. It does NOT replace logging — the slog handler is composed via
// NewMultiHandler so a JSON stdout handler keeps working alongside Sentry.
//
// Anti-pattern: do not put error-tracking logic in eden-platform-go/platform/
// aosentry — that package is the AO LLM gateway, NOT an error tracker.
package errortrack

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

// Config configures errortrack.Init. All fields except DSN are optional.
type Config struct {
	// DSN is the Sentry project DSN. An empty DSN puts errortrack in no-op
	// mode (Init returns a no-op flush, slog handler discards, HTTP
	// middleware passes through unchanged).
	DSN string

	// Environment tags every event (e.g. "production", "staging", "dev").
	Environment string

	// Release tags every event (typically the git SHA).
	Release string

	// SampleRate controls the fraction of error events sent. 0 = unset
	// (sentry-go default of 1.0). 1.0 = all events sent.
	SampleRate float64
}

// Init configures the global sentry hub and returns a flush function that
// callers should defer at boot:
//
//	flush, err := errortrack.Init(cfg)
//	if err != nil { slog.Error("errortrack init", "error", err); os.Exit(1) }
//	defer flush()
//
// An empty DSN is NOT an error — it puts the package in no-op mode (returns a
// no-op flush) so dev and CI environments work without a Sentry project.
//
// SendDefaultPII is hard-coded to false; the BeforeSend hook is hard-coded to
// the package's PII scrubber. Callers cannot opt out of the scrubber.
func Init(cfg Config) (func(), error) {
	if cfg.DSN == "" {
		slog.Info("errortrack: DSN empty, transport disabled (no-op mode)")
		return func() {}, nil
	}

	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		SampleRate:       sampleRate,
		SendDefaultPII:   false,
		BeforeSend:       BeforeSend,
		AttachStacktrace: true,
	})
	if err != nil {
		return nil, fmt.Errorf("errortrack: sentry.Init: %w", err)
	}

	return func() { sentry.Flush(2 * time.Second) }, nil
}

// CaptureException reports a non-nil error to Sentry, preferring the hub
// stored on ctx (set by sentryhttp middleware) and falling back to the global
// CurrentHub. A nil error is a no-op so call sites can pass through error
// returns without nil-checking.
func CaptureException(ctx context.Context, err error) {
	if err == nil {
		return
	}
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	if hub == nil {
		return
	}
	hub.CaptureException(err)
}

// Recover recovers from a panic in the calling goroutine and ships it to
// Sentry. Intended for use as `defer errortrack.Recover(ctx)` at the top of
// goroutine bodies (job handlers, etc.). A nil panic value is a no-op.
//
// Out of scope for the current TRD but exported for forthcoming worker wiring.
func Recover(ctx context.Context) {
	if r := recover(); r != nil {
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub()
		}
		if hub != nil {
			hub.Recover(r)
			hub.Flush(2 * time.Second)
		}
		// Re-panic so caller's own recover (if any) can observe.
		panic(r)
	}
}

// HTTPMiddleware wraps an http.Handler with sentryhttp's panic recovery and
// scope-per-request behavior.
//
// Insertion ordering in biz-api:
//
//	platformserver.LoggingMiddleware(  // outer: logs precede capture
//	    errortrack.HTTPMiddleware(     // recover + report
//	        basicAuthMw.Wrap(...),     // inner: unauth errors still reportable
//	    ),
//	)
//
// Repanic is true so the surrounding LoggingMiddleware (and net/http's own
// recovery if any) still observe the panic after Sentry has captured it.
func HTTPMiddleware(h http.Handler) http.Handler {
	return sentryhttp.New(sentryhttp.Options{
		Repanic:         true,
		WaitForDelivery: false,
		Timeout:         5 * time.Second,
	}).Handle(h)
}

// HTTPMiddlewareForHub is a test helper variant that binds the wrapper to a
// specific *sentry.Hub via request context (instead of CurrentHub). Production
// code should use HTTPMiddleware. Repanic is false in this variant so
// httptest.Recorder can observe the 500 response without test framework
// interference.
func HTTPMiddlewareForHub(hub *sentry.Hub, h http.Handler) http.Handler {
	wrapped := sentryhttp.New(sentryhttp.Options{
		Repanic:         false,
		WaitForDelivery: true,
		Timeout:         5 * time.Second,
	}).Handle(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := sentry.SetHubOnContext(r.Context(), hub)
		wrapped.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BeforeSend is the package's PII scrubber. Exported so tests can call it
// directly without booting a full sentry client. Stripped fields:
//
//   - Headers: Authorization, Cookie, Stripe-Signature, X-Api-Key
//     (case-sensitive; sentry-go canonicalizes header keys to Title-Case).
//   - Request.Data: cleared to "" (request bodies routinely contain JWTs,
//     emails, password hashes, Stripe payloads).
//   - User.Email and User.IPAddress: cleared to "" (PII).
//
// Other fields (User.ID, User.Username, query strings, breadcrumbs) are NOT
// stripped — those are retained for debugging. Callers that want to scrub
// query strings or breadcrumbs should layer additional scrubbers on top.
func BeforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}

	if event.Request != nil {
		if event.Request.Headers != nil {
			for _, key := range sensitiveHeaders {
				delete(event.Request.Headers, key)
			}
		}
		event.Request.Data = ""
	}

	event.User.Email = ""
	event.User.IPAddress = ""

	return event
}

// sensitiveHeaders enumerates header names whose values are stripped before
// any event ships to Sentry. sentry-go's request capture canonicalizes keys to
// Title-Case via http.Header.Set semantics, so we delete the canonical forms.
var sensitiveHeaders = []string{
	"Authorization",
	"Cookie",
	"Stripe-Signature",
	"X-Api-Key",
}
