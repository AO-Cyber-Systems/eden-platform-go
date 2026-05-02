package errortrack

import (
	"context"
	"log/slog"

	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

// SlogHandler returns a slog.Handler that promotes ErrorLevel records to
// Sentry events via the package's currently-configured client (i.e. the global
// hub configured by Init). Compose with a JSON stdout handler via
// NewMultiHandler so structured logs continue to land in the regular log
// stream:
//
//	baseHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
//	slog.SetDefault(slog.New(errortrack.NewMultiHandler(baseHandler, errortrack.SlogHandler())))
//
// In no-op mode (empty DSN), the underlying sentry hub has no transport so
// the handler still runs but no events ship.
func SlogHandler() slog.Handler {
	return sentryslog.Option{
		EventLevel: []slog.Level{slog.LevelError},
		LogLevel:   []slog.Level{slog.LevelWarn, slog.LevelInfo, slog.LevelError},
	}.NewSentryHandler(context.Background())
}

// SlogHandlerForHub is a test helper variant that binds the slog handler to a
// specific *sentry.Hub instead of CurrentHub. Production code should use
// SlogHandler.
func SlogHandlerForHub(hub *sentry.Hub) slog.Handler {
	return sentryslog.Option{
		EventLevel: []slog.Level{slog.LevelError},
		LogLevel:   []slog.Level{slog.LevelWarn, slog.LevelInfo, slog.LevelError},
		Hub:        hub,
	}.NewSentryHandler(context.Background())
}

// MultiHandler tees slog records to multiple handlers. Used to compose a JSON
// stdout handler with the Sentry handler so logs land in BOTH places.
//
// Implementation notes:
//   - Enabled returns true if any child returns true (logical OR).
//   - Handle calls every child; the first non-nil error is returned but every
//     child still receives the record.
//   - WithAttrs/WithGroup propagate to every child.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler builds a MultiHandler over the given handlers. nil handlers
// are filtered out so callers can pass conditional handlers without
// nil-checking.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &MultiHandler{handlers: filtered}
}

// Enabled returns true if any child handler is enabled at the given level.
func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle dispatches the record to every child handler. If multiple children
// return errors, only the first is returned (others are still called).
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// WithAttrs returns a new MultiHandler with attrs propagated to every child.
func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: out}
}

// WithGroup returns a new MultiHandler with the group name propagated to every
// child.
func (m *MultiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: out}
}
