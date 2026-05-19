package oidcrp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func samplePayload() InFlightPayload {
	return InFlightPayload{
		PKCEVerifier: "verifier-1234567890123456789012345678901234567890ABC",
		StoredNonce:  "nonce-xyz",
		TenantID:     "tenant-1",
		IdpID:        "idp-okta",
		ReturnURL:    "https://app.example.com/cb",
		CreatedAt:    time.Now(),
	}
}

func TestInMemoryInFlightStore_PutPopHappy(t *testing.T) {
	t.Parallel()
	s := NewInMemoryInFlightStore()
	ctx := context.Background()
	p := samplePayload()
	if err := s.Put(ctx, "n1", p, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Pop(ctx, "n1")
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.PKCEVerifier != p.PKCEVerifier {
		t.Errorf("PKCEVerifier mismatch")
	}
	if got.StoredNonce != p.StoredNonce {
		t.Errorf("StoredNonce mismatch")
	}
}

func TestInMemoryInFlightStore_PopIsSingleUse(t *testing.T) {
	t.Parallel()
	s := NewInMemoryInFlightStore()
	ctx := context.Background()
	if err := s.Put(ctx, "n1", samplePayload(), time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Pop(ctx, "n1"); err != nil {
		t.Fatalf("first Pop: %v", err)
	}
	_, err := s.Pop(ctx, "n1")
	if !errors.Is(err, ErrInFlightNotFound) {
		t.Fatalf("second Pop: expected ErrInFlightNotFound, got %v", err)
	}
}

func TestInMemoryInFlightStore_NotFound(t *testing.T) {
	t.Parallel()
	s := NewInMemoryInFlightStore()
	_, err := s.Pop(context.Background(), "never-existed")
	if !errors.Is(err, ErrInFlightNotFound) {
		t.Fatalf("expected ErrInFlightNotFound, got %v", err)
	}
}

func TestInMemoryInFlightStore_Expired(t *testing.T) {
	t.Parallel()
	s := NewInMemoryInFlightStore()
	ctx := context.Background()
	// Put with a tiny TTL.
	if err := s.Put(ctx, "n-exp", samplePayload(), 10*time.Millisecond); err != nil {
		t.Fatalf("Put: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	_, err := s.Pop(ctx, "n-exp")
	if !errors.Is(err, ErrInFlightExpired) {
		t.Fatalf("expected ErrInFlightExpired, got %v", err)
	}
	// After Pop hits expired, the key should be gone (no second-chance read).
	_, err = s.Pop(ctx, "n-exp")
	if !errors.Is(err, ErrInFlightNotFound) {
		t.Fatalf("expected ErrInFlightNotFound on second Pop, got %v", err)
	}
}

func TestInMemoryInFlightStore_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewInMemoryInFlightStore()
	ctx := context.Background()
	const N = 200
	var wg sync.WaitGroup

	// Writers populate distinct keys.
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := nonceKey(i)
			_ = s.Put(ctx, key, samplePayload(), time.Minute)
		}()
	}
	wg.Wait()

	// Readers pop the same set concurrently — each key should pop exactly
	// once (winning goroutine) and ErrInFlightNotFound from the loser.
	var popped, missed int64
	var mu sync.Mutex
	wg = sync.WaitGroup{}
	for i := 0; i < N; i++ {
		key := nonceKey(i)
		wg.Add(2)
		for k := 0; k < 2; k++ {
			go func() {
				defer wg.Done()
				_, err := s.Pop(ctx, key)
				mu.Lock()
				if err == nil {
					popped++
				} else if errors.Is(err, ErrInFlightNotFound) {
					missed++
				} else {
					t.Errorf("unexpected err: %v", err)
				}
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	if popped != N || missed != N {
		t.Errorf("expected each key popped once: popped=%d missed=%d (want %d/%d)", popped, missed, N, N)
	}
}

func nonceKey(i int) string {
	// Cheap deterministic key generator that avoids strconv import.
	return string(rune('a'+i/26)) + string(rune('a'+i%26))
}

// Compile-time assertion that InMemoryInFlightStore satisfies the
// interface — guarantees future refactors don't drift the surface.
var _ InFlightStore = (*InMemoryInFlightStore)(nil)
