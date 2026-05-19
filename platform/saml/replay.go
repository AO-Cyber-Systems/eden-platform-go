package saml

import (
	"context"
	"sync"
	"time"
)

// ReplayStore records observed SAML assertion IDs and reports whether a
// given ID has already been observed within its expiry window.
//
// Implementations MUST be safe for concurrent use. Implementations MUST
// implement Seen atomically — for N concurrent calls with the same
// assertionID, exactly one MUST return alreadySeen=false and the rest
// MUST return alreadySeen=true. A non-atomic implementation would allow
// SAML assertion replay attacks.
//
// Two implementations exist:
//   - InMemoryReplayStore in this package — testing and single-process
//     deployments.
//   - The AOID Postgres-backed store in TRD 06-05 — production
//     multi-replica deployments. The Postgres store uses an
//     INSERT ... ON CONFLICT DO NOTHING + rows-affected check to achieve
//     the same atomicity guarantee.
type ReplayStore interface {
	// Seen records assertionID and returns true if it was ALREADY
	// recorded within its expiry. expiresAt informs the implementation
	// when the row may be evicted; callers SHOULD pass the assertion's
	// NotOnOrAfter time (or a reasonable upper bound like
	// NotOnOrAfter + max-clock-skew).
	Seen(ctx context.Context, assertionID string, expiresAt time.Time) (alreadySeen bool, err error)
}

// InMemoryReplayStore is a goroutine-safe in-memory ReplayStore suitable
// for unit tests and single-process AOID deployments.
//
// Entries are evicted lazily by a background goroutine that sweeps on a
// fixed interval (default 30 seconds). Operators wishing tighter or
// looser sweep cadence should construct the store via
// NewInMemoryReplayStoreWithInterval.
type InMemoryReplayStore struct {
	mu       sync.Mutex
	entries  map[string]time.Time // assertionID -> expiresAt
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewInMemoryReplayStore returns a store with a 30-second sweep interval.
func NewInMemoryReplayStore() *InMemoryReplayStore {
	return NewInMemoryReplayStoreWithInterval(30 * time.Second)
}

// NewInMemoryReplayStoreWithInterval is the same but with a custom sweep
// interval — useful for tests that want to observe the background sweep
// without sleeping for 30 seconds.
func NewInMemoryReplayStoreWithInterval(interval time.Duration) *InMemoryReplayStore {
	s := &InMemoryReplayStore{
		entries:  map[string]time.Time{},
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	if interval > 0 {
		s.wg.Add(1)
		go s.loop()
	}
	return s
}

// Seen records assertionID + expiresAt atomically and reports whether
// the ID was already present. Safe for concurrent use.
func (s *InMemoryReplayStore) Seen(ctx context.Context, assertionID string, expiresAt time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[assertionID]; ok {
		return true, nil
	}
	s.entries[assertionID] = expiresAt
	return false, nil
}

// SweepExpired removes entries with expiresAt <= now. Exposed for tests;
// production callers should rely on the background sweep.
func (s *InMemoryReplayStore) SweepExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, exp := range s.entries {
		if !exp.After(now) {
			delete(s.entries, id)
		}
	}
}

// Close stops the background sweep goroutine. Safe to call multiple times
// (subsequent calls are no-ops).
func (s *InMemoryReplayStore) Close() {
	select {
	case <-s.stopCh:
		return
	default:
	}
	close(s.stopCh)
	s.wg.Wait()
}

func (s *InMemoryReplayStore) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.SweepExpired(now)
		}
	}
}
