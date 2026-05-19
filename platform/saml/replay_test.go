package saml

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInMemoryReplayStore_FirstSeenReturnsFalse(t *testing.T) {
	store := NewInMemoryReplayStore()
	defer store.Close()

	seen, err := store.Seen(context.Background(), "assert-1", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("Seen first: %v", err)
	}
	if seen {
		t.Fatal("first Seen should return false")
	}
}

func TestInMemoryReplayStore_SecondSeenReturnsTrue(t *testing.T) {
	store := NewInMemoryReplayStore()
	defer store.Close()

	if _, err := store.Seen(context.Background(), "assert-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("first Seen: %v", err)
	}
	seen, err := store.Seen(context.Background(), "assert-1", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("second Seen: %v", err)
	}
	if !seen {
		t.Fatal("second Seen should return true")
	}
}

// Concurrency-critical test: 100 goroutines call Seen with the same
// assertion ID. EXACTLY one should observe alreadySeen=false; the other
// 99 must observe alreadySeen=true. This is the load-bearing invariant
// of the replay store — a race here would let an attacker replay an
// assertion.
func TestInMemoryReplayStore_ConcurrentSeen_ExactlyOneFirstObserver(t *testing.T) {
	store := NewInMemoryReplayStore()
	defer store.Close()

	const N = 100
	var wg sync.WaitGroup
	var firstCount, replayCount int64

	exp := time.Now().Add(time.Minute)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			seen, err := store.Seen(context.Background(), "race-id", exp)
			if err != nil {
				t.Errorf("Seen: %v", err)
				return
			}
			if seen {
				atomic.AddInt64(&replayCount, 1)
			} else {
				atomic.AddInt64(&firstCount, 1)
			}
		}()
	}
	wg.Wait()

	if firstCount != 1 {
		t.Fatalf("firstCount=%d (want exactly 1)", firstCount)
	}
	if replayCount != N-1 {
		t.Fatalf("replayCount=%d (want %d)", replayCount, N-1)
	}
}

func TestInMemoryReplayStore_ExpiredEntriesEvictable(t *testing.T) {
	store := NewInMemoryReplayStore()
	defer store.Close()

	// Insert with an already-elapsed TTL.
	past := time.Now().Add(-time.Hour)
	if _, err := store.Seen(context.Background(), "old-id", past); err != nil {
		t.Fatalf("Seen: %v", err)
	}

	// Force the GC sweep.
	store.SweepExpired(time.Now())

	// After sweep, the same ID should appear unseen.
	seen, err := store.Seen(context.Background(), "old-id", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("Seen after sweep: %v", err)
	}
	if seen {
		t.Fatal("expected old-id to be evicted after sweep, but Seen=true")
	}
}

func TestInMemoryReplayStore_DistinctIDs(t *testing.T) {
	store := NewInMemoryReplayStore()
	defer store.Close()
	exp := time.Now().Add(time.Minute)
	for _, id := range []string{"a", "b", "c"} {
		seen, err := store.Seen(context.Background(), id, exp)
		if err != nil {
			t.Fatalf("Seen %s: %v", id, err)
		}
		if seen {
			t.Fatalf("%s: first observation should be unseen", id)
		}
	}
}

func TestInMemoryReplayStore_BackgroundCleanupRunsAndStops(t *testing.T) {
	// Use a very short sweep interval to exercise the background goroutine.
	store := NewInMemoryReplayStoreWithInterval(20 * time.Millisecond)

	past := time.Now().Add(-time.Hour)
	if _, err := store.Seen(context.Background(), "x", past); err != nil {
		t.Fatalf("Seen: %v", err)
	}
	// Allow the background sweep to run.
	time.Sleep(80 * time.Millisecond)

	// Close should not deadlock.
	store.Close()
}

// Ensure InMemoryReplayStore satisfies ReplayStore at compile-time.
var _ ReplayStore = (*InMemoryReplayStore)(nil)
