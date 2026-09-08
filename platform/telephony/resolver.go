package telephony

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EntitlementChecker is the minimal slice of an entitlements service the
// resolver consults to gate providers behind a tier (e.g. SignalWire
// requiring a paid plan). Defined here — NOT imported from
// platform/entitlements — so this package never hard-depends on
// entitlements; a consumer wires a concrete checker only if it wants gating.
type EntitlementChecker interface {
	Allowed(ctx context.Context, companyID uuid.UUID, featureKey string) (bool, string, error)
}

// noEntitlementCheck is used when no checker is wired (dev/test). It always
// allows.
type noEntitlementCheck struct{}

func (noEntitlementCheck) Allowed(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "", nil
}

// PlatformDefaultStore supplies the platform-wide default telephony Config.
// It is the tier the resolver consults between a per-company row and the
// caller-supplied fallback Provider. A nil store means the tier is skipped,
// preserving pre-platform-default behavior.
//
// The interface is defined HERE (not imported from another package) so the
// resolver can read a platform default without an import cycle. Callers wire
// the concrete store as this interface via WithPlatformDefaults.
type PlatformDefaultStore interface {
	// TwilioPlatformDefault returns the active platform-default Twilio config,
	// or ErrTenantNotConfigured when none is configured.
	TwilioPlatformDefault(ctx context.Context) (Config, error)
}

// Resolver maps a company ID to a per-tenant Provider.
//
// Resolution order:
//  1. ConfigStore.Get(companyID) — if present, build via Registry.
//  2. ConfigStore returns ErrTenantNotConfigured — consult the optional
//     PlatformDefaultStore for a platform-wide default and build via Registry.
//  3. No platform default — fall back to the Provider supplied at
//     construction time. This is the FINAL tier and is never short-circuited
//     away.
//  4. No fallback wired — return NoopProvider so calls fail loud.
//
// Tier gating: when the resolved provider is SignalWire, the resolver checks
// the tenant is entitled via the EntitlementChecker. Returns
// ErrTierNotAllowed when not entitled.
type Resolver struct {
	store         ConfigStore
	registry      *Registry
	fallback      Provider
	ent           EntitlementChecker
	platformStore PlatformDefaultStore // optional; nil = platform-default tier skipped

	mu    sync.RWMutex
	cache map[uuid.UUID]cachedProvider
	ttl   time.Duration
	now   func() time.Time
}

type cachedProvider struct {
	provider Provider
	expires  time.Time
}

// NewResolver constructs a Resolver. fallback may be nil — NoopProvider is
// substituted in that case. ent may be nil — checks are skipped (dev mode).
func NewResolver(store ConfigStore, reg *Registry, fallback Provider, ent EntitlementChecker) *Resolver {
	if fallback == nil {
		fallback = NoopProvider{}
	}
	if ent == nil {
		ent = noEntitlementCheck{}
	}
	return &Resolver{
		store:    store,
		registry: reg,
		fallback: fallback,
		ent:      ent,
		cache:    map[uuid.UUID]cachedProvider{},
		ttl:      60 * time.Second,
		now:      time.Now,
	}
}

// SetTTL overrides the cache TTL. Used by tests; production callers should
// use the default.
func (r *Resolver) SetTTL(d time.Duration) {
	r.mu.Lock()
	r.ttl = d
	r.mu.Unlock()
}

// WithPlatformDefaults wires the optional platform-default tier. Passing nil
// (or never calling this) leaves the tier disabled, preserving
// pre-platform-default behavior.
func (r *Resolver) WithPlatformDefaults(store PlatformDefaultStore) *Resolver {
	r.mu.Lock()
	r.platformStore = store
	r.mu.Unlock()
	return r
}

// For returns the per-tenant Provider, building it on first use and caching
// it for SetTTL.
func (r *Resolver) For(ctx context.Context, companyID uuid.UUID) (Provider, error) {
	if cached, ok := r.lookup(companyID); ok {
		return cached, nil
	}

	cfg, err := r.store.Get(ctx, companyID)
	switch {
	case errors.Is(err, ErrTenantNotConfigured):
		// Tier 2: a platform-wide default sits between the missing company
		// row and the fallback. nil store skips this tier.
		if p, ok := r.platformDefault(ctx); ok {
			r.cacheStore(companyID, p)
			return p, nil
		}
		// Tier 3 (FINAL): caller-supplied fallback. Tier gating only applies
		// when the active provider is SignalWire; never short-circuit this
		// tier away.
		r.cacheStore(companyID, r.fallback)
		return r.fallback, nil
	case err != nil:
		return nil, fmt.Errorf("telephony resolve: %w", err)
	}

	// Tier gate: SignalWire requires the telephony_signalwire feature.
	if cfg.Provider == ProviderSignalWire {
		allowed, _, errEnt := r.ent.Allowed(ctx, companyID, "telephony_signalwire")
		if errEnt != nil {
			return nil, fmt.Errorf("telephony entitlement check: %w", errEnt)
		}
		if !allowed {
			return nil, ErrTierNotAllowed
		}
	}

	p, err := r.registry.For(cfg)
	if err != nil {
		return nil, fmt.Errorf("telephony build: %w", err)
	}
	r.cacheStore(companyID, p)
	return p, nil
}

// platformDefault consults the optional PlatformDefaultStore and builds a
// Provider from the platform-wide default Config. It returns (provider, true)
// on success and (nil, false) when no store is wired, no platform default is
// configured, or building the provider fails — in every false case the caller
// falls through to the fallback tier (which is never skipped).
func (r *Resolver) platformDefault(ctx context.Context) (Provider, bool) {
	r.mu.RLock()
	store := r.platformStore
	r.mu.RUnlock()
	if store == nil {
		return nil, false
	}
	cfg, err := store.TwilioPlatformDefault(ctx)
	if err != nil {
		// ErrTenantNotConfigured or any read error -> fall through to the
		// fallback tier.
		return nil, false
	}
	p, err := r.registry.For(cfg)
	if err != nil {
		return nil, false
	}
	return p, true
}

// Invalidate clears the cached entry for a company. Call after an admin
// upserts a new per-company config so subsequent resolution picks up the
// change.
func (r *Resolver) Invalidate(companyID uuid.UUID) {
	r.mu.Lock()
	delete(r.cache, companyID)
	r.mu.Unlock()
}

// InvalidateAll clears EVERY cached entry by replacing the cache with a fresh
// map under the lock. Call after a PLATFORM-default write: companies that
// resolve via the platform tier are cached under their own ids, so only a
// whole-cache flush guarantees they pick up the new default.
func (r *Resolver) InvalidateAll() {
	r.mu.Lock()
	r.cache = map[uuid.UUID]cachedProvider{}
	r.mu.Unlock()
}

// Fallback returns the configured fallback provider. Used by callers that
// need to short-circuit when there is no per-tenant config. Always non-nil.
func (r *Resolver) Fallback() Provider { return r.fallback }

func (r *Resolver) lookup(id uuid.UUID) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cache[id]
	if !ok {
		return nil, false
	}
	if r.now().After(c.expires) {
		return nil, false
	}
	return c.provider, true
}

func (r *Resolver) cacheStore(id uuid.UUID, p Provider) {
	r.mu.Lock()
	r.cache[id] = cachedProvider{provider: p, expires: r.now().Add(r.ttl)}
	r.mu.Unlock()
}
