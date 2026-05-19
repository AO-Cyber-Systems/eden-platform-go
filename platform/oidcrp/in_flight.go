package oidcrp

import (
	"context"
	"sync"
	"time"
)

// InFlightPayload carries the per-request state an OIDC RP needs to resume
// a flow at the /callback step: the PKCE verifier (so the token exchange
// can prove possession), the stored nonce (so ExchangeAndVerify can bind
// the ID token), the tenant/idp pair (so the right verifier is loaded),
// and the post-login redirect target (so the user lands where they meant
// to go).
//
// Records are addressed by the OIDC `state` parameter's nonce field rather
// than the OAuth `code` — the code only appears at the callback, while
// nonce is generated at /start and embedded in the signed state, giving us
// a stable key from initiation through callback.
type InFlightPayload struct {
	PKCEVerifier string
	StoredNonce  string
	TenantID     string
	IdpID        string
	ReturnURL    string
	CreatedAt    time.Time
}

// InFlightStore persists InFlightPayload records during an in-progress
// OIDC authorization-code flow. Single-use Pop semantics: a record can be
// read exactly once; second Pop returns ErrInFlightNotFound regardless of
// whether the first Pop succeeded or hit ErrInFlightExpired.
//
// Concurrency: implementations MUST be safe for concurrent Put/Pop from
// many goroutines.
//
// Backend choice:
//   - InMemoryInFlightStore (this file) — single-process, durable across
//     no restarts. Fine for dev + single-replica deployments.
//   - PostgresInFlightStore — lives in AOID's internal/federation package
//     (TRD 06-05), needed for multi-replica deployments where the start
//     and callback land on different pods.
type InFlightStore interface {
	// Put writes payload under key with a TTL. Returns nil on success; an
	// error only on backend failure (memory store never errors).
	Put(ctx context.Context, key string, payload InFlightPayload, ttl time.Duration) error

	// Pop reads and deletes the record under key. Returns:
	//   - (payload, nil) on success.
	//   - (zero, ErrInFlightNotFound) if no record exists OR a prior Pop
	//     consumed it.
	//   - (zero, ErrInFlightExpired) if the record exists but TTL passed;
	//     the record IS deleted as a side effect.
	Pop(ctx context.Context, key string) (InFlightPayload, error)
}

// inFlightEntry is the wire-level value stored in InMemoryInFlightStore.
type inFlightEntry struct {
	Payload  InFlightPayload
	ExpireAt time.Time
}

// InMemoryInFlightStore is a single-process map+mutex implementation. TTLs
// are lazy: an expired record is removed on the next Pop that touches its
// key. This avoids a background sweeper goroutine — fine for the expected
// throughput (one row per active OIDC flow per tenant per IdP, single-digit
// k/s peak).
type InMemoryInFlightStore struct {
	mu sync.Mutex
	m  map[string]inFlightEntry
}

// NewInMemoryInFlightStore returns an empty in-memory store.
func NewInMemoryInFlightStore() *InMemoryInFlightStore {
	return &InMemoryInFlightStore{m: make(map[string]inFlightEntry)}
}

// Put inserts payload under key with the given TTL. ttl <= 0 is treated as
// "expire immediately on next read" — the store does NOT panic on zero TTL.
func (s *InMemoryInFlightStore) Put(_ context.Context, key string, payload InFlightPayload, ttl time.Duration) error {
	s.mu.Lock()
	s.m[key] = inFlightEntry{
		Payload:  payload,
		ExpireAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
	return nil
}

// Pop reads-and-deletes the record at key. See interface doc for semantics.
func (s *InMemoryInFlightStore) Pop(_ context.Context, key string) (InFlightPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return InFlightPayload{}, ErrInFlightNotFound
	}
	// Delete unconditionally — single-use semantics on both happy path
	// and expiry. A second Pop sees ErrInFlightNotFound.
	delete(s.m, key)
	if time.Now().After(e.ExpireAt) {
		return InFlightPayload{}, ErrInFlightExpired
	}
	return e.Payload, nil
}
