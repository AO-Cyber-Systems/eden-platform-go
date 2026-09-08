package telephony

import (
	"fmt"
	"sync"
)

// ProviderFactory builds a Provider from a Config. Factories are pure — they
// don't touch shared state — so the Registry's mutex only guards the
// factories map itself.
type ProviderFactory func(Config) (Provider, error)

// Registry maps ProviderType -> ProviderFactory. A resolver looks up the
// factory for the resolved tenant config and invokes it to build a
// per-tenant Provider. Factories are typically registered at process
// start-up.
type Registry struct {
	mu        sync.RWMutex
	factories map[ProviderType]ProviderFactory
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[ProviderType]ProviderFactory{}}
}

// Register binds a factory to a provider type. Re-registering the same type
// overwrites the prior factory — useful for tests.
func (r *Registry) Register(t ProviderType, f ProviderFactory) {
	if f == nil {
		panic(fmt.Sprintf("telephony: nil factory for provider %q", t))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[t] = f
}

// For returns a Provider built from the given config. Returns
// ErrUnsupportedProvider when no factory is registered for c.Provider.
func (r *Registry) For(c Config) (Provider, error) {
	r.mu.RLock()
	f, ok := r.factories[c.Provider]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, c.Provider)
	}
	return f(c)
}

// Has reports whether a factory is registered for the given type.
func (r *Registry) Has(t ProviderType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[t]
	return ok
}
