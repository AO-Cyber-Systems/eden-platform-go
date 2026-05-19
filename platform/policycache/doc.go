// Package policycache is a multi-replica-safe versioned-snapshot cache for
// per-tenant configuration rows. It is the canonical Eden primitive for
// "policy hot-reload": a row in Postgres changes, all replicas notice and
// refresh their in-memory copy before the next request.
//
// # Motivation
//
// Many products in the AOCyber portfolio need to read frequently-mutating
// per-tenant configuration (federation policy, OAuth policy, MFA policy,
// inactivity policy, rate-limit policy, etc.) on every request. The naive
// "query Postgres on every request" approach is wasteful; the naive
// "cache in memory" approach breaks horizontal scaling because Replica A
// caches a stale value after Replica B writes the new one.
//
// policycache solves this with three coordinated components:
//
//  1. Cache[K, V] — a thread-safe, generic, in-memory map[K]V with
//     snapshot replacement semantics. Designed for ~10^4 entries
//     (tenants × rows-per-tenant); not an LRU/TTL eviction cache.
//
//  2. PostgresListener — wraps a *dedicated* *pgx.Conn (NOT a
//     pgxpool.Pool — pool connections are recycled and silently drop
//     LISTEN subscriptions) and surfaces LISTEN/NOTIFY payloads as
//     callbacks. Acts as the optimistic invalidation channel: when a
//     replica writes a row, it issues NOTIFY <channel>, '<key>'; all
//     other replicas listening receive it within a few milliseconds.
//
//  3. IntervalPoller — periodically (default 30s) calls a refresh
//     function. Acts as the correctness floor: even if every NOTIFY
//     is lost (network partition, conn drop, listener crash), the
//     cache converges within one poll interval.
//
// PolicyCache combines the three into a single primitive: pass a *pgx.Conn,
// a channel name, a "refresh-all-changed-since" function, a "parse-payload"
// function, and a "refresh-one-key" function, and the combinator runs the
// listener + poller goroutines for you.
//
// # Conn lifetime contract
//
// CRITICAL: The *pgx.Conn passed to PostgresListener (and to NewPolicyCache)
// MUST be a dedicated connection acquired via pool.Acquire() and NEVER
// released back to the pool while the listener is running. Pool-recycled
// connections drop their LISTEN subscriptions silently, leading to a cache
// that appears healthy but never receives NOTIFY events again.
//
// The typical pattern is:
//
//	dedicated, _ := pool.Acquire(ctx)
//	pc := policycache.NewPolicyCache(dedicated.Conn(), "policy_changed", ...)
//	defer pc.Shutdown(shutdownCtx)
//	defer dedicated.Release()
//
// # Multi-replica safety
//
// LISTEN/NOTIFY is per-connection: a NOTIFY landing on Replica A does NOT
// reach Replica B unless Replica B has its own LISTEN. Every replica must
// hold its own dedicated *pgx.Conn and its own PolicyCache instance. The
// poller is the floor that guarantees ≤ poll-interval staleness even when
// NOTIFY is lost.
//
// # See also
//
// See README.md in this package for a worked AOID example, the anti-pattern
// table, and the reconnect strategy guidance.
package policycache
