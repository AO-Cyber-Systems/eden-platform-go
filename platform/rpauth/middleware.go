package rpauth

// middleware.go — the HTTP seam over Introspector, plus the mapping from an
// AOID Identity to whatever principal the relying party actually works with.
//
// SHAPED AGAINST A REAL CONSUMER, NOT GUESSED AT. Every non-obvious decision
// below is answerable by pointing at AODex's existing middleware (its
// RequireAuth / OptionalAuth / CurrentUser), which is the first thing that has
// to adopt this:
//
//   - THE LIBRARY DOES NOT OWN THE PRINCIPAL. AODex's CurrentUser has eight
//     fields and only two of them (email, and the AOID subject) come from AOID
//     at all. The local row id, display name, admin flag, must-change-password
//     flag and API-key scopes are application data that AOID has never heard
//     of. A CurrentUser type living in this package would therefore be either
//     wrong for AODex or so wide it stops being about identity. So the library
//     validates the token and hands the app an Identity; the app supplies a
//     PrincipalLoader that turns that into its own type. This is also what the
//     Identity doc comment already promised: claims an application needs beyond
//     Identity come from that application's own mirror row.
//
//   - "NOT AUTHENTICATED" AND "CANNOT CHECK" ARE DIFFERENT ANSWERS. AODex today
//     returns 401 for every failure. Under D1 that is no longer acceptable: once
//     AOID is in the request path, an AOID outage would present as every user's
//     credentials suddenly being rejected — indistinguishable, in logs and in
//     dashboards, from a credential-stuffing wave, and it would bounce live
//     sessions to a login screen that also cannot work. Require answers 401 for
//     ErrInactive and 503 for ErrUnavailable. Both deny; only one blames the
//     user.
//
//   - OPTIONAL MUST NOT BLOCK, BUT MUST NOT LIE EITHER. AODex chains its own
//     API-key scheme after session auth, so this package cannot be the only
//     thing that decides a request is anonymous. Optional therefore always
//     continues, recording the OUTCOME (including an outage) in the context, and
//     RequireAuthenticated is a separate gate that reads it. That lets a service
//     compose: rpauth.Optional → its own API-key middleware →
//     rpauth.RequireAuthenticated, and still return 503 rather than 401 when the
//     reason nobody authenticated was that AOID was unreachable.
//
//   - PREFLIGHT PASSES THROUGH. Browsers send OPTIONS without credentials, so
//     authenticating a preflight always fails, and a 401 without CORS headers
//     blocks the real request. AODex has this guard with a comment explaining it
//     was learned the hard way; it belongs here so the next adopter never
//     relearns it.
//
//   - TOKEN EXTRACTION IS PLUGGABLE, AND THAT IS NOT DECORATION. Two live
//     requirements force it: AODex's /cable WebSocket upgrade comes from a
//     browser, which cannot set an Authorization header on a WebSocket, and
//     AODex's own API keys arrive as `Authorization: Bearer aodex_...`, which
//     must NOT be sent to AOID for introspection. An extractor that cannot
//     decline would introspect every API key on every request.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// TokenExtractor pulls the AOID access token out of a request, returning "" when
// the request carries none THAT THIS PACKAGE SHOULD HANDLE.
//
// Returning "" is a first-class answer, not a failure: a service with its own
// credential scheme uses it to say "this one is mine, do not introspect it".
type TokenExtractor func(*http.Request) string

// BearerToken is the default extractor: `Authorization: Bearer <token>`.
func BearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// CookieToken returns an extractor reading the token from a named cookie.
//
// This exists for WebSocket upgrades: a browser cannot set request headers on a
// WebSocket handshake, so a header-only extractor makes long-lived connections
// unauthenticatable. Prefer BearerToken everywhere a header is possible — a
// cookie is ambient authority and carries CSRF weight a header does not.
func CookieToken(name string) TokenExtractor {
	return func(r *http.Request) string {
		c, err := r.Cookie(name)
		if err != nil || c == nil {
			return ""
		}
		return strings.TrimSpace(c.Value)
	}
}

// FirstToken tries each extractor in order and returns the first non-empty
// result, so a service can accept a header normally and a cookie on the routes
// that have no choice.
func FirstToken(extractors ...TokenExtractor) TokenExtractor {
	return func(r *http.Request) string {
		for _, ex := range extractors {
			if tok := ex(r); tok != "" {
				return tok
			}
		}
		return ""
	}
}

// Excluding returns an extractor that declines any token matching skip.
//
// The motivating case: AODex issues its own API keys with a distinguishing
// prefix and sends them as `Authorization: Bearer <key>`. Without this, every
// API-key request would be introspected against AOID, which cannot know them —
// a round trip per request to be told "inactive", and an ErrInactive that
// masks the real, working credential scheme underneath.
func Excluding(inner TokenExtractor, skip func(string) bool) TokenExtractor {
	return func(r *http.Request) string {
		tok := inner(r)
		if tok == "" || skip(tok) {
			return ""
		}
		return tok
	}
}

// PrincipalLoader maps a validated AOID Identity onto the relying party's own
// principal — typically by reading its local mirror row keyed by
// Identity.Subject.
//
// Returning an error DENIES the request. That is the correct default: a
// principal that cannot be loaded is a user the service cannot make an
// authorization decision about. A loader that wants "authenticated but not
// provisioned here" to be a usable state should return a zero principal and no
// error, and let the handler decide.
type PrincipalLoader[P any] func(context.Context, *Identity) (P, error)

// Outcome is what an authentication attempt produced. It is stored in the
// request context by Optional and Require so a later gate can act on the
// REASON, not merely the absence, of a principal.
type Outcome int

const (
	// OutcomeAnonymous — no token was presented. Not an error.
	OutcomeAnonymous Outcome = iota
	// OutcomeAuthenticated — the token was valid and the principal loaded.
	OutcomeAuthenticated
	// OutcomeRejected — the authority answered that the token is not active
	// (expired, revoked, never issued). The user must re-authenticate.
	OutcomeRejected
	// OutcomeUnavailable — the authority could not be consulted. NOT an
	// authentication failure. Deny, but never blame the credential: this is the
	// state that must not be reported to users, dashboards or alerting as a
	// spike in bad logins.
	OutcomeUnavailable
	// OutcomePrincipalError — the token was valid but the relying party could
	// not load its own principal for it (mirror row missing, database down).
	// Distinct from Rejected because AOID said yes; the fault is local.
	OutcomePrincipalError
)

// String renders an Outcome for logs and metrics.
func (o Outcome) String() string {
	switch o {
	case OutcomeAnonymous:
		return "anonymous"
	case OutcomeAuthenticated:
		return "authenticated"
	case OutcomeRejected:
		return "rejected"
	case OutcomeUnavailable:
		return "unavailable"
	case OutcomePrincipalError:
		return "principal_error"
	default:
		return "unknown"
	}
}

// Result is the full record of an authentication attempt, kept in the request
// context. Principal is only meaningful when Outcome is OutcomeAuthenticated.
type Result[P any] struct {
	Outcome   Outcome
	Identity  *Identity
	Principal P
	Err       error
}

// resultKey is generic so two different principal types can coexist in one
// context without colliding — distinct instantiations are distinct types, and
// therefore distinct context keys.
type resultKey[P any] struct{}

// identityKey carries the Identity independently of the principal type, so
// code that only needs the AOID subject (audit, tracing, an entitlements
// lookup) does not have to know the service's principal type to read it.
type identityKey struct{}

// Authenticator turns AOID tokens into a relying party's own principal and
// exposes that as HTTP middleware.
type Authenticator[P any] struct {
	introspector *Introspector
	load         PrincipalLoader[P]
	extract      TokenExtractor
	writeErr     ErrorWriter
	logger       *slog.Logger
	// delayFn is a test seam for RevalidateLoop's inter-check delay (see
	// revalidate.go). Nil means the real jittered interval; there is no Option
	// for it on purpose, because the interval is derived, not chosen.
	delayFn func(time.Duration) time.Duration
}

// ErrorWriter renders a denial. It is configurable because adopting this
// package must not change a service's existing error wire format — AODex
// already answers `{"error":"Unauthorized"}` and its clients parse that.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, res Outcome)

// DefaultErrorWriter emits `{"error":"..."}` with the matching status, which is
// the shape AODex already returns.
//
// The BODY IS DELIBERATELY COARSE. A 401 says "Unauthorized" whether the token
// was expired, revoked or never issued — the status distinguishes what the
// client must DO, and nothing more granular is owed to an unauthenticated
// caller. The Outcome is available to the logger, where it belongs.
func DefaultErrorWriter(w http.ResponseWriter, _ *http.Request, status int, _ Outcome) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := "Unauthorized"
	if status == http.StatusServiceUnavailable {
		body = "Authentication temporarily unavailable"
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"error": body})
}

// Option configures an Authenticator.
type Option[P any] func(*Authenticator[P])

// WithExtractor overrides the default bearer-header token extractor.
func WithExtractor[P any](ex TokenExtractor) Option[P] {
	return func(a *Authenticator[P]) {
		if ex != nil {
			a.extract = ex
		}
	}
}

// WithErrorWriter overrides how denials are rendered.
func WithErrorWriter[P any](ew ErrorWriter) Option[P] {
	return func(a *Authenticator[P]) {
		if ew != nil {
			a.writeErr = ew
		}
	}
}

// WithLogger sets the logger. Defaults to slog.Default().
func WithLogger[P any](l *slog.Logger) Option[P] {
	return func(a *Authenticator[P]) {
		if l != nil {
			a.logger = l
		}
	}
}

// NewAuthenticator builds an Authenticator over an Introspector and the relying
// party's principal loader.
func NewAuthenticator[P any](i *Introspector, load PrincipalLoader[P], opts ...Option[P]) (*Authenticator[P], error) {
	if i == nil {
		return nil, errors.New("rpauth: Introspector is required")
	}
	if load == nil {
		return nil, errors.New("rpauth: PrincipalLoader is required")
	}
	a := &Authenticator[P]{
		introspector: i,
		load:         load,
		extract:      BearerToken,
		writeErr:     DefaultErrorWriter,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// authenticate runs the token → Identity → principal pipeline once.
func (a *Authenticator[P]) authenticate(r *http.Request) Result[P] {
	var zero P

	token := a.extract(r)
	if token == "" {
		return Result[P]{Outcome: OutcomeAnonymous, Principal: zero}
	}

	identity, err := a.introspector.Validate(r.Context(), token)
	switch {
	case errors.Is(err, ErrInactive):
		return Result[P]{Outcome: OutcomeRejected, Principal: zero, Err: err}
	case errors.Is(err, ErrUnavailable):
		// Logged at WARN, not at the level credential failures use: this is a
		// dependency outage and must be legible as one.
		a.logger.Warn("[rpauth] introspection unavailable — denying without blaming the credential",
			"error", err)
		return Result[P]{Outcome: OutcomeUnavailable, Principal: zero, Err: err}
	case err != nil:
		// An unclassified error is treated as an outage, not a rejection. Fail
		// closed either way, but never manufacture "your credential is bad"
		// from an error this package does not recognise.
		a.logger.Warn("[rpauth] unclassified introspection error — treating as unavailable",
			"error", err)
		return Result[P]{Outcome: OutcomeUnavailable, Principal: zero, Err: err}
	}

	principal, err := a.load(r.Context(), identity)
	if err != nil {
		a.logger.Warn("[rpauth] principal load failed for a valid token",
			"subject", identity.Subject,
			"tenant_slug", identity.TenantSlug,
			"error", err)
		return Result[P]{Outcome: OutcomePrincipalError, Identity: identity, Principal: zero, Err: err}
	}

	return Result[P]{Outcome: OutcomeAuthenticated, Identity: identity, Principal: principal}
}

// withResult stores a Result (and the bare Identity) in the request context.
func withResult[P any](ctx context.Context, res Result[P]) context.Context {
	ctx = context.WithValue(ctx, resultKey[P]{}, res)
	if res.Identity != nil {
		ctx = context.WithValue(ctx, identityKey{}, res.Identity)
	}
	return ctx
}

// Optional authenticates when a token is present and ALWAYS continues.
//
// It records the outcome in the context rather than swallowing it, so a service
// that chains its own credential scheme afterwards can still distinguish "nobody
// presented anything" from "AOID was down" when it finally decides. Use
// RequireAuthenticated as the closing gate on such a chain.
func (a *Authenticator[P]) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		res := a.authenticate(r)
		next.ServeHTTP(w, r.WithContext(withResult(r.Context(), res)))
	})
}

// Require authenticates and denies anything that is not authenticated.
//
// 401 for anonymous and rejected; 503 for an authority outage or a failed
// principal load. Both deny — the distinction is whether the caller is being
// told to re-authenticate (which will not help if the authority is down) or
// that the service is temporarily unable to answer.
func (a *Authenticator[P]) Require(next http.Handler) http.Handler {
	return a.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Optional forwards preflight WITHOUT authenticating, so this gate — not
		// just the outer wrapper — has to let it past. Without this, Require
		// would 401 every preflight even though Optional deliberately skipped it.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		res, ok := ResultFrom[P](r.Context())
		if !ok || res.Outcome != OutcomeAuthenticated {
			outcome := OutcomeAnonymous
			if ok {
				outcome = res.Outcome
			}
			a.writeErr(w, r, statusFor(outcome), outcome)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireAuthenticated is the closing gate for a composed chain: it enforces
// that SOMETHING authenticated the request, without itself doing any
// authenticating.
//
// This is the piece that lets AODex keep its own API-key scheme. Mount
// Optional, then the service's own middleware (which sets its own principal on
// success), then this. When nothing authenticated, the recorded rpauth Outcome
// still decides 401 vs 503, so an AOID outage does not get reported to users as
// a credential problem just because a later link in the chain also declined.
func RequireAuthenticated[P any](authenticated func(*http.Request) bool, writeErr ErrorWriter) func(http.Handler) http.Handler {
	if writeErr == nil {
		writeErr = DefaultErrorWriter
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if authenticated != nil && authenticated(r) {
				next.ServeHTTP(w, r)
				return
			}
			outcome := OutcomeAnonymous
			if res, ok := ResultFrom[P](r.Context()); ok {
				outcome = res.Outcome
			}
			writeErr(w, r, statusFor(outcome), outcome)
		})
	}
}

// statusFor maps an outcome to the HTTP status that denies it honestly.
func statusFor(o Outcome) int {
	switch o {
	case OutcomeUnavailable, OutcomePrincipalError:
		// 503, not 401. The credential was never shown to be bad — in the
		// PrincipalError case AOID explicitly said it was good — so telling the
		// client to re-authenticate would be both wrong and useless.
		return http.StatusServiceUnavailable
	default:
		return http.StatusUnauthorized
	}
}

// ResultFrom returns the full authentication Result recorded for this request.
func ResultFrom[P any](ctx context.Context) (Result[P], bool) {
	res, ok := ctx.Value(resultKey[P]{}).(Result[P])
	return res, ok
}

// PrincipalFrom returns the loaded principal, and false unless authentication
// actually succeeded. Behind Require this is always true.
func PrincipalFrom[P any](ctx context.Context) (P, bool) {
	var zero P
	res, ok := ctx.Value(resultKey[P]{}).(Result[P])
	if !ok || res.Outcome != OutcomeAuthenticated {
		return zero, false
	}
	return res.Principal, true
}

// MustPrincipal returns the principal or panics. Only use behind Require, which
// guarantees it is present — the panic is a wiring bug, not a runtime condition.
func MustPrincipal[P any](ctx context.Context) P {
	p, ok := PrincipalFrom[P](ctx)
	if !ok {
		panic("rpauth: MustPrincipal called without Require middleware")
	}
	return p
}

// IdentityFrom returns the validated AOID Identity, independently of the
// principal type. Audit, tracing and entitlement lookups need the AOID subject
// and should not have to name the service's principal type to get it.
func IdentityFrom(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(*Identity)
	return id, ok && id != nil
}
