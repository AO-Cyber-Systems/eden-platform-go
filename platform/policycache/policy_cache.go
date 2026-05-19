package policycache

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// PolicyCache combines a Cache, a PostgresListener, and an IntervalPoller
// into a single primitive for per-tenant policy hot-reload across
// horizontally-scaled replicas.
//
// The contract from the consumer's perspective:
//
//   - NewPolicyCache spawns two background goroutines (listener + poller).
//   - The listener subscribes to channel and, for each NOTIFY, parses the
//     payload via parseKey and calls refreshOne(ctx, key). refreshOne is
//     responsible for re-fetching that one key from Postgres and updating
//     the Cache (via Set or Invalidate).
//   - The poller fires refresh(ctx, lastSeen) every WithPollInterval
//     (default 30s). refresh is responsible for re-fetching ALL rows with
//     updated_at > lastSeen and updating the Cache (via Replace or many
//     Set). The poller is the correctness floor; NOTIFY is the optimization.
//   - Cache() returns the underlying Cache for read access. The caller's
//     refresh / refreshOne closures should close over the same Cache and
//     write to it through the Cache methods (Set/Replace/Invalidate).
//   - Shutdown stops both goroutines and is idempotent. It does NOT close
//     the conn; the caller owns the conn lifecycle (typically deferred
//     pool.Release).
type PolicyCache[K comparable, V any] struct {
	cache    *Cache[K, V]
	listener *PostgresListener
	poller   *IntervalPoller

	cancel    context.CancelFunc
	wg        sync.WaitGroup
	shutdown  chan struct{}
	closeOnce sync.Once
}

// Option configures a PolicyCache. Functional-options pattern follows
// platform/scheduler precedent (WithTimeout etc.).
type Option func(*pcConfig)

type pcConfig struct {
	pollInterval time.Duration
}

const defaultPollInterval = 30 * time.Second

func newPCConfig(opts []Option) pcConfig {
	cfg := pcConfig{pollInterval: defaultPollInterval}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithPollInterval sets the interval between fallback poller ticks.
// Defaults to 30 seconds. Panics if d <= 0 (always a programmer error).
func WithPollInterval(d time.Duration) Option {
	if d <= 0 {
		panic("policycache: WithPollInterval: d must be > 0")
	}
	return func(c *pcConfig) {
		c.pollInterval = d
	}
}

// NewPolicyCache constructs a PolicyCache and spawns its listener +
// poller goroutines. It does NOT block on initial refresh; the poller
// fires immediately on its own goroutine so the cache warms in the
// background.
//
// Arguments:
//
//   - conn: dedicated *pgx.Conn (NOT from a pool). See the conn-lifetime
//     contract in this package's doc.go.
//   - channel: Postgres LISTEN channel name (Postgres identifier rules).
//   - refresh: called by the poller; must re-fetch all changed rows and
//     update the cache. Receives the wall-clock time captured BEFORE the
//     previous successful refresh started (zero on the first call).
//   - parseKey: called by the listener; converts a NOTIFY payload string
//     into a typed K. Return an error to drop the notification (it is
//     logged but not re-tried; the poller will catch the row eventually).
//   - refreshOne: called by the listener after a successful parseKey;
//     re-fetches and updates the cache for that one key.
//   - opts: functional options (WithPollInterval, ...).
//
// Panics if conn is nil, channel is empty, or any of refresh/parseKey/
// refreshOne is nil — all programmer errors.
func NewPolicyCache[K comparable, V any](
	conn *pgx.Conn,
	channel string,
	refresh func(ctx context.Context, lastSeen time.Time) error,
	parseKey func(payload string) (K, error),
	refreshOne func(ctx context.Context, k K) error,
	opts ...Option,
) *PolicyCache[K, V] {
	if conn == nil {
		panic("policycache: NewPolicyCache: conn must not be nil")
	}
	if channel == "" {
		panic("policycache: NewPolicyCache: channel must not be empty")
	}
	if refresh == nil || parseKey == nil || refreshOne == nil {
		panic("policycache: NewPolicyCache: refresh, parseKey, refreshOne must all be non-nil")
	}

	cfg := newPCConfig(opts)
	cache := New[K, V]()
	listener := NewListener(conn)
	poller := NewPoller(cfg.pollInterval, refresh)

	ctx, cancel := context.WithCancel(context.Background())

	pc := &PolicyCache[K, V]{
		cache:    cache,
		listener: listener,
		poller:   poller,
		cancel:   cancel,
		shutdown: make(chan struct{}),
	}

	// Listener goroutine: blocks on WaitForNotification; on each NOTIFY,
	// parse payload → refreshOne(ctx, key). Errors are logged but do not
	// stop the loop on the listener side — Listen itself returns on
	// unrecoverable conn errors, at which point we log and exit.
	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		onNotify := func(payload string) {
			k, err := parseKey(payload)
			if err != nil {
				slog.Warn("policycache: parseKey failed; dropping NOTIFY",
					"channel", channel, "payload", payload, "error", err)
				return
			}
			// Refresh in a goroutine so the listener loop is not blocked
			// by a slow refreshOne. Bounded concurrency is the caller's
			// responsibility if refreshOne is expensive.
			pc.wg.Add(1)
			go func() {
				defer pc.wg.Done()
				if err := refreshOne(ctx, k); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					slog.Warn("policycache: refreshOne failed",
						"channel", channel, "payload", payload, "error", err)
				}
			}()
		}
		onError := func(err error) {
			slog.Warn("policycache: listener error", "channel", channel, "error", err)
		}
		if err := listener.Listen(ctx, channel, onNotify, onError); err != nil {
			// Already logged via onError; nothing more to do here. The
			// caller is responsible for monitoring conn health (e.g. via
			// LastSeen on the poller) and reconnecting.
			_ = err
		}
	}()

	// Poller goroutine.
	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		_ = poller.Run(ctx)
	}()

	return pc
}

// Cache returns the underlying Cache. Callers read from it; writes
// happen via the refresh / refreshOne callbacks passed to NewPolicyCache.
func (p *PolicyCache[K, V]) Cache() *Cache[K, V] {
	return p.cache
}

// Poller returns the underlying IntervalPoller. Exposed primarily so
// callers can observe LastSeen for health checks.
func (p *PolicyCache[K, V]) Poller() *IntervalPoller {
	return p.poller
}

// Shutdown stops the listener and poller goroutines and waits for them
// to exit. It is idempotent: subsequent calls are no-ops and return nil.
// Shutdown does NOT close the *pgx.Conn passed to NewPolicyCache — the
// caller owns conn lifecycle.
//
// The provided ctx bounds how long Shutdown will wait for goroutines to
// drain. If ctx expires before drain, Shutdown returns ctx.Err().
func (p *PolicyCache[K, V]) Shutdown(ctx context.Context) error {
	var rerr error
	p.closeOnce.Do(func() {
		p.cancel()
		close(p.shutdown)
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		rerr = ctx.Err()
	}
	return rerr
}
