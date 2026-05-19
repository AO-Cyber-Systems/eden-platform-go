package policycache

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCache_GetMissing_ReturnsZeroAndFalse(t *testing.T) {
	c := New[string, int]()
	v, ok := c.Get("absent")
	if ok {
		t.Errorf("expected ok=false for missing key, got true")
	}
	if v != 0 {
		t.Errorf("expected zero-value 0 for missing key, got %d", v)
	}
}

func TestCache_SetGet_RoundTrip(t *testing.T) {
	c := New[string, int]()
	c.Set("a", 1)
	c.Set("b", 2)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Errorf("Get(a) = (%d, %v), want (1, true)", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Errorf("Get(b) = (%d, %v), want (2, true)", v, ok)
	}
}

func TestCache_Invalidate_RemovesEntry(t *testing.T) {
	c := New[string, int]()
	c.Set("a", 1)
	c.Invalidate("a")
	if _, ok := c.Get("a"); ok {
		t.Errorf("expected Get(a) to return ok=false after Invalidate")
	}
	// Invalidating a missing key is a no-op (does not panic).
	c.Invalidate("absent")
}

func TestCache_Replace_AtomicSwap(t *testing.T) {
	c := New[string, int]()
	c.Set("a", 1)
	c.Set("b", 2)

	snap := map[string]int{"x": 10, "y": 20, "z": 30}
	c.Replace(snap)

	if _, ok := c.Get("a"); ok {
		t.Errorf("expected Get(a)=false after Replace, got ok=true")
	}
	if v, ok := c.Get("x"); !ok || v != 10 {
		t.Errorf("Get(x) = (%d, %v), want (10, true)", v, ok)
	}
	if c.Len() != 3 {
		t.Errorf("Len after Replace = %d, want 3", c.Len())
	}

	// Mutating the source map after Replace must NOT affect the cache.
	snap["x"] = 999
	if v, _ := c.Get("x"); v != 10 {
		t.Errorf("Replace must clone its input; expected v=10, got v=%d", v)
	}
}

func TestCache_Len_Keys_Consistent(t *testing.T) {
	c := New[string, int]()
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	if c.Len() != 3 {
		t.Errorf("Len = %d, want 3", c.Len())
	}

	keys := c.Keys()
	sort.Strings(keys)
	want := []string{"a", "b", "c"}
	if len(keys) != len(want) {
		t.Fatalf("Keys len = %d, want %d", len(keys), len(want))
	}
	for i := range keys {
		if keys[i] != want[i] {
			t.Errorf("Keys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestCache_ReplaceWithNil_ClearsCache(t *testing.T) {
	c := New[string, int]()
	c.Set("a", 1)
	c.Replace(nil)
	if c.Len() != 0 {
		t.Errorf("expected Len=0 after Replace(nil), got %d", c.Len())
	}
}

// TestCache_Concurrent_RaceFree exercises Get/Set/Replace under the race
// detector with many goroutines. Must be run with -race to be meaningful.
func TestCache_Concurrent_RaceFree(t *testing.T) {
	c := New[int, int]()

	const writers = 16
	const readers = 16
	const iters = 500

	var wg sync.WaitGroup
	wg.Add(writers + readers + 1)

	// Writers: random Set/Invalidate.
	for w := 0; w < writers; w++ {
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				k := (seed + i) % 64
				c.Set(k, i)
				if i%7 == 0 {
					c.Invalidate(k)
				}
			}
		}(w)
	}

	// Readers: Get is allowed to race with Set; we only assert no -race trip.
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_, _ = c.Get(i % 64)
				_ = c.Len()
				_ = c.Keys()
			}
		}()
	}

	// Replacer: periodically swaps the whole map.
	go func() {
		defer wg.Done()
		for i := 0; i < iters/10; i++ {
			snap := map[int]int{}
			for k := 0; k < 64; k++ {
				snap[k] = i
			}
			c.Replace(snap)
		}
	}()

	wg.Wait()
}

// TestCache_Replace_ObservedAsAtomic asserts that a concurrent Get
// observes EITHER the pre-Replace or post-Replace state, never a partial
// map. We approximate this by Replace'ing between two disjoint snapshots
// and verifying every Get returns the value-set of one snapshot.
func TestCache_Replace_ObservedAsAtomic(t *testing.T) {
	c := New[string, int]()
	pre := map[string]int{"a": 1, "b": 2, "c": 3}
	post := map[string]int{"a": 10, "b": 20, "c": 30}
	c.Replace(pre)

	done := make(chan struct{})
	var observed atomic.Int64
	var violations atomic.Int64

	go func() {
		defer close(done)
		for {
			select {
			case <-time.After(50 * time.Millisecond):
				return
			default:
			}
			va, _ := c.Get("a")
			vb, _ := c.Get("b")
			vc, _ := c.Get("c")
			observed.Add(1)
			isPre := va == 1 && vb == 2 && vc == 3
			isPost := va == 10 && vb == 20 && vc == 30
			if !isPre && !isPost {
				violations.Add(1)
			}
		}
	}()

	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			c.Replace(post)
		} else {
			c.Replace(pre)
		}
	}
	<-done

	if observed.Load() == 0 {
		t.Skip("race window too narrow on this machine; observed 0 reads")
	}
	if violations.Load() != 0 {
		t.Errorf("Replace observed as non-atomic: %d violations out of %d reads",
			violations.Load(), observed.Load())
	}
}
