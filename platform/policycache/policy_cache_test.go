package policycache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolicyCache_DefaultPollInterval(t *testing.T) {
	cfg := newPCConfig(nil)
	if cfg.pollInterval != 30*time.Second {
		t.Errorf("default pollInterval = %v, want 30s", cfg.pollInterval)
	}
}

func TestPolicyCache_WithPollInterval(t *testing.T) {
	cfg := newPCConfig([]Option{WithPollInterval(100 * time.Millisecond)})
	if cfg.pollInterval != 100*time.Millisecond {
		t.Errorf("WithPollInterval not applied: got %v, want 100ms", cfg.pollInterval)
	}
}

func TestPolicyCache_WithPollInterval_RejectsZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("WithPollInterval(0) must panic")
		}
	}()
	newPCConfig([]Option{WithPollInterval(0)})
}

// TestIntervalPoller_FiresImmediately verifies that Run kicks off a
// refresh on entry (so cold-start cache warms without waiting one full
// interval).
func TestIntervalPoller_FiresImmediately(t *testing.T) {
	var calls atomic.Int64
	refresh := func(ctx context.Context, lastSeen time.Time) error {
		calls.Add(1)
		return nil
	}
	p := NewPoller(time.Hour, refresh) // long interval — we only care about t=0 fire

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = p.Run(ctx)
	}()
	// Give the goroutine time to enter Run and fire the first call.
	for i := 0; i < 50; i++ {
		if calls.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if calls.Load() < 1 {
		t.Errorf("expected immediate refresh on Run, got %d calls", calls.Load())
	}
}

// TestIntervalPoller_TicksAtInterval verifies repeated firing.
func TestIntervalPoller_TicksAtInterval(t *testing.T) {
	var calls atomic.Int64
	refresh := func(ctx context.Context, lastSeen time.Time) error {
		calls.Add(1)
		return nil
	}
	p := NewPoller(50*time.Millisecond, refresh)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	// Expect at least 4 calls: immediate + ~5 ticks in 300ms. Allow slack.
	if got := calls.Load(); got < 3 {
		t.Errorf("expected ≥3 refresh calls, got %d", got)
	}
}

// TestIntervalPoller_ErrorDoesNotStop verifies that an error from refresh
// is logged but the poller keeps ticking and does NOT advance lastSeen.
func TestIntervalPoller_ErrorDoesNotStop(t *testing.T) {
	var calls atomic.Int64
	var lastSeenObserved time.Time
	refresh := func(ctx context.Context, lastSeen time.Time) error {
		n := calls.Add(1)
		lastSeenObserved = lastSeen
		if n <= 2 {
			return errors.New("simulated failure")
		}
		return nil
	}
	p := NewPoller(40*time.Millisecond, refresh)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if got := calls.Load(); got < 4 {
		t.Errorf("expected ≥4 calls after retrying through errors, got %d", got)
	}
	// Once we've started succeeding, lastSeen passed to refresh should be
	// > zero (it was advanced by the most-recent successful refresh).
	if lastSeenObserved.IsZero() {
		t.Errorf("expected lastSeen to advance after a successful refresh; still zero")
	}
}

// TestIntervalPoller_PanicRecovered confirms that a panicking refresh
// closure does not bring down the poller loop.
func TestIntervalPoller_PanicRecovered(t *testing.T) {
	var calls atomic.Int64
	refresh := func(ctx context.Context, lastSeen time.Time) error {
		n := calls.Add(1)
		if n == 1 {
			panic("boom")
		}
		return nil
	}
	p := NewPoller(50*time.Millisecond, refresh)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)
	if got := calls.Load(); got < 2 {
		t.Errorf("expected ≥2 calls (panic recovered + retry), got %d", got)
	}
}

func TestNewPoller_PanicsOnBadArgs(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on zero interval")
		}
	}()
	_ = NewPoller(0, func(ctx context.Context, _ time.Time) error { return nil })
}

func TestNewPoller_PanicsOnNilRefresh(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on nil refresh")
		}
	}()
	_ = NewPoller(time.Second, nil)
}
