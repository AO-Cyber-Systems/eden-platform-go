package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/errortrack"
)

// Config configures Setup. All fields are optional; sensible defaults are
// applied for empty values.
type Config struct {
	// ServiceName is attached to log lines as `service`.
	ServiceName string

	// Environment ("production", "staging", "dev"). Forwarded to errortrack.
	Environment string

	// Release (typically the git SHA). Forwarded to errortrack.
	Release string

	// SentryDSN configures the Sentry transport. Empty DSN puts errortrack in
	// no-op mode (CI-safe).
	SentryDSN string

	// LogLevel controls slog level: "debug" | "info" | "warn" | "error".
	// Empty defaults to LOG_LEVEL env or "info".
	LogLevel string

	// LogFormat controls slog format: "json" | "text". Empty defaults to
	// LOG_FORMAT env or "text".
	LogFormat string

	// SampleRate forwarded to errortrack (0 = unset = sentry-go default 1.0).
	SampleRate float64

	// OTLP optionally enables the OTLP/HTTP exporter pipeline (tracer +
	// meter, with logger to follow). Nil = OTLP disabled (existing behavior
	// preserved for AOSentry/AODex). SetupOTLP availability invariants apply:
	// empty Endpoint or init failure degrades silently to no-op providers.
	OTLP *OTLPConfig
}

// Shutdown is the cleanup function returned by Setup. Callers must `defer`
// the returned value at process boot to flush in-flight Sentry events.
type Shutdown func()

// Setup wires the canonical observability stack for a service:
//
//   - errortrack.Init for Sentry transport (PII scrubber on)
//   - slog default handler (JSON or text) composed with the Sentry slog handler
//     via errortrack.NewMultiHandler so logs land in BOTH places
//   - returns the errortrack flush as the Shutdown
//
// Call once at main(); defer the returned Shutdown:
//
//	shutdown, err := observability.Setup(observability.Config{
//	    ServiceName: "biz-api",
//	    Environment: os.Getenv("EDEN_ENV"),
//	    Release:     os.Getenv("GIT_SHA"),
//	    SentryDSN:   os.Getenv("SENTRY_DSN"),
//	})
//	if err != nil { log.Fatal(err) }
//	defer shutdown()
//
// An empty SentryDSN is supported (no-op transport) so dev and CI work
// without a Sentry project.
func Setup(cfg Config) (Shutdown, error) {
	flush, err := errortrack.Init(errortrack.Config{
		DSN:         cfg.SentryDSN,
		Environment: cfg.Environment,
		Release:     cfg.Release,
		SampleRate:  cfg.SampleRate,
	})
	if err != nil {
		return nil, fmt.Errorf("observability: errortrack init: %w", err)
	}

	level := cfg.LogLevel
	if level == "" {
		level = envOrDefault("LOG_LEVEL", "info")
	}
	format := cfg.LogFormat
	if format == "" {
		format = envOrDefault("LOG_FORMAT", "text")
	}

	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var base slog.Handler
	switch strings.ToLower(format) {
	case "json":
		base = slog.NewJSONHandler(os.Stdout, opts)
	default:
		base = slog.NewTextHandler(os.Stdout, opts)
	}

	// Service-name attribute on every log line for cross-service queries.
	if cfg.ServiceName != "" {
		base = base.WithAttrs([]slog.Attr{slog.String("service", cfg.ServiceName)})
	}

	// Compose with the Sentry slog handler so error-level logs auto-promote
	// into Sentry events. In no-op DSN mode, the Sentry handler still runs
	// but no events ship.
	combined := errortrack.NewMultiHandler(base, errortrack.SlogHandler())
	slog.SetDefault(slog.New(combined))

	// Optional OTLP chain. Never blocks boot — SetupOTLP returns nil error
	// in all degrade paths. We still log any unexpected non-nil error.
	var otlpShutdown OTLPShutdown = noopOTLPShutdown
	if cfg.OTLP != nil {
		res := BuildResource(cfg)
		sh, otlpErr := SetupOTLP(context.Background(), *cfg.OTLP, res)
		if otlpErr != nil {
			slog.Warn("observability: SetupOTLP returned unexpected error", "error", otlpErr)
		} else {
			otlpShutdown = sh
		}
	}

	return Shutdown(func() {
		otlpShutdown()
		flush()
	}), nil
}

// MustSetup is the panic-on-error variant for boot code that wants a
// one-liner. The returned Shutdown is still must-defer.
func MustSetup(cfg Config) Shutdown {
	shutdown, err := Setup(cfg)
	if err != nil {
		panic(fmt.Sprintf("observability: setup: %v", err))
	}
	return shutdown
}
