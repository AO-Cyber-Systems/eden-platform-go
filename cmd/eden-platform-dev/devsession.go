//go:build dev

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/config"
)

// DevSessionEndpoint is the dev-only HTTP path a zero-interaction client hits
// to retrieve an already-authenticated access token — no login form, no
// typed credentials. It is mounted directly on the top-level mux (see
// main.go), sitting alongside "/up" and "/metrics" — NOT behind
// connect.WithInterceptors. That is deliberate, not a bypass: this endpoint
// plays the role of a login page (unauthenticated by design, because its
// entire job is handing out the FIRST token), while every actual API call
// a client then makes with that token still goes through the real,
// fully-enabled auth and RBAC interceptors.
const DevSessionEndpoint = "/dev/session"

// DefaultDevServerAddr is what guardServerAddr's error message recommends
// when SERVER_ADDR binds beyond loopback. Port 8091 is this workspace's
// fixed local-verification port — 8080 is permanently occupied by another
// app on the operator's machine and must never be suggested here.
const DefaultDevServerAddr = "127.0.0.1:8091"

// DevSession is the payload returned by GET /dev/session and logged at
// startup. It carries everything a client needs to call the platform API
// without ever seeing a login form.
type DevSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	CompanyID    string `json:"company_id"`
	Email        string `json:"email"`
}

// mintDevSession authenticates the seeded dev tenant user (see devseed.go)
// through auth.Service.Login — the EXACT production login path: real
// password verification against the seeded Argon2id hash, a real
// company-membership lookup, real role resolution, a genuine audit log
// entry, and a real access token signed by the SAME auth.JWTManager the
// rest of the server uses to validate every other request. This function
// authenticates a real, already-seeded user through the real service; it
// does not fabricate claims, does not call the JWTManager directly, and
// never touches an interceptor. The only thing it removes is the
// interactive step of a human typing DevTenantEmail/DevTenantPassword into
// a login form.
func mintDevSession(ctx context.Context, authService *auth.Service) (*DevSession, error) {
	resp, err := authService.Login(ctx, DevTenantEmail, DevTenantPassword)
	if err != nil {
		return nil, fmt.Errorf("log in seeded dev tenant user: %w", err)
	}
	return &DevSession{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		UserID:       resp.User.ID.String(),
		CompanyID:    DevTenantCompanyID.String(),
		Email:        resp.User.Email,
	}, nil
}

// registerDevSessionEndpoint mounts DevSessionEndpoint on mux, returning the
// pre-minted session as JSON. A client that wants zero interaction — not
// even reading a startup log line — can fetch its token with a single GET.
func registerDevSessionEndpoint(mux *http.ServeMux, session *DevSession) {
	mux.HandleFunc(DevSessionEndpoint, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(session); err != nil {
			slog.Error("dev session: encode response", "error", err)
		}
	})
}

// guardAgainstProductionConfig refuses to let this playground start against
// anything that looks like real production infrastructure. This binary
// auto-issues a real, valid access token for a hardcoded, publicly-known
// password (DevTenantPassword) the moment it boots — that is exactly the
// kind of behavior that must never run against a real database or be
// reachable beyond loopback. Both checks run unconditionally (regardless of
// which storage backend flag was passed): the shared config defaults
// (localhost DB, and whatever SERVER_ADDR resolves to) are what get
// evaluated, so an operator's ambient environment is checked the same way a
// deliberately-supplied one would be.
func guardAgainstProductionConfig(cfg *config.PlatformConfig) error {
	if err := guardDatabaseURL(cfg.DatabaseURL); err != nil {
		return err
	}
	return guardServerAddr(cfg.ServerAddr)
}

// guardDatabaseURL refuses a DATABASE_URL whose host isn't loopback. A
// hostless or unparseable URL (e.g. a driver-specific DSN with no host
// component) is treated as safe — there is nothing resembling a reachable
// remote host to refuse on.
func guardDatabaseURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	if !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("refusing to start dev playground: DATABASE_URL host %q is not loopback — this looks like a real database, and the dev playground auto-issues a real access token for a hardcoded password. Point DATABASE_URL at a local database or unset it", u.Hostname())
	}
	return nil
}

// guardServerAddr refuses a bind address that isn't explicitly loopback.
// This includes the common Go idiom of a bare ":PORT" (which binds every
// interface, not just loopback) — for this playground that is
// production-shaped, not merely unusual.
func guardServerAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("refusing to start dev playground: SERVER_ADDR %q binds beyond loopback — the dev playground auto-issues a real access token for a hardcoded password and must never be reachable outside this machine. Set SERVER_ADDR=%s", addr, DefaultDevServerAddr)
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// startDevSession is the single entry point main.go calls: it refuses to
// proceed against production-shaped config, then authenticates the seeded
// dev user through the real login path and mounts the dev-only token
// endpoint. A non-nil error is meant to be fatal — mirroring how main.go
// already treats other boot-time errors (e.g. pgstore backend failures).
func startDevSession(cfg *config.PlatformConfig, authService *auth.Service, mux *http.ServeMux) error {
	if err := guardAgainstProductionConfig(cfg); err != nil {
		return err
	}

	session, err := mintDevSession(context.Background(), authService)
	if err != nil {
		return fmt.Errorf("mint dev session: %w", err)
	}

	registerDevSessionEndpoint(mux, session)

	slog.Info("dev session ready — zero-interaction login, no credentials required",
		"email", session.Email,
		"user_id", session.UserID,
		"company_id", session.CompanyID,
		"endpoint", DevSessionEndpoint,
		"access_token", session.AccessToken,
	)
	return nil
}
