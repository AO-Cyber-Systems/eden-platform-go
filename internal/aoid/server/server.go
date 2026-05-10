// Package server hosts the aoid HTTP server lifecycle: build, listen,
// graceful shutdown.
//
// Responsibilities:
//   - Construct an http.ServeMux with /healthz, /readyz and (in later
//     TRDs) the OIDC well-known endpoints + platform service routes.
//   - Block on the supplied context; on cancellation, run a bounded
//     graceful shutdown.
//
// The server is intentionally a single struct without DI gymnastics —
// callers pass a *config.Config and the server wires it up. Tests
// construct a server with overridden fields and call Run directly.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/platform/observability"
	platformserver "github.com/aocybersystems/eden-platform-go/platform/server"
)

// Server bundles the runtime pieces. Addr() is filled in once the
// listener binds — useful for tests that pass ":0" and need to discover
// the actual port.
type Server struct {
	Config *config.Config
	Health *platformserver.HealthChecker

	ready    atomic.Bool
	listener net.Listener

	// extraRoutes is populated by later TRDs (discovery, jwks, platform
	// services). Each entry registers itself onto the mux when buildMux
	// runs. Keeping the hook here lets 29-01 ship without 29-02/29-03
	// dependencies.
	extraRoutes []func(*http.ServeMux)
}

// New builds a Server.
func New(cfg *config.Config) *Server {
	return &Server{
		Config: cfg,
		Health: platformserver.NewHealthChecker(),
	}
}

// AddRoutes registers a handler-installation callback. Used by later
// TRDs to mount the discovery, JWKS, and platform-service handlers
// without forcing 29-01 to import them.
func (s *Server) AddRoutes(register func(*http.ServeMux)) {
	s.extraRoutes = append(s.extraRoutes, register)
}

// Run starts the HTTP server on cfg.ListenAddr and blocks until ctx is
// cancelled, then performs a graceful shutdown bounded by
// cfg.ShutdownTimeout. The error returned reflects the listener failing
// or the context being cancelled (in which case nil is returned after
// successful shutdown).
func Run(ctx context.Context, cfg *config.Config) error {
	s := New(cfg)
	return s.Start(ctx, nil)
}

// Start is like Run but uses the supplied listener instead of dialing
// cfg.ListenAddr. Returning the listener address is the test seam —
// pass `net.Listen("tcp", "127.0.0.1:0")` to bind a free port.
func (s *Server) Start(ctx context.Context, ln net.Listener) error {
	return s.run(ctx, ln)
}

// Addr returns the bound listener address. Returns "" before the listener
// is up.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// IsReady reports whether the server has flipped to ready (post-bind,
// pre-shutdown).
func (s *Server) IsReady() bool {
	return s.ready.Load()
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("/healthz", s.Health.Handler())
	mux.HandleFunc("/readyz", s.readyHandler)

	for _, register := range s.extraRoutes {
		register(mux)
	}
	return mux
}

func (s *Server) readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{
		"ready":   s.ready.Load(),
		"version": s.Config.Release,
	}
	if s.ready.Load() {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) run(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", s.Config.ListenAddr)
		if err != nil {
			return fmt.Errorf("aoid: listen %q: %w", s.Config.ListenAddr, err)
		}
	}
	s.listener = ln

	srv := &http.Server{
		Handler:           s.buildMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	s.ready.Store(true)
	slog.Info("aoid started",
		"addr", ln.Addr().String(),
		"issuer", s.Config.Issuer,
		"environment", s.Config.Environment,
	)

	select {
	case err := <-errCh:
		s.ready.Store(false)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("aoid: serve: %w", err)
	case <-ctx.Done():
		s.ready.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.Config.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("aoid: shutdown error", "error", err)
			return fmt.Errorf("aoid: shutdown: %w", err)
		}
		<-errCh
		slog.Info("aoid stopped")
		return nil
	}
}

// MustSetupObservability wires platform/observability with aoid-flavored
// defaults. Returned shutdown should be deferred at main(). Lives on the
// server package so cmd/aoid/main.go stays a small file.
func MustSetupObservability(cfg *config.Config) observability.Shutdown {
	return observability.MustSetup(observability.Config{
		ServiceName: "aoid",
		Environment: cfg.Environment,
		Release:     cfg.Release,
		SentryDSN:   cfg.SentryDSN,
		LogLevel:    cfg.LogLevel,
		LogFormat:   cfg.LogFormat,
	})
}
