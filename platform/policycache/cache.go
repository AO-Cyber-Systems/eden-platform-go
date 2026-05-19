package policycache

import (
	"sync"
)

// Cache is a thread-safe, generic, in-memory map[K]V designed for per-tenant
// configuration snapshots.
//
// # Semantics
//
//   - Get is non-blocking and uses an RLock.
//   - Set / Invalidate / Replace serialize through a write Lock.
//   - Replace atomically swaps the entire snapshot under the write Lock; a
//     concurrent Get observes EITHER the pre-Replace or post-Replace map,
//     never a partial state.
//   - Replace defensively clones its input map; the caller may mutate the
//     map it passed in without affecting the cache.
//   - V is stored by value (no defensive deep-clone). Callers that need
//     mutation isolation MUST Clone the value before mutating.
//
// # Not an eviction cache
//
// Cache holds every entry until explicitly removed. There is no LRU, TTL,
// or capacity limit. Designed for ~10^4 entries (e.g. tenants × policy-rows
// per tenant). Do NOT use this for hot-path query result caching where the
// working set is unbounded.
type Cache[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// New constructs an empty Cache.
func New[K comparable, V any]() *Cache[K, V] {
	return &Cache[K, V]{m: make(map[K]V)}
}

// Get returns the value for k and a boolean indicating presence. The
// boolean is false (and V is the zero value) if k is absent.
func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.mu.RLock()
	v, ok := c.m[k]
	c.mu.RUnlock()
	return v, ok
}

// Set inserts or replaces the value for k.
func (c *Cache[K, V]) Set(k K, v V) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

// Invalidate removes k from the cache. A subsequent Get returns (zero, false).
// Invalidating a missing key is a no-op.
func (c *Cache[K, V]) Invalidate(k K) {
	c.mu.Lock()
	delete(c.m, k)
	c.mu.Unlock()
}

// Replace atomically swaps the entire cache contents to snapshot. The input
// map is defensively cloned; the caller may mutate it afterwards. Passing
// a nil snapshot clears the cache.
func (c *Cache[K, V]) Replace(snapshot map[K]V) {
	clone := make(map[K]V, len(snapshot))
	for k, v := range snapshot {
		clone[k] = v
	}
	c.mu.Lock()
	c.m = clone
	c.mu.Unlock()
}

// Len returns the number of entries currently in the cache.
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	n := len(c.m)
	c.mu.RUnlock()
	return n
}

// Keys returns a snapshot of the keys currently in the cache. The slice
// order is not stable across calls.
func (c *Cache[K, V]) Keys() []K {
	c.mu.RLock()
	keys := make([]K, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	return keys
}

// Snapshot returns a defensive copy of the entire cache contents. Useful
// for tests and rare bulk-export operations; not for hot paths.
func (c *Cache[K, V]) Snapshot() map[K]V {
	c.mu.RLock()
	out := make(map[K]V, len(c.m))
	for k, v := range c.m {
		out[k] = v
	}
	c.mu.RUnlock()
	return out
}
