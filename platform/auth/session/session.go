// Package session provides cookie-backed sessions persisted in PostgreSQL via
// the alexedwards/scs/v2 + pgxstore stack. It is the platform-side promotion
// of aodex-go/internal/auth/session.go and session_meta.go.
//
// The Manager type is a thin wrapper around *scs.SessionManager that adds
// helpers for storing/reading the authenticated user ID, the in-progress
// 2FA user ID, and per-session metadata (IP, user agent, last-active
// timestamp) so introspection endpoints can list and revoke sessions.
package session

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Default lifetime applied when Config.Lifetime is zero.
const DefaultLifetime = 24 * time.Hour

// Default session keys. Exported so consumers performing direct GetString
// against the underlying *scs.SessionManager can use them too.
const (
	KeyUserID         = "user_id"
	KeyTwoFactorUser  = "two_factor_user_id"
)

// Config configures Manager creation.
type Config struct {
	// Pool is required. The pgxstore session table will be created in this
	// database (default name "go_sessions" — set TableName to override).
	Pool *pgxpool.Pool

	// CookieName is the HTTP cookie name. Required.
	CookieName string

	// CookieDomain is set on Set-Cookie. May be empty.
	CookieDomain string

	// Production toggles strict cookie semantics: SameSite=None + Secure=true.
	// When false (development), cookies use SameSite=Lax + Secure=false so they
	// work over plain http://localhost.
	Production bool

	// Lifetime overrides the session lifetime. Defaults to DefaultLifetime.
	Lifetime time.Duration

	// TableName overrides the pgxstore table name. Defaults to "go_sessions".
	TableName string
}

// Manager wraps *scs.SessionManager with platform-friendly helpers.
type Manager struct {
	*scs.SessionManager
	pool *pgxpool.Pool
}

// New creates a Manager from a Config. Config.Pool and Config.CookieName are
// required; everything else has defaults.
func New(cfg Config) *Manager {
	tableName := cfg.TableName
	if tableName == "" {
		tableName = "go_sessions"
	}
	sm := newCookieManager(cfg)
	sm.Store = pgxstore.NewWithConfig(cfg.Pool, pgxstore.Config{TableName: tableName})
	return &Manager{SessionManager: sm, pool: cfg.Pool}
}

// NewWithStore creates a Manager with a caller-supplied scs.Store. Use this
// for tests (memstore) or for callers wanting an alternative backend.
// Manager.RecordMetadata and Manager.TouchActivity will be no-ops without a
// pool unless the caller also assigns one via SetPool.
func NewWithStore(cfg Config, store scs.Store) *Manager {
	sm := newCookieManager(cfg)
	sm.Store = store
	return &Manager{SessionManager: sm, pool: cfg.Pool}
}

// SetPool overrides the pgxpool used for metadata/activity updates. Callers
// using NewWithStore can attach a pool later if they want metadata persistence.
func (m *Manager) SetPool(pool *pgxpool.Pool) {
	m.pool = pool
}

func newCookieManager(cfg Config) *scs.SessionManager {
	lifetime := cfg.Lifetime
	if lifetime == 0 {
		lifetime = DefaultLifetime
	}
	sm := scs.New()
	sm.Lifetime = lifetime
	sm.Cookie.Name = cfg.CookieName
	sm.Cookie.Path = "/"
	sm.Cookie.HttpOnly = true
	if cfg.Production {
		sm.Cookie.Secure = true
		sm.Cookie.SameSite = http.SameSiteNoneMode
	} else {
		sm.Cookie.Secure = false
		sm.Cookie.SameSite = http.SameSiteLaxMode
	}
	if cfg.CookieDomain != "" {
		sm.Cookie.Domain = cfg.CookieDomain
	}
	return sm
}

// SetUserID stores the authenticated user's ID in the session.
func (m *Manager) SetUserID(ctx context.Context, userID string) {
	m.Put(ctx, KeyUserID, userID)
}

// GetUserID retrieves the authenticated user's ID from the session.
func (m *Manager) GetUserID(ctx context.Context) (string, bool) {
	v := m.GetString(ctx, KeyUserID)
	if v == "" {
		return "", false
	}
	return v, true
}

// SetTwoFactorUserID stores the pending 2FA user ID. Set after password
// verification but before 2FA confirmation.
func (m *Manager) SetTwoFactorUserID(ctx context.Context, userID string) {
	m.Put(ctx, KeyTwoFactorUser, userID)
}

// GetTwoFactorUserID retrieves the pending 2FA user ID.
func (m *Manager) GetTwoFactorUserID(ctx context.Context) (string, bool) {
	v := m.GetString(ctx, KeyTwoFactorUser)
	if v == "" {
		return "", false
	}
	return v, true
}

// Clear destroys the current session.
func (m *Manager) Clear(ctx context.Context) error {
	return m.Destroy(ctx)
}

// CurrentToken returns the scs session token for the current request, or
// the empty string if none is active.
func (m *Manager) CurrentToken(ctx context.Context) string {
	return m.Token(ctx)
}

// RecordMetadata persists per-session introspection metadata (user id, ip,
// user agent, last-active timestamp) onto the session row so introspection
// endpoints can list and revoke sessions.
//
// Best-effort: a failure here does NOT abort login. The session still works;
// it just won't appear in the introspection list until the next login.
//
// The session table must have user_id, ip, user_agent, last_active_at
// columns. Migration is the consumer's responsibility (existing AODex
// migration creates them on go_sessions). When the Manager has no pool
// (NewWithStore without SetPool), this is a no-op.
func (m *Manager) RecordMetadata(ctx context.Context, r *http.Request) error {
	if m.pool == nil {
		return nil
	}
	token, _, err := m.Commit(ctx)
	if err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	userID := m.GetString(ctx, KeyUserID)
	if userID == "" {
		return nil
	}
	_, err = m.pool.Exec(ctx,
		`UPDATE go_sessions
		   SET user_id = $1::uuid,
		       ip = $2,
		       user_agent = $3,
		       last_active_at = NOW()
		 WHERE token = $4`,
		userID, ClientIP(r), r.UserAgent(), token,
	)
	return err
}

// TouchActivity updates last_active_at for the current session token.
// Best-effort; silent on error. Does nothing when the Manager has no pool.
func (m *Manager) TouchActivity(ctx context.Context) {
	if m.pool == nil {
		return
	}
	token := m.Token(ctx)
	if token == "" {
		return
	}
	_, _ = m.pool.Exec(ctx, `UPDATE go_sessions SET last_active_at = NOW() WHERE token = $1`, token)
}

// ClientIP extracts the client IP from the request, preferring proxy
// headers. Falls back to RemoteAddr with port stripped.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.Index(xff, ","); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}
