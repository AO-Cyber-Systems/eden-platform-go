package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryAllowsBurstUpToCapacity(t *testing.T) {
	l := NewMemory()
	tier := Tier{Name: "free", RequestsPerMinute: 60}
	ctx := context.Background()

	// Prime bucket via first call, then top up via Observation to simulate burst.
	_, _ = l.Check(ctx, "k", tier)
	l.Update("k", Observation{LimitRequests: 60, RemainingRequests: 60, ResetRequests: 60 * time.Second})

	allowed := 0
	for i := 0; i < 60; i++ {
		dec, err := l.Check(ctx, "k", tier)
		if err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
		if dec.Allow {
			allowed++
		}
	}
	if allowed < 30 || allowed > 61 {
		t.Errorf("expected ~60 allowed in burst, got %d", allowed)
	}
}

func TestMemoryDenyAfterExhaust(t *testing.T) {
	l := NewMemory()
	tier := Tier{Name: "tiny", RequestsPerMinute: 1}
	ctx := context.Background()

	dec, _ := l.Check(ctx, "k", tier)
	if !dec.Allow {
		t.Fatalf("first call should be allowed")
	}
	dec, _ = l.Check(ctx, "k", tier)
	if dec.Allow {
		t.Errorf("second call should be denied")
	}
	if dec.RetryAfter <= 0 {
		t.Errorf("expected positive retry-after")
	}
}

func TestUpdateFromObservation(t *testing.T) {
	l := NewMemory().(*memoryLimiter)
	_, _ = l.Check(context.Background(), "k", Tier{Name: "x", RequestsPerMinute: 60})
	l.Update("k", Observation{LimitRequests: 200, RemainingRequests: 100, ResetRequests: 60 * time.Second})
	snap := l.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 snap")
	}
	if snap[0].ObservedLimit != 200 {
		t.Errorf("expected observed limit 200, got %d", snap[0].ObservedLimit)
	}
	if snap[0].ObservedRemaining != 100 {
		t.Errorf("expected remaining 100, got %d", snap[0].ObservedRemaining)
	}
}

func TestRetryAfterBlocks(t *testing.T) {
	l := NewMemory()
	_, _ = l.Check(context.Background(), "k", Tier{Name: "x", RequestsPerMinute: 60})
	l.Update("k", Observation{RetryAfter: time.Second})
	dec, _ := l.Check(context.Background(), "k", Tier{Name: "x", RequestsPerMinute: 60})
	if dec.Allow {
		t.Errorf("should be blocked by retry-after")
	}
}

type stubRedis struct {
	mu     sync.Mutex
	counts map[string]int64
	ttls   map[string]time.Time
	failOn string
}

func newStubRedis() *stubRedis {
	return &stubRedis{counts: make(map[string]int64), ttls: make(map[string]time.Time)}
}

func (s *stubRedis) Incr(_ context.Context, k string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOn == k {
		return 0, errors.New("redis down")
	}
	s.counts[k]++
	return s.counts[k], nil
}

func (s *stubRedis) ExpireNX(_ context.Context, k string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ttls[k]; ok {
		return nil
	}
	s.ttls[k] = time.Now().Add(ttl)
	return nil
}

func (s *stubRedis) Get(_ context.Context, k string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[k], nil
}

func (s *stubRedis) TTL(_ context.Context, k string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.ttls[k]; ok {
		return time.Until(t), nil
	}
	return -1, nil
}

func TestRedisFleetCounterBlocks(t *testing.T) {
	rc := newStubRedis()
	l := NewRedis(rc, "test:")
	tier := Tier{Name: "tier", RequestsPerMinute: 3}
	ctx := context.Background()

	allowedFleet := 0
	for i := 0; i < 5; i++ {
		dec, err := l.Check(ctx, "k", tier)
		if errors.Is(err, ErrFleetLimit) {
			continue
		}
		if dec.Allow {
			allowedFleet++
		}
	}
	if allowedFleet > 3 {
		t.Errorf("expected fleet to cap at 3, got %d", allowedFleet)
	}
	if rc.counts["test:k"] < 3 {
		t.Errorf("expected fleet counter ≥3, got %d", rc.counts["test:k"])
	}
}

func TestRedisFailureFallsBackToLocal(t *testing.T) {
	rc := newStubRedis()
	rc.failOn = "test:k"
	l := NewRedis(rc, "test:")
	tier := Tier{Name: "tier", RequestsPerMinute: 60}

	dec, err := l.Check(context.Background(), "k", tier)
	if err != nil {
		t.Errorf("unexpected error on Redis hiccup: %v", err)
	}
	if !dec.Allow {
		t.Errorf("expected fallback to allow first call")
	}
}

func TestSnapshotIncludesFleetMetadata(t *testing.T) {
	rc := newStubRedis()
	l := NewRedis(rc, "test:")
	_, _ = l.Check(context.Background(), "k", Tier{Name: "x", RequestsPerMinute: 60})
	snaps := l.Snapshot()
	if len(snaps) != 1 || !snaps[0].FleetBacked {
		t.Errorf("expected fleet-backed snap, got %+v", snaps)
	}
	if snaps[0].FleetCount < 1 {
		t.Errorf("expected at least 1 fleet count, got %d", snaps[0].FleetCount)
	}
}

func TestBucketKeyComposes(t *testing.T) {
	if got := BucketKey("tenant", "abc", "endpoint", "/v1"); got != "tenant:abc:endpoint:/v1" {
		t.Errorf("BucketKey: got %q", got)
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	d := MustParseRetryAfter("30")
	if d != 30*time.Second {
		t.Errorf("expected 30s, got %v", d)
	}
	if MustParseRetryAfter("") != 0 {
		t.Errorf("expected 0 for empty header")
	}
}
