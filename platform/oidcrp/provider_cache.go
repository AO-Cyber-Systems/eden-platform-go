package oidcrp

import (
	"context"
	"fmt"
	"sync"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/sync/singleflight"
)

// ProviderCache caches *oidc.Provider discovery results, keyed by an
// opaque caller-supplied string (typically tenant_id+idp_id). Singleflight
// collapses concurrent Get calls for the same key into one underlying
// oidc.NewProvider call — this prevents discovery-endpoint thundering herd
// on cold start when many goroutines hit /federate/oidc/.../callback at once.
//
// Refresh contract: the cache stores values forever. Callers that hit a
// JWKS "kid not found" during ID-token verification MUST call Invalidate(key)
// to force the next Get to re-discover (which picks up rotated signing keys).
type ProviderCache struct {
	mu sync.Map // key string -> *oidc.Provider
	sf singleflight.Group
}

// NewProviderCache returns an empty ProviderCache.
func NewProviderCache() *ProviderCache {
	return &ProviderCache{}
}

// Get returns the *oidc.Provider for key, fetching via oidc.NewProvider(ctx,
// issuer) on cache miss. Concurrent misses for the same key are collapsed
// to one underlying discovery call.
func (c *ProviderCache) Get(ctx context.Context, key, issuer string) (*oidc.Provider, error) {
	if v, ok := c.mu.Load(key); ok {
		return v.(*oidc.Provider), nil
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// Re-check under singleflight in case another goroutine already
		// populated the cache while we were waiting our turn.
		if v, ok := c.mu.Load(key); ok {
			return v, nil
		}
		p, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			return nil, fmt.Errorf("oidcrp: discover %q: %w", issuer, err)
		}
		c.mu.Store(key, p)
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*oidc.Provider), nil
}

// Invalidate removes the cached provider for key. The next Get will
// re-fetch via oidc.NewProvider. Idempotent.
func (c *ProviderCache) Invalidate(key string) {
	c.mu.Delete(key)
}
