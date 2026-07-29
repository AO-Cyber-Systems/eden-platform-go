// Package rpauth is the relying-party side of AOID authentication: it turns an
// opaque bearer token into an authenticated Identity by calling AOID's OAuth 2.0
// token introspection endpoint (RFC 7662), with bounded caching.
//
// It exists so that "AOID is the source of truth for identity" is implemented
// once rather than per application. AODex is the first consumer; AOCore,
// eden-biz and the rest adopt the same package.
//
// WHY INTROSPECTION AND NOT LOCAL JWT VERIFICATION: a self-validated JWT cannot
// be revoked before it expires. Introspection asks the authority on every cache
// miss, so revoking a session in AOID actually ends it in every relying party.
// That property is the entire point, and it is why the cache TTL below is a
// deliberate, documented budget rather than an implementation detail.
package rpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInactive means the authority answered, definitively, that the token is
	// not valid — expired, revoked, or never issued. The caller must reject the
	// request.
	ErrInactive = errors.New("rpauth: token is not active")

	// ErrUnavailable means the authority could not be reached or returned an
	// unusable response. This is NOT an authentication failure, and callers must
	// not treat it as one: it is a dependency outage. Fail CLOSED for access
	// decisions, but distinguish it in telemetry or an AOID blip looks like a
	// spike in credential attacks.
	ErrUnavailable = errors.New("rpauth: introspection unavailable")
)

// DefaultMaxTTL bounds how long any introspection result is trusted.
//
// THIS VALUE IS THE REVOCATION LAG BUDGET. A token revoked in AOID keeps
// working at a relying party for at most this long. Lowering it costs an
// introspection round trip per token per window; raising it widens the window
// in which a revoked session still works. 60s is chosen to match AOEdge's
// existing API-key validator so the platform has one number, not two.
const DefaultMaxTTL = 60 * time.Second

// Identity is the authenticated principal a relying party sees.
//
// IT CARRIES ONLY WHAT AOID ACTUALLY SENDS. An earlier version of this struct
// also had AccountID, TenantID and Email, decoded from `account_id`,
// `tenant_id` and `email`. AOID emits none of those — the fields were written
// against an imagined contract, tested against a fake issuer, and would have
// been empty on every real token. They are gone rather than backfilled, for
// two reasons:
//
//   - SUBJECT IS THE RIGHT KEY, NOT ACCOUNT. `sub` is aoid.identities.id, the
//     GLOBAL identity. An account is per-TENANT: one person in two workspaces
//     has two accounts and one identity. A relying party keeps one local row
//     per PERSON, so the identity is the durable join key and the account is
//     the wrong axis to hang it on.
//   - EVERYTHING ELSE IS ALREADY LOCAL. A service that has a mirror row keyed
//     by subject already knows that user's email; re-deriving it from the
//     authority would have meant adding a database lookup to AOID's hottest
//     endpoint, whose response is cached and covered by a detached JWS at the
//     AOEdge boundary. Not worth it for a value the caller already holds.
//
// So this stays deliberately small: anything an application needs beyond it
// comes from that application's own mirror row, not smuggled through the token.
type Identity struct {
	// Subject is aoid.identities.id — the stable, global principal id and the
	// key a relying party should store on its mirror row.
	Subject string
	// TenantSlug is the ACTIVE tenant's slug. AOID sends it as `tnt`; despite
	// the name that field is the slug, not a UUID.
	TenantSlug string
	Scopes     []string
	ExpiresAt  time.Time
}

// introspectResponse is the subset of AOID's RFC 7662 response this package
// reads. Field names match what AOID actually emits (internal/oauth
// IntrospectResp) — notably `tnt`, which carries the tenant SLUG.
type introspectResponse struct {
	Active bool   `json:"active"`
	Sub    string `json:"sub"`
	Tnt    string `json:"tnt"`
	Scope  string `json:"scope"`
	Exp    int64  `json:"exp"`
	// TokenType distinguishes an access token from a refresh token. AOID sets
	// "Bearer" on the access-token path and "refresh_token" on the other; see
	// Validate for why that distinction is load-bearing here.
	TokenType string `json:"token_type"`
}

// Doer is the HTTP client seam. In production this is an mTLS client — AOID is
// mTLS-only, and AOEdge already authenticates to /oauth/introspect with a
// client certificate.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config configures an Introspector.
type Config struct {
	// Endpoint is the full introspection URL, e.g.
	// https://auth.aocyber.ai/oauth/introspect
	Endpoint string
	// Client performs the request. Required — it carries the mTLS identity.
	Client Doer
	// Cache holds positive results. Optional; nil disables caching, which means
	// an introspection call per request.
	Cache *Cache[Identity]
	// MaxTTL overrides DefaultMaxTTL. See the constant: this is the revocation
	// lag budget.
	MaxTTL time.Duration
	// NowFn is a test seam; nil means time.Now.
	NowFn func() time.Time
}

// Introspector validates opaque tokens against AOID.
type Introspector struct {
	endpoint string
	client   Doer
	cache    *Cache[Identity]
	maxTTL   time.Duration
	nowFn    func() time.Time
}

// NewIntrospector builds an Introspector. It returns an error rather than
// panicking on missing configuration so a service can fail its own boot check.
func NewIntrospector(cfg Config) (*Introspector, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("rpauth: Endpoint is required")
	}
	if cfg.Client == nil {
		return nil, errors.New("rpauth: Client is required (it carries the mTLS identity)")
	}
	maxTTL := cfg.MaxTTL
	if maxTTL <= 0 {
		maxTTL = DefaultMaxTTL
	}
	nowFn := cfg.NowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Introspector{
		endpoint: cfg.Endpoint,
		client:   cfg.Client,
		cache:    cfg.Cache,
		maxTTL:   maxTTL,
		nowFn:    nowFn,
	}, nil
}

// MaxTTL is the effective cache TTL this Introspector was built with — i.e. the
// revocation lag budget it is actually enforcing, after any Config override.
//
// It is exported so that code which has to CHOOSE A PERIOD (the revalidation
// hook for long-lived connections, chiefly) derives that period from this
// number instead of picking one independently. A component that picks its own
// interval silently widens the platform's revocation window; see
// revalidate.go.
func (i *Introspector) MaxTTL() time.Duration { return i.maxTTL }

// Validate resolves a raw bearer token to an Identity, serving a cached result
// when one is present and unexpired.
//
// Returns ErrInactive when the authority rejects the token and ErrUnavailable
// when it cannot be consulted. Callers MUST distinguish the two.
func (i *Introspector) Validate(ctx context.Context, rawToken string) (*Identity, error) {
	return i.validate(ctx, rawToken, true)
}

// ValidateUncached is Validate with the cache READ skipped: it always asks the
// authority, whatever is currently cached for rawToken.
//
// USE IT WHEN THE POINT OF THE CALL IS THE FRESHNESS, not the identity. A
// re-check that can be answered out of the cache proves only that the token was
// valid when the entry was written, which is exactly the question a revocation
// check is asking. The cache is still WRITTEN — a fresh positive result is a
// fresh positive result, and refreshing the entry cannot make any subsequent
// cache hit older than MaxTTL — and a definitive rejection still drops the
// entry, so a revocation seen here also ends that token's ordinary HTTP
// requests immediately instead of after the remaining TTL.
func (i *Introspector) ValidateUncached(ctx context.Context, rawToken string) (*Identity, error) {
	return i.validate(ctx, rawToken, false)
}

func (i *Introspector) validate(ctx context.Context, rawToken string, useCache bool) (*Identity, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrInactive
	}

	if useCache && i.cache != nil {
		if cached, ok := i.cache.Get(rawToken); ok {
			return &cached, nil
		}
	}

	form := url.Values{}
	form.Set("token", rawToken)
	form.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %v", ErrUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: authority returned HTTP %d", ErrUnavailable, resp.StatusCode)
	}

	var out introspectResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%w: unparseable response", ErrUnavailable)
	}

	// RFC 7662: an inactive token is a 200 with active=false, not an error status.
	if !out.Active {
		// Drop any cached positive. On the cached path this is a no-op (a hit
		// would have returned above), but on the UNCACHED path it is the
		// mechanism by which a revocation noticed by a long-lived connection
		// also ends that token's ordinary HTTP requests, instead of leaving
		// them to ride out the rest of the TTL.
		i.Invalidate(rawToken)
		return nil, ErrInactive
	}

	// REJECT REFRESH TOKENS. This is not defensive tidiness — AOID's two
	// introspection paths disagree about what `sub` means. On the access-token
	// path it is aoid.identities.id (the global identity); on the refresh-token
	// path it is aoid.accounts.id. Accepting a refresh token here would resolve
	// a VALID-LOOKING Identity whose Subject is an account id, and a relying
	// party would then join its mirror row on the wrong entity — silently, and
	// only for the subset of requests that presented the wrong credential type.
	//
	// A refresh token is also simply not a bearer credential: it is redeemable
	// at the token endpoint, not presentable as authorization. ErrInactive keeps
	// the caller's 401 mapping correct while the wrapped text keeps the real
	// reason legible in logs.
	if strings.EqualFold(out.TokenType, "refresh_token") {
		i.Invalidate(rawToken)
		return nil, fmt.Errorf("%w: refresh token presented as a bearer credential", ErrInactive)
	}

	identity := Identity{
		Subject: out.Sub,
		// AOID sends the tenant SLUG under `tnt`. Mapping it to TenantID would
		// put a slug in a UUID-shaped field; the name here matches the value.
		TenantSlug: out.Tnt,
		Scopes:     splitScopes(out.Scope),
	}
	if out.Exp > 0 {
		identity.ExpiresAt = time.Unix(out.Exp, 0).UTC()
	}

	if i.cache != nil {
		if ttl := i.computeTTL(resp.Header.Get("Cache-Control"), out.Exp); ttl > 0 {
			i.cache.Put(rawToken, identity, ttl)
		}
	}

	return &identity, nil
}

// Invalidate drops any cached result for rawToken, so the next Validate goes
// back to the authority. Used when a relying party is told out of band that a
// session ended.
func (i *Introspector) Invalidate(rawToken string) {
	if i.cache != nil {
		i.cache.Invalidate(rawToken)
	}
}

// computeTTL is the smallest of: the configured MaxTTL, any max-age the
// authority asked for, and the token's own remaining lifetime.
//
// Taking the minimum matters in both directions — the cache must never outlive
// the token (or a relying party would honour an expired credential), and it
// must never ignore a shorter max-age (which is how the authority signals
// "re-check me sooner than usual").
func (i *Introspector) computeTTL(cacheControl string, tokenExp int64) time.Duration {
	best := i.maxTTL

	if maxAge := parseMaxAge(cacheControl); maxAge > 0 && maxAge < best {
		best = maxAge
	}
	if tokenExp > 0 {
		if remaining := time.Unix(tokenExp, 0).Sub(i.nowFn()); remaining < best {
			best = remaining
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// parseMaxAge extracts max-age from a Cache-Control header, 0 when absent or malformed.
func parseMaxAge(cc string) time.Duration {
	if cc == "" {
		return 0
	}
	for _, part := range strings.Split(cc, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(strings.ToLower(part), "max-age=") {
			continue
		}
		n, err := strconv.Atoi(part[len("max-age="):])
		if err != nil || n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	return 0
}

// splitScopes splits the RFC 6749 space-delimited scope string.
func splitScopes(scope string) []string {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	return strings.Fields(scope)
}
