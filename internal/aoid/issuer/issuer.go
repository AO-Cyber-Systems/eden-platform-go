// Package issuer implements the AO ID OIDC authorization server: the
// /oauth2/authorize, /oauth2/token, and /oauth2/userinfo endpoints.
//
// The issuer composes platform/auth (for credential verification + JWT
// signing + refresh token storage) with internal/aoid/clients (the OIDC
// client registry) and an in-memory CodeStore.
//
// Lifecycle: callers construct a single Issuer at boot, register its
// HTTP handlers on the aoid mux, and let request handlers borrow shared
// state via closures. The Issuer is safe for concurrent use; all
// embedded stores own their own locks.
package issuer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/clients"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
)

// Config controls Issuer behavior. Fields are populated from
// internal/aoid/config.Config at boot time.
type Config struct {
	// Issuer is the canonical issuer URL — the iss claim of every JWT.
	Issuer string

	// AuthCodeTTL is how long an issued auth code stays valid before
	// /oauth2/token rejects it. RFC 6749 recommends "short", typically
	// 10 minutes; we use that.
	AuthCodeTTL time.Duration

	// SessionTTL is the lifetime of the AO ID login session cookie.
	SessionTTL time.Duration
}

// DefaultConfig returns sensible defaults. AuthCodeTTL=10m, SessionTTL=24h.
func DefaultConfig() Config {
	return Config{
		AuthCodeTTL: 10 * time.Minute,
		SessionTTL:  24 * time.Hour,
	}
}

// Issuer composes the dependencies needed to serve OIDC issuer
// endpoints. Each field is borrowed (not owned) — the caller composes
// them at boot.
type Issuer struct {
	Config Config

	Auth      *auth.Service
	JWT       *auth.JWTManager
	Clients   clients.Registry
	Codes     CodeStore
	Sessions  SessionStore
	UserStore UserResolver

	// SecureCookies controls whether the AO ID session cookie sets the
	// Secure flag. Defaults to true in production environments; set to
	// false for local dev / tests.
	SecureCookies bool
}

// UserResolver fetches a User by ID. Callers wire this directly to the
// platform auth store so the issuer doesn't need its own DB pool — the
// auth.Service exposes Login but not Lookup, so the issuer reads
// directly from the AuthStore for userinfo and ID-token claims.
type UserResolver interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (auth.User, error)
}

// New constructs an Issuer. UserStore + Auth + JWT + Clients are
// required; Codes + Sessions default to in-memory implementations if
// nil.
func New(cfg Config, authSvc *auth.Service, jwt *auth.JWTManager, reg clients.Registry, store UserResolver) *Issuer {
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 10 * time.Minute
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 24 * time.Hour
	}
	return &Issuer{
		Config:    cfg,
		Auth:      authSvc,
		JWT:       jwt,
		Clients:   reg,
		Codes:     NewMemoryCodeStore(),
		Sessions:  NewMemorySessionStore(),
		UserStore: store,
	}
}

// Mount registers all OIDC issuer routes onto mux. Call this from
// cmd/aoid/boot.go in place of the discovery.IssuerNotActive
// registrations.
func (i *Issuer) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/oauth2/authorize", i.AuthorizeHandler)
	mux.HandleFunc("/oauth2/token", i.TokenHandler)
	mux.HandleFunc("/oauth2/userinfo", i.UserinfoHandler)
}

// authorizeRequest is the parsed form of an /oauth2/authorize request.
// We capture every field both for GET (query) and POST (form) handling.
type authorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
}

func parseAuthorizeRequest(r *http.Request) authorizeRequest {
	get := func(k string) string {
		if v := r.URL.Query().Get(k); v != "" {
			return v
		}
		return r.FormValue(k)
	}
	return authorizeRequest{
		ResponseType:        get("response_type"),
		ClientID:            get("client_id"),
		RedirectURI:         get("redirect_uri"),
		Scope:               get("scope"),
		State:               get("state"),
		Nonce:               get("nonce"),
		CodeChallenge:       get("code_challenge"),
		CodeChallengeMethod: get("code_challenge_method"),
		Prompt:              get("prompt"),
	}
}

// AuthorizeHandler serves both GET and POST on /oauth2/authorize.
//
// GET — render the login form (or auto-issue code when session is
// already authenticated).
// POST — accept email+password, authenticate, redirect with code.
//
// Per RFC 6749, errors that come BEFORE we trust the redirect_uri (e.g.
// unknown client_id, mismatched redirect) MUST NOT redirect — they
// surface as 400s on the authorize page directly. Errors AFTER trust is
// established (bad scope, missing PKCE, etc.) redirect back with
// `error=...` query params.
func (i *Issuer) AuthorizeHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ar := parseAuthorizeRequest(r)
	ctx := r.Context()

	// 1. Validate client + redirect first — these never redirect.
	if ar.ClientID == "" {
		http.Error(w, "missing client_id", http.StatusBadRequest)
		return
	}
	client, err := i.Clients.Lookup(ctx, ar.ClientID)
	if err != nil {
		writeAuthorizeError(w, "invalid_client", "unknown client_id")
		return
	}
	if ar.RedirectURI == "" {
		writeAuthorizeError(w, "invalid_request", "missing redirect_uri")
		return
	}
	if err := i.Clients.ValidateRedirect(client, ar.RedirectURI); err != nil {
		writeAuthorizeError(w, "invalid_redirect_uri", "redirect_uri does not match registered URI")
		return
	}

	// 2. From here on, errors redirect back to the client with error params.
	if ar.ResponseType != "code" {
		redirectErr(w, r, ar.RedirectURI, "unsupported_response_type", "only response_type=code is supported", ar.State)
		return
	}
	if ar.CodeChallenge == "" || ar.CodeChallengeMethod != "S256" {
		redirectErr(w, r, ar.RedirectURI, "invalid_request", "code_challenge with method=S256 is required (PKCE)", ar.State)
		return
	}

	scopes := splitScopes(ar.Scope)
	if !containsString(scopes, "openid") {
		redirectErr(w, r, ar.RedirectURI, "invalid_scope", "scope must include openid", ar.State)
		return
	}
	for _, s := range scopes {
		if !client.HasScope(s) {
			redirectErr(w, r, ar.RedirectURI, "invalid_scope", "scope %q not allowed for this client: "+s, ar.State)
			return
		}
	}

	// 3. POST: process login submission.
	if r.Method == http.MethodPost && r.FormValue("aoid_login") == "1" {
		i.handleLoginPost(w, r, ar, client)
		return
	}

	// 4. GET / non-login POST: check existing session.
	userID := i.currentUser(r)
	if userID != "" {
		i.completeAuthorization(w, r, ar, userID)
		return
	}

	// 5. Render login form.
	i.renderLogin(w, r, ar, client, "")
}

// completeAuthorization issues an auth code and redirects to the
// client's redirect_uri. Called once the requesting user is known.
func (i *Issuer) completeAuthorization(w http.ResponseWriter, r *http.Request, ar authorizeRequest, userID string) {
	code, err := generateCode()
	if err != nil {
		slog.Error("aoid: generate code", "error", err)
		redirectErr(w, r, ar.RedirectURI, "server_error", "could not generate code", ar.State)
		return
	}
	now := time.Now()
	if err := i.Codes.Save(r.Context(), AuthCode{
		Code:                code,
		ClientID:            ar.ClientID,
		UserID:              userID,
		Scope:               splitScopes(ar.Scope),
		RedirectURI:         ar.RedirectURI,
		CodeChallenge:       ar.CodeChallenge,
		CodeChallengeMethod: ar.CodeChallengeMethod,
		Nonce:               ar.Nonce,
		ExpiresAt:           now.Add(i.Config.AuthCodeTTL),
		CreatedAt:           now,
	}); err != nil {
		slog.Error("aoid: save code", "error", err)
		redirectErr(w, r, ar.RedirectURI, "server_error", "could not persist code", ar.State)
		return
	}
	u, err := url.Parse(ar.RedirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if ar.State != "" {
		q.Set("state", ar.State)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// handleLoginPost authenticates the user via platform/auth.Service and
// either rerenders the form (on failure) or issues a code (on success).
func (i *Issuer) handleLoginPost(w http.ResponseWriter, r *http.Request, ar authorizeRequest, client *clients.Client) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if email == "" || password == "" {
		i.renderLogin(w, r, ar, client, "Enter both email and password.")
		return
	}
	resp, err := i.Auth.Login(r.Context(), email, password)
	if err != nil {
		i.renderLogin(w, r, ar, client, "Invalid email or password.")
		return
	}
	// Mint an AO ID session cookie so subsequent /authorize requests
	// from the same browser don't re-prompt.
	sid, err := i.Sessions.Create(r.Context(), resp.User.ID.String(), time.Now().Add(i.Config.SessionTTL))
	if err != nil {
		slog.Error("aoid: create session", "error", err)
		// Non-fatal: still issue the code.
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    sid,
			Path:     "/",
			HttpOnly: true,
			Secure:   i.SecureCookies,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(i.Config.SessionTTL),
		})
	}
	i.completeAuthorization(w, r, ar, resp.User.ID.String())
}

// currentUser returns the user id from the AO ID session cookie, or ""
// if no valid session.
func (i *Issuer) currentUser(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	uid, err := i.Sessions.Lookup(r.Context(), c.Value)
	if err != nil {
		return ""
	}
	return uid
}

// writeAuthorizeError renders a plain 400 page for errors that occur
// before we trust the redirect URI (per OIDC spec).
func writeAuthorizeError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, "OIDC error: %s\n%s\n", code, desc)
}

// redirectErr 302s back to the client's redirect_uri with error params.
// Used after redirect URI is validated.
func redirectErr(w http.ResponseWriter, r *http.Request, redirectURI, code, desc, state string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func splitScopes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := strings.Fields(s)
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// SessionCookieName is the AO ID login session cookie. Distinct from any
// downstream client's cookies so AO ID and AODex sessions never collide.
const SessionCookieName = "aoid_session"

// SessionStore is the persistence interface for AO ID login sessions.
type SessionStore interface {
	Create(ctx context.Context, userID string, expiresAt time.Time) (string, error)
	Lookup(ctx context.Context, sessionID string) (string, error)
	Destroy(ctx context.Context, sessionID string) error
}

// MemorySessionStore is a thread-safe in-memory SessionStore.
type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]memSession
}

type memSession struct {
	UserID    string
	ExpiresAt time.Time
}

// NewMemorySessionStore constructs an empty MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]memSession)}
}

// Create generates a new session id, stores it, returns the id.
func (s *MemorySessionStore) Create(_ context.Context, userID string, expiresAt time.Time) (string, error) {
	id, err := generateCode()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = memSession{UserID: userID, ExpiresAt: expiresAt}
	s.mu.Unlock()
	return id, nil
}

// Lookup returns the user id for sessionID. Returns ErrSessionNotFound
// if missing/expired.
func (s *MemorySessionStore) Lookup(_ context.Context, sessionID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return "", ErrSessionNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, sessionID)
		return "", ErrSessionNotFound
	}
	return sess.UserID, nil
}

// Destroy removes a session.
func (s *MemorySessionStore) Destroy(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

// ErrSessionNotFound is returned by SessionStore.Lookup when the
// session id is unknown or expired.
var ErrSessionNotFound = errors.New("session not found")
