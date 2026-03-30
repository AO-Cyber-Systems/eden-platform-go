package entitlements

import (
	"sync"
	"time"
)

// Cache is a simple TTL-based in-memory cache. It is safe for concurrent use.
type Cache[K comparable, V any] struct {
	mu      sync.RWMutex
	entries map[K]cacheEntry[V]
	ttl     time.Duration
}

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// NewCache creates a new cache with the given TTL.
func NewCache[K comparable, V any](ttl time.Duration) *Cache[K, V] {
	return &Cache[K, V]{
		entries: make(map[K]cacheEntry[V]),
		ttl:     ttl,
	}
}

// Get returns the cached value and true if it exists and has not expired.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		var zero V
		return zero, false
	}
	return entry.value, true
}

// Set stores a value in the cache with the configured TTL.
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	c.entries[key] = cacheEntry[V]{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Invalidate removes a single key from the cache.
func (c *Cache[K, V]) Invalidate(key K) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	c.entries = make(map[K]cacheEntry[V])
	c.mu.Unlock()
}
