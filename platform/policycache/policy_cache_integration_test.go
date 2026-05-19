//go:build integration

package policycache_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aocybersystems/eden-platform-go/platform/policycache"
)

func TestPolicyCache_NotifyTriggers_RefreshOne(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_pc_notify")

	var refreshOneCalls atomic.Int64
	var lastKey atomic.Int64

	refresh := func(ctx context.Context, lastSeen time.Time) error { return nil }
	parseKey := func(payload string) (int, error) { return strconv.Atoi(payload) }
	refreshOne := func(ctx context.Context, k int) error {
		refreshOneCalls.Add(1)
		lastKey.Store(int64(k))
		return nil
	}

	pc := policycache.NewPolicyCache[int, string](
		lc, channel, refresh, parseKey, refreshOne,
		policycache.WithPollInterval(time.Hour), // suppress poller noise
	)
	defer func() {
		_ = pc.Shutdown(context.Background())
	}()

	// Give the listener time to subscribe.
	time.Sleep(250 * time.Millisecond)

	// Fire NOTIFY '<channel>', '42' from a different conn.
	_, err := pool.Exec(context.Background(), fmt.Sprintf("NOTIFY %s, '42'", channel))
	require.NoError(t, err)

	// Wait up to 2s for refreshOne to fire.
	deadline := time.After(2 * time.Second)
	for {
		if refreshOneCalls.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refreshOne was not called within 2s")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if got := lastKey.Load(); got != 42 {
		t.Errorf("refreshOne called with k=%d, want 42", got)
	}
}

func TestPolicyCache_PollerTicks_RefreshCalled(t *testing.T) {
	lc, _, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_pc_poll")
	var refreshCalls atomic.Int64

	refresh := func(ctx context.Context, lastSeen time.Time) error {
		refreshCalls.Add(1)
		return nil
	}
	parseKey := func(payload string) (int, error) { return strconv.Atoi(payload) }
	refreshOne := func(ctx context.Context, k int) error { return nil }

	pc := policycache.NewPolicyCache[int, string](
		lc, channel, refresh, parseKey, refreshOne,
		policycache.WithPollInterval(100*time.Millisecond),
	)
	defer func() {
		_ = pc.Shutdown(context.Background())
	}()

	time.Sleep(350 * time.Millisecond)

	// Expect: immediate fire + ≥2 ticks at 100ms = ≥3 calls.
	if got := refreshCalls.Load(); got < 3 {
		t.Errorf("expected ≥3 refresh calls in 350ms (immediate + ticks); got %d", got)
	}
}

func TestPolicyCache_Shutdown_Idempotent(t *testing.T) {
	lc, _, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_pc_shutdown")
	refresh := func(ctx context.Context, lastSeen time.Time) error { return nil }
	parseKey := func(payload string) (int, error) { return strconv.Atoi(payload) }
	refreshOne := func(ctx context.Context, k int) error { return nil }

	pc := policycache.NewPolicyCache[int, string](
		lc, channel, refresh, parseKey, refreshOne,
		policycache.WithPollInterval(time.Hour),
	)
	time.Sleep(150 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pc.Shutdown(ctx); err != nil {
		t.Errorf("first Shutdown returned %v; want nil", err)
	}
	if err := pc.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown returned %v; want nil (idempotent)", err)
	}
}

func TestPolicyCache_NotifyParseError_Ignored(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_pc_parseerr")

	var refreshOneCalls atomic.Int64
	refresh := func(ctx context.Context, lastSeen time.Time) error { return nil }
	parseKey := func(payload string) (int, error) {
		return strconv.Atoi(payload) // non-numeric payload → error
	}
	refreshOne := func(ctx context.Context, k int) error {
		refreshOneCalls.Add(1)
		return nil
	}

	pc := policycache.NewPolicyCache[int, string](
		lc, channel, refresh, parseKey, refreshOne,
		policycache.WithPollInterval(time.Hour),
	)
	defer func() { _ = pc.Shutdown(context.Background()) }()
	time.Sleep(250 * time.Millisecond)

	// Bogus payload → parseKey returns error → refreshOne MUST NOT fire.
	_, err := pool.Exec(context.Background(), fmt.Sprintf("NOTIFY %s, 'not-a-number'", channel))
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)
	if got := refreshOneCalls.Load(); got != 0 {
		t.Errorf("refreshOne fired on un-parsable payload (calls=%d); want 0", got)
	}
}

// TestPolicyCache_ConcurrentNotifyAndPoll exercises the documented race:
// NOTIFY firing while the poller is mid-refresh. Both must complete
// without data loss; cache's RWMutex serializes Set/Replace.
func TestPolicyCache_ConcurrentNotifyAndPoll(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_pc_race")

	var refreshCalls atomic.Int64
	var refreshOneCalls atomic.Int64
	var mu sync.Mutex
	cache := make(map[int]string)

	refresh := func(ctx context.Context, lastSeen time.Time) error {
		// Simulate slow refresh.
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		for k := 0; k < 10; k++ {
			cache[k] = "poller"
		}
		mu.Unlock()
		refreshCalls.Add(1)
		return nil
	}
	parseKey := func(payload string) (int, error) { return strconv.Atoi(payload) }
	refreshOne := func(ctx context.Context, k int) error {
		mu.Lock()
		cache[k] = "notify"
		mu.Unlock()
		refreshOneCalls.Add(1)
		return nil
	}

	pc := policycache.NewPolicyCache[int, string](
		lc, channel, refresh, parseKey, refreshOne,
		policycache.WithPollInterval(40*time.Millisecond),
	)
	defer func() { _ = pc.Shutdown(context.Background()) }()
	time.Sleep(200 * time.Millisecond)

	// Burst-fire NOTIFYs while poller is running.
	for i := 0; i < 20; i++ {
		_, err := pool.Exec(context.Background(),
			fmt.Sprintf("NOTIFY %s, '%d'", channel, i%10))
		require.NoError(t, err)
		time.Sleep(15 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	if refreshCalls.Load() == 0 {
		t.Errorf("expected poller refresh to fire at least once")
	}
	if refreshOneCalls.Load() == 0 {
		t.Errorf("expected refreshOne to fire at least once")
	}
}
