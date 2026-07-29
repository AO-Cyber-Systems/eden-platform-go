package rpauth

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Cache is a bounded, TTL-expiring LRU keyed by a SECRET (a bearer token or API
// key). It is generic so a relying party can cache introspection results,
// API-key validations, or anything else keyed the same way.
//
// Keys are stored as hex(sha256(secret)) and never in the clear. A cache like
// this is a natural place for a credential to leak into a heap dump or a
// debugger; hashing means the map holds nothing replayable.
//
// Only POSITIVE results should ever be cached. Caching a rejection would let a
// transient outage pin a valid session into a denied state for the TTL, and
// caching an "inactive" verdict adds nothing — a revoked token is already the
// fast path.
type Cache[V any] struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
	maxSize int
	nowFn   func() time.Time
}

type cacheEntry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
}

// NewCache constructs a cache holding at most maxSize entries. nowFn is a test
// seam; nil means time.Now.
func NewCache[V any](maxSize int, nowFn func() time.Time) *Cache[V] {
	if maxSize <= 0 {
		maxSize = 1
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Cache[V]{
		entries: make(map[string]*list.Element, maxSize),
		lru:     list.New(),
		maxSize: maxSize,
		nowFn:   nowFn,
	}
}

// hashKey returns hex(sha256(secret)) so the raw credential never becomes a map key.
func hashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Get returns the cached value for secret when present and unexpired.
// An expired entry is evicted on read rather than left for a sweeper.
func (c *Cache[V]) Get(secret string) (V, bool) {
	var zero V
	key := hashKey(secret)

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	entry := el.Value.(*cacheEntry[V])
	if !c.nowFn().Before(entry.expiresAt) {
		c.lru.Remove(el)
		delete(c.entries, key)
		return zero, false
	}
	c.lru.MoveToFront(el)
	return entry.value, true
}

// Put stores value under secret for ttl. A non-positive ttl is a no-op: callers
// compute the TTL from token lifetime, and a zero result means "already expired,
// do not cache" rather than "cache forever".
func (c *Cache[V]) Put(secret string, value V, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	key := hashKey(secret)
	expiresAt := c.nowFn().Add(ttl)

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[key]; ok {
		entry := el.Value.(*cacheEntry[V])
		entry.value = value
		entry.expiresAt = expiresAt
		c.lru.MoveToFront(el)
		return
	}

	el := c.lru.PushFront(&cacheEntry[V]{key: key, value: value, expiresAt: expiresAt})
	c.entries[key] = el

	for c.lru.Len() > c.maxSize {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.lru.Remove(oldest)
		delete(c.entries, oldest.Value.(*cacheEntry[V]).key)
	}
}

// Invalidate drops the entry for secret. Used when a relying party learns of a
// revocation out of band and does not want to wait out the TTL.
func (c *Cache[V]) Invalidate(secret string) {
	key := hashKey(secret)

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[key]; ok {
		c.lru.Remove(el)
		delete(c.entries, key)
	}
}

// Len reports the number of entries currently held, expired ones included
// (they are evicted lazily on read).
func (c *Cache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
