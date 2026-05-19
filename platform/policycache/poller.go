package policycache

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// IntervalPoller calls a refresh function on a fixed interval and is the
// "correctness floor" for the policycache primitive: even if every NOTIFY
// is lost (network partition, listener crash, conn recycle), the cache
// converges within one poll interval.
//
// # Semantics
//
//   - Run blocks until ctx is cancelled; returns nil on clean shutdown.
//   - On each tick, refresh(ctx, lastSeen) is invoked. lastSeen is the
//     wall-clock time captured BEFORE the most recent refresh started
//     (or zero on the first call). refresh is expected to query rows
//     whose updated_at > lastSeen and update the Cache accordingly.
//   - On the first tick after Run begins, the call is fired immediately
//     (so the cache warms up without waiting one full interval). This
//     matches the behavior consumers expect: "start the service, the
//     cache is populated within seconds, not 30 seconds".
//   - If refresh returns an error, it is logged via slog and the next
//     tick proceeds normally. Errors do NOT cause Run to exit. lastSeen
//     is NOT advanced on a failed refresh, so the next attempt re-tries
//     the same window.
//
// # Why clock-derived lastSeen instead of max(observed updated_at)?
//
// Using clock time means we tolerate clock skew (up to ~poll-interval
// minus some safety) without losing notifications. Using max(updated_at)
// would be lossy across replicas with different clocks AND would interact
// badly with Postgres tx commit semantics (a row written at T1 may not
// be visible to a SELECT at T2 if T2's snapshot was taken before the
// commit landed). At a 30s default poll interval, second-level clock
// skew is irrelevant.
type IntervalPoller struct {
	interval time.Duration
	refresh  func(ctx context.Context, lastSeen time.Time) error

	mu       sync.Mutex
	lastSeen time.Time
}

// NewPoller constructs an IntervalPoller. Panics if interval is non-positive
// or refresh is nil — both are programmer errors.
func NewPoller(interval time.Duration, refresh func(ctx context.Context, lastSeen time.Time) error) *IntervalPoller {
	if interval <= 0 {
		panic("policycache: NewPoller: interval must be > 0")
	}
	if refresh == nil {
		panic("policycache: NewPoller: refresh must be non-nil")
	}
	return &IntervalPoller{interval: interval, refresh: refresh}
}

// Run loops until ctx is cancelled. The first refresh fires immediately;
// subsequent refreshes fire every interval.
func (p *IntervalPoller) Run(ctx context.Context) error {
	// Fire once immediately so the cache warms without waiting a full tick.
	p.runOnce(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.runOnce(ctx)
		}
	}
}

func (p *IntervalPoller) runOnce(ctx context.Context) {
	p.mu.Lock()
	prev := p.lastSeen
	p.mu.Unlock()

	startedAt := time.Now()

	err := safeRefresh(ctx, p.refresh, prev)
	if err != nil {
		// If the parent context cancelled mid-refresh, swallow silently —
		// shutdown is in progress.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		slog.Warn("policycache: refresh failed",
			"error", err,
			"last_seen", prev,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return
	}

	// Advance lastSeen only on a successful refresh, using the wall-clock
	// time we captured BEFORE the refresh started (so we don't miss rows
	// committed during the refresh window).
	p.mu.Lock()
	if startedAt.After(p.lastSeen) {
		p.lastSeen = startedAt
	}
	p.mu.Unlock()
}

// LastSeen returns the wall-clock time of the most recent successful
// refresh, or the zero time if no refresh has yet succeeded. Useful for
// observability / health checks.
func (p *IntervalPoller) LastSeen() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSeen
}

// safeRefresh wraps the user-supplied refresh function with a panic
// recovery so a buggy refresh closure cannot bring down the poller loop.
func safeRefresh(ctx context.Context, fn func(context.Context, time.Time) error, lastSeen time.Time) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("policycache: refresh panicked")
			slog.Error("policycache: refresh panic", "recovered", r)
		}
	}()
	return fn(ctx, lastSeen)
}
