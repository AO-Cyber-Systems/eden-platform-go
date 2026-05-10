// Package ratelimit provides a portable, multi-tier rate limiter for the
// Eden portfolio. It combines an in-process adaptive token bucket with an
// optional Redis-backed fixed-window counter for fleet-wide enforcement.
//
// Donor: aosentry/internal/llm/provider_limiter (production-tested with
// adaptive tuning from upstream rate-limit headers). See TRD 18-02.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrFleetLimit indicates the shared (Redis) counter has tripped. Callers
// surface this as HTTP 429.
var ErrFleetLimit = errors.New("ratelimit: fleet-wide limit exceeded")

// Tier describes a per-key rate-limit configuration.
type Tier struct {
	Name              string
	RequestsPerMinute int
	TokensPerMinute   int // 0 disables token-based limiting
}

// Decision is returned by Limiter.Check.
type Decision struct {
	Allow      bool
	RetryAfter time.Duration
	Limit      int
	Remaining  int
}

// Observation is metadata learned from upstream responses (e.g. provider
// rate-limit headers) that the limiter uses to adapt the local bucket.
type Observation struct {
	LimitRequests     int
	RemainingRequests int
	RetryAfter        time.Duration
	ResetRequests     time.Duration
}

// Limiter is the portable rate-limit contract.
type Limiter interface {
	// Check examines the bucket for key with the given tier and returns
	// whether to allow the request. It does not block — callers wanting
	// blocking semantics can spin on Decision.RetryAfter.
	Check(ctx context.Context, key string, tier Tier) (Decision, error)

	// Update adapts the bucket from observed upstream responses.
	Update(key string, obs Observation)

	// Snapshot returns point-in-time bucket state for admin visibility.
	Snapshot() []BucketSnapshot
}

// BucketSnapshot is a point-in-time view of one limiter bucket.
type BucketSnapshot struct {
	Key               string    `json:"key"`
	Tier              string    `json:"tier"`
	Tokens            float64   `json:"tokens"`
	MaxTokens         float64   `json:"max_tokens"`
	RefillRatePerSec  float64   `json:"refill_rate_per_sec"`
	ObservedLimit     int       `json:"observed_limit"`
	ObservedRemaining int       `json:"observed_remaining"`
	RetryAfter        time.Time `json:"retry_after,omitempty"`
	LastUpdated       time.Time `json:"last_updated"`
	FleetCount        int64     `json:"fleet_count,omitempty"`
	FleetWindowSecs   int64     `json:"fleet_window_secs,omitempty"`
	FleetBacked       bool      `json:"fleet_backed"`
}

// RedisClient is the minimal Redis surface required by the fleet-aware
// limiter. We deliberately do NOT import go-redis at the package boundary —
// callers adapt their preferred client to this interface (see
// `examples/redis_adapter.go.txt` in this package's README).
type RedisClient interface {
	Incr(ctx context.Context, key string) (int64, error)
	ExpireNX(ctx context.Context, key string, ttl time.Duration) error
	Get(ctx context.Context, key string) (int64, error)
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// memoryLimiter is the in-process adaptive token-bucket implementation,
// suitable for single-instance deployments and tests.
type memoryLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	redis   RedisClient // optional — nil for pure single-instance
	prefix  string
}

// NewMemory constructs an in-process limiter (no Redis).
func NewMemory() Limiter {
	return &memoryLimiter{buckets: make(map[string]*bucket), prefix: "platform:rate:"}
}

// NewRedis constructs a fleet-aware limiter. The redis arg may be nil, in
// which case it falls back to in-memory only. prefix is prepended to every
// fleet counter key (e.g. "aosentry:rate:").
func NewRedis(client RedisClient, prefix string) Limiter {
	if prefix == "" {
		prefix = "platform:rate:"
	}
	return &memoryLimiter{
		buckets: make(map[string]*bucket),
		redis:   client,
		prefix:  prefix,
	}
}

func (m *memoryLimiter) Check(ctx context.Context, key string, tier Tier) (Decision, error) {
	b := m.getOrCreate(key, tier)

	// Fleet-wide pre-check (best-effort). On Redis error we fall through to local.
	if m.redis != nil && tier.RequestsPerMinute > 0 {
		count, err := m.incrWindowCounter(ctx, key)
		if err == nil && count > int64(tier.RequestsPerMinute) {
			ttl, _ := m.redis.TTL(ctx, m.fleetKey(key))
			return Decision{Allow: false, RetryAfter: ttl, Limit: tier.RequestsPerMinute, Remaining: 0}, ErrFleetLimit
		}
	}

	allow, retryAfter, remaining := b.tryConsume()
	return Decision{
		Allow:      allow,
		RetryAfter: retryAfter,
		Limit:      tier.RequestsPerMinute,
		Remaining:  remaining,
	}, nil
}

func (m *memoryLimiter) Update(key string, obs Observation) {
	m.mu.RLock()
	b := m.buckets[key]
	m.mu.RUnlock()
	if b == nil {
		return
	}
	b.update(obs)
}

func (m *memoryLimiter) Snapshot() []BucketSnapshot {
	m.mu.RLock()
	keys := make([]string, 0, len(m.buckets))
	bs := make([]*bucket, 0, len(m.buckets))
	for k, b := range m.buckets {
		keys = append(keys, k)
		bs = append(bs, b)
	}
	redis := m.redis
	m.mu.RUnlock()

	out := make([]BucketSnapshot, 0, len(keys))
	for i, b := range bs {
		s := b.snapshot(keys[i])
		if redis != nil {
			s.FleetBacked = true
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			if v, err := redis.Get(ctx, m.fleetKey(keys[i])); err == nil {
				s.FleetCount = v
			}
			if ttl, err := redis.TTL(ctx, m.fleetKey(keys[i])); err == nil && ttl > 0 {
				s.FleetWindowSecs = int64(ttl.Seconds())
			}
			cancel()
		}
		out = append(out, s)
	}
	return out
}

func (m *memoryLimiter) getOrCreate(key string, tier Tier) *bucket {
	m.mu.RLock()
	if b, ok := m.buckets[key]; ok {
		m.mu.RUnlock()
		return b
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.buckets[key]; ok {
		return b
	}
	rpm := tier.RequestsPerMinute
	if rpm <= 0 {
		rpm = 60
	}
	refill := float64(rpm) / 60.0
	// Start with 1 token of headroom so very tight tiers (e.g. 1 RPM) can
	// admit the first request; this matches the donor's intent that a fresh
	// bucket is "available, not pre-exhausted".
	startTokens := 1.0
	if startTokens > float64(rpm) {
		startTokens = float64(rpm)
	}
	b := &bucket{
		tierName:    tier.Name,
		tokens:      startTokens,
		maxTokens:   float64(rpm),
		refillRate:  refill,
		lastRefill:  time.Now(),
		lastUpdated: time.Now(),
	}
	m.buckets[key] = b
	return b
}

func (m *memoryLimiter) incrWindowCounter(ctx context.Context, key string) (int64, error) {
	opCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	count, err := m.redis.Incr(opCtx, m.fleetKey(key))
	if err != nil {
		return 0, err
	}
	// Set TTL only on first INCR of the window.
	_ = m.redis.ExpireNX(opCtx, m.fleetKey(key), 60*time.Second)
	return count, nil
}

func (m *memoryLimiter) fleetKey(key string) string { return m.prefix + key }

// bucket is the adaptive token bucket.
type bucket struct {
	mu sync.Mutex

	tierName string

	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time

	observedLimit     int
	observedRemaining int
	retryAfter        time.Time
	lastUpdated       time.Time
}

// tryConsume attempts to take one token. Returns (allow, retryAfter, remaining).
func (b *bucket) tryConsume() (bool, time.Duration, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()

	// Hard block from upstream Retry-After.
	if !b.retryAfter.IsZero() && time.Now().Before(b.retryAfter) {
		return false, time.Until(b.retryAfter), int(b.tokens)
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0, int(b.tokens)
	}
	// Wait for next token.
	wait := time.Second
	if b.refillRate > 0 {
		wait = time.Duration(float64(time.Second) / b.refillRate)
	}
	return false, wait, 0
}

func (b *bucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

func (b *bucket) update(o Observation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastUpdated = time.Now()

	if o.RetryAfter > 0 {
		b.retryAfter = time.Now().Add(o.RetryAfter)
	}

	if o.LimitRequests > 0 && o.LimitRequests != b.observedLimit {
		b.observedLimit = o.LimitRequests
		b.refillRate = float64(o.LimitRequests) / 60.0
		b.maxTokens = float64(o.LimitRequests)
	}

	if o.RemainingRequests >= 0 && o.LimitRequests > 0 {
		b.observedRemaining = o.RemainingRequests
		b.tokens = float64(o.RemainingRequests)
	}

	if o.ResetRequests > 0 && o.LimitRequests > 0 {
		needed := float64(o.LimitRequests - o.RemainingRequests)
		if needed > 0 && o.ResetRequests.Seconds() > 0 {
			b.refillRate = needed / o.ResetRequests.Seconds()
		}
	}
}

func (b *bucket) snapshot(key string) BucketSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return BucketSnapshot{
		Key:               key,
		Tier:              b.tierName,
		Tokens:            b.tokens,
		MaxTokens:         b.maxTokens,
		RefillRatePerSec:  b.refillRate,
		ObservedLimit:     b.observedLimit,
		ObservedRemaining: b.observedRemaining,
		RetryAfter:        b.retryAfter,
		LastUpdated:       b.lastUpdated,
	}
}

// BucketKey computes a stable key from segments. Use it to compose
// per-tenant + per-endpoint keys (e.g. BucketKey("tenant", "abc", "endpoint", "/v1/chat")).
func BucketKey(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ":" + p
	}
	return out
}

// MustParseRetryAfter parses an HTTP "Retry-After" header value (either
// integer seconds or RFC1123 date) into a Duration. Best-effort — returns 0
// when neither parse succeeds.
func MustParseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	var secs int64
	if _, err := fmt.Sscanf(header, "%d", &secs); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
