package scheduler

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresLocker uses Postgres advisory locks for cross-replica scheduling
// dedup. Lock keys are derived by hashing the lease name to a 64-bit int.
//
// Two-replica safety guarantee: pg_try_advisory_lock(key) returns true for
// at most one connection at a time. We use the SESSION-scoped variant and
// release explicitly via pg_advisory_unlock; the release function returns
// the connection to the pool when called.
type PostgresLocker struct {
	pool *pgxpool.Pool
}

// NewPostgresLocker wires a Postgres-backed Locker.
func NewPostgresLocker(pool *pgxpool.Pool) *PostgresLocker { return &PostgresLocker{pool: pool} }

// activeLocks tracks open locker connections (for accounting/metrics).
var activeLocks atomic.Int64

// TryLock attempts to acquire pg_advisory_lock for the hashed name.
//
// Limitations:
//   - ttl is advisory only — the actual lock release happens when the
//     release() func is called or the underlying connection drops.
//   - The hashed key collides if two distinct names produce the same hash;
//     the practical collision rate at portfolio scale is negligible (the
//     lease names include the unix-second timestamp).
func (l *PostgresLocker) TryLock(ctx context.Context, name string, _ time.Duration) (bool, func(), error) {
	key := hashLockName(name)

	// Acquire a dedicated connection so we can release the same one later.
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return false, func() {}, fmt.Errorf("scheduler: acquire conn: %w", err)
	}

	var got bool
	row := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key)
	if err := row.Scan(&got); err != nil {
		conn.Release()
		return false, func() {}, fmt.Errorf("scheduler: pg_try_advisory_lock: %w", err)
	}
	if !got {
		conn.Release()
		return false, func() {}, nil
	}

	activeLocks.Add(1)
	release := func() {
		// Best-effort release. If unlock fails (network blip), the connection
		// release will eventually drop the lock anyway.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(releaseCtx, "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
		activeLocks.Add(-1)
	}
	return true, release, nil
}

// hashLockName converts a string to a 64-bit advisory-lock key.
func hashLockName(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64())
}

// ActiveLockCount returns the number of currently-held PostgresLocker
// leases (debug/metrics only).
func ActiveLockCount() int64 { return activeLocks.Load() }
