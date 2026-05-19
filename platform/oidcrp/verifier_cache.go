package oidcrp

import (
	"sync"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// VerifierCache caches *oidc.IDTokenVerifier keyed by an opaque caller-
// supplied string (typically tenant_id+idp_id). A verifier is pinned to
// a specific *oidc.Provider + clientID + supportedAlgs triple — callers
// must use a stable key per logical IdP binding.
//
// SkipIssuerCheck and SkipClientIDCheck are NEVER set by this package.
// supportedAlgs defaults to []string{oidc.RS256, oidc.ES256} when nil; this
// rejects ID tokens signed with "none" or HS256, both of which are valid
// per RFC 7515 but inappropriate for federated identity.
//
// Empty clientID panics — this is a developer error caught at boot, not a
// runtime condition that should be surfaced as an error.
type VerifierCache struct {
	mu sync.Map // key string -> *oidc.IDTokenVerifier
}

// NewVerifierCache returns an empty VerifierCache.
func NewVerifierCache() *VerifierCache {
	return &VerifierCache{}
}

// defaultSupportedAlgs is the conservative default set used when callers
// pass nil. RS256 + ES256 covers ~all OIDC providers in production while
// excluding HS256 (symmetric, inappropriate for federation) and "none".
var defaultSupportedAlgs = []string{oidc.RS256, oidc.ES256}

// Get returns the *oidc.IDTokenVerifier for key, building one via
// provider.VerifierContext-equivalent on cache miss. The (provider,
// clientID, supportedAlgs) triple is captured at first Get and reused for
// all subsequent calls with the same key — pass a stable key per logical
// IdP binding.
//
// Empty clientID panics.
func (c *VerifierCache) Get(key string, provider *oidc.Provider, clientID string, supportedAlgs []string) *oidc.IDTokenVerifier {
	if clientID == "" {
		panic("oidcrp: VerifierCache.Get: empty clientID (developer error — wire your client_id at boot)")
	}
	if v, ok := c.mu.Load(key); ok {
		return v.(*oidc.IDTokenVerifier)
	}
	algs := supportedAlgs
	if len(algs) == 0 {
		algs = defaultSupportedAlgs
	}
	cfg := &oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: algs,
		// SkipIssuerCheck / SkipClientIDCheck deliberately left false.
	}
	v := provider.Verifier(cfg)
	actual, _ := c.mu.LoadOrStore(key, v)
	return actual.(*oidc.IDTokenVerifier)
}

// Invalidate removes the cached verifier for key. Useful after rotating
// the underlying provider (typically alongside ProviderCache.Invalidate).
func (c *VerifierCache) Invalidate(key string) {
	c.mu.Delete(key)
}
