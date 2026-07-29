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

// Identity is the authenticated principal a relying party sees. It is
// deliberately small: claims an application needs beyond this should be read
// from that application's own mirror row, not smuggled through the token.
type Identity struct {
	Subject    string
	AccountID  string
	TenantID   string
	TenantSlug string
	Email      string
	Scopes     []string
	ExpiresAt  time.Time
}

// introspectResponse is the RFC 7662 response plus AOID's extensions.
type introspectResponse struct {
	Active     bool   `json:"active"`
	Sub        string `json:"sub"`
	AccountID  string `json:"account_id"`
	TenantID   string `json:"tenant_id"`
	TenantSlug string `json:"tenant_slug"`
	Email      string `json:"email"`
	Scope      string `json:"scope"`
	Exp        int64  `json:"exp"`
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

// Validate resolves a raw bearer token to an Identity.
//
// Returns ErrInactive when the authority rejects the token and ErrUnavailable
// when it cannot be consulted. Callers MUST distinguish the two.
func (i *Introspector) Validate(ctx context.Context, rawToken string) (*Identity, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrInactive
	}

	if i.cache != nil {
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
		return nil, ErrInactive
	}

	identity := Identity{
		Subject:    out.Sub,
		AccountID:  out.AccountID,
		TenantID:   out.TenantID,
		TenantSlug: out.TenantSlug,
		Email:      out.Email,
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
