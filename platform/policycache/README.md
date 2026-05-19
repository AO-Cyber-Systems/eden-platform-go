# platform/policycache

Multi-replica-safe versioned-snapshot cache for per-tenant configuration
rows. The canonical Eden primitive for "policy hot-reload": a row in
Postgres changes, every replica notices and refreshes its in-memory copy
before the next request — no service restart, no manual intervention.

## Motivation

Many products in the AOCyber portfolio need to read frequently-mutating
per-tenant configuration on every request: federation policy, OAuth
policy, MFA / session policy, inactivity policy, rate-limit policy, and
so on. Three naive approaches all fall short:

1. **Query Postgres on every request.** Easy to reason about, but
   wastes the database budget and adds tens of milliseconds of
   round-trip latency to every hot-path call.
2. **In-process cache with no invalidation.** Fast, but breaks the moment
   you horizontally scale: Replica A caches a stale value after Replica B
   writes the new one, and the only fix is a rolling restart.
3. **Cache with TTL.** Mitigates staleness but never eliminates it — and
   admins still complain that "I changed the policy and it didn't take
   effect for two minutes".

`policycache` solves the third bullet without giving up on the first
two, by combining three coordinated components.

## Three-component model

### `Cache[K comparable, V any]`

A thread-safe generic map with snapshot semantics:

- `Get(k)` — `sync.RWMutex` read; returns zero-value + false on miss.
- `Set(k, v)` / `Invalidate(k)` — single-key write under write lock.
- `Replace(map[K]V)` — atomic full-snapshot swap; defensively clones
  its input so the caller may freely mutate the source map afterwards.
- `Snapshot()` — defensive copy of the full map. Use for tests and
  bulk-export; not for hot paths.

Designed for ~10⁴ entries (tenants × policy-rows-per-tenant); not an
LRU/TTL eviction cache. Memory budget at typical row size (~1KB) is
~10MB — comfortably within any service's budget.

`V` is stored by value with no defensive deep-clone. Callers that
mutate cached values **must** Clone before mutating. The pattern we
recommend is "treat cache values as immutable" — write a fresh value
through `Set` instead of mutating in place.

### `PostgresListener`

Wraps a **dedicated** `*pgx.Conn` and surfaces `LISTEN`/`NOTIFY`
payloads as callbacks. The listener:

- Executes `LISTEN <channel>` once on entry.
- Blocks on `WaitForNotification` and dispatches each payload via the
  caller's `onNotify` callback. Notifications on channels other than the
  subscribed one are filtered out.
- Returns `nil` on a clean `ctx` cancellation.
- Returns the underlying error on connection failure or LISTEN-statement
  failure, and invokes `onError` once with the same error so the caller
  can record metrics and schedule a reconnect.

The listener does **not** attempt to reconnect on its own. Reconnect is
a caller-policy concern (the caller knows the pool, retry budget,
backoff strategy, etc.).

### `IntervalPoller`

A simple loop that calls `refresh(ctx, lastSeen)` every interval
(default 30s):

- Fires once immediately on `Run` so the cache warms without waiting
  one full interval.
- Advances `lastSeen` to the wall-clock time captured **before** the
  refresh started — only on successful refresh. A failed refresh
  re-tries the same window on the next tick.
- A panicking `refresh` is recovered; the loop continues.

Why clock-derived `lastSeen` instead of `max(observed updated_at)`?
Because clock skew between replicas is bounded (a few seconds in any
well-run fleet) and is irrelevant at a 30-second poll interval.
`max(updated_at)` would interact badly with Postgres tx commit
semantics — a row written at T1 may not be visible to a `SELECT` at T2
if T2's snapshot was taken before the commit landed.

### `PolicyCache` combinator

Wires the three pieces together. Pass a `*pgx.Conn`, a channel name,
and three callbacks; `policycache` spawns the listener + poller
goroutines and returns a single struct.

## Conn-lifetime contract

> **CRITICAL.** The `*pgx.Conn` passed to `NewListener` (and to
> `NewPolicyCache`) MUST be a dedicated connection. Acquire it via
> `pool.Acquire(ctx)` and hold it for the lifetime of the listener.
> Do NOT release it back to the pool while the listener is running.

Why: `pgxpool` recycles connections (max-conn-lifetime, idle timeouts,
health checks). A recycled connection silently drops its `LISTEN`
subscription. The cache appears healthy — `Get` continues to return
values — but no NOTIFY events ever land again, and only the poller's
30-second floor keeps the cache from drifting.

The recommended pattern:

```go
dedicated, err := pool.Acquire(ctx)
if err != nil { return err }
// DO NOT call dedicated.Release() while the listener is running.

pc := policycache.NewPolicyCache[uuid.UUID, db.FederationPolicy](
    dedicated.Conn(),
    "federation_policy_changed",
    refresh,
    parseKey,
    refreshOne,
    policycache.WithPollInterval(30*time.Second),
)
defer dedicated.Release()           // released AFTER pc.Shutdown
defer pc.Shutdown(shutdownCtx)      // runs first (LIFO)
```

## AOID usage example

AOID's `federation_policies` table has a per-row trigger that issues
`NOTIFY federation_policy_changed, '<tenant_id>'` whenever a row is
updated. The AOID consumer side looks like:

```go
type federationPolicyCache struct {
    pc *policycache.PolicyCache[uuid.UUID, db.FederationPolicy]
}

func newFederationPolicyCache(ctx context.Context, pool *pgxpool.Pool, q *db.Queries) (*federationPolicyCache, error) {
    dedicated, err := pool.Acquire(ctx)
    if err != nil { return nil, err }

    refresh := func(ctx context.Context, lastSeen time.Time) error {
        rows, err := q.ListFederationPoliciesUpdatedSince(ctx, lastSeen)
        if err != nil { return err }
        for _, r := range rows {
            fc.pc.Cache().Set(r.TenantID, r)
        }
        return nil
    }
    parseKey := func(payload string) (uuid.UUID, error) {
        return uuid.Parse(payload)
    }
    refreshOne := func(ctx context.Context, tenantID uuid.UUID) error {
        r, err := q.GetFederationPolicy(ctx, tenantID)
        if errors.Is(err, pgx.ErrNoRows) {
            fc.pc.Cache().Invalidate(tenantID)
            return nil
        }
        if err != nil { return err }
        fc.pc.Cache().Set(tenantID, r)
        return nil
    }

    fc := &federationPolicyCache{}
    fc.pc = policycache.NewPolicyCache[uuid.UUID, db.FederationPolicy](
        dedicated.Conn(),
        "federation_policy_changed",
        refresh, parseKey, refreshOne,
    )
    return fc, nil
}

func (fc *federationPolicyCache) Get(tenantID uuid.UUID) (db.FederationPolicy, bool) {
    return fc.pc.Cache().Get(tenantID)
}
```

The same pattern repeats for the other three policy tables in AOID Obj
10 (OAuth policies, auth/MFA/session policies, inactivity policies) —
that four-fold reuse is exactly the reason this primitive lives in
Eden rather than being copy-pasted four times inside AOID.

## Anti-pattern table

| Don't | Do | Why |
|---|---|---|
| Pass a `pgxpool.Pool` or `*pgxpool.Conn` from Acquire that gets released back | Pass `dedicated.Conn()` and keep `dedicated` alive | Pool recycles conns; `LISTEN` subscriptions silently die |
| Treat `NOTIFY` as authoritative ("if NOTIFY fires, the cache is fresh") | Always wire the poller; `WithPollInterval(30s)` is the floor | NOTIFY is lost on conn drops, partitions, listener restarts |
| Use `Cache` as an LRU / TTL cache for hot query results | Keep the working set bounded (per-tenant config; ~10⁴ entries) | No eviction policy; unbounded growth is unbounded RAM |
| Do heavy work inside `onNotify` (HTTP calls, big SQL queries, …) | Make `onNotify` fast; `refreshOne` is already a separate goroutine | Listener loop blocks; Postgres applies NOTIFY backpressure |
| Bury reconnect logic inside `Listen` | Caller decides reconnect on `onError` (knows pool, backoff, retry budget) | Avoids hard-coded backoff policies in a generic primitive |
| Mutate values returned by `Get` in place | Clone before mutating, or write a fresh value via `Set` | Values are not deep-cloned; reader-writer aliasing → races |

## Reconnect strategy

The listener returns and invokes `onError(err)` on unrecoverable
connection failures. The caller decides what to do:

```go
for {
    dedicated, err := pool.Acquire(ctx)
    if err != nil {
        slog.Warn("policycache: acquire failed; retrying", "error", err)
        time.Sleep(backoff.Next())
        continue
    }
    pc := policycache.NewPolicyCache(dedicated.Conn(), ...)
    <-pc.shutdown // implementation detail; expose your own done-chan
    dedicated.Release()
    if ctx.Err() != nil { return }
    // loop and reconnect
}
```

In practice most callers wire the reconnect loop at service boot and
treat NOTIFY drops as rare events — the poller catches anything missed
within the poll interval, so reconnect urgency is "minutes" not
"milliseconds".

## Memory budget

At ~1KB per cached value (typical policy row JSON) and ~10⁴ entries,
total resident memory is ~10MB per cache. AOID's Obj 10 wires four
caches per replica → ~40MB. Negligible inside any production service
RSS budget.

## Race: NOTIFY firing while poller is mid-refresh

This race is documented but not problematic. `refresh` (the poller)
writes via `cache.Replace` or many `cache.Set`; `refreshOne` (the
listener) writes via a single `cache.Set` / `cache.Invalidate`. The
cache's `sync.RWMutex` serializes all writes; one key may be Set twice
(once by each) but no value is corrupted and the final read returns
the most-recent write. The integration test
`TestPolicyCache_ConcurrentNotifyAndPoll` exercises this scenario
under `-race` with no failures.

## Future extensions (out of scope for v1)

- Sharded cache for >10⁵ entries (would require partitioning by `K`).
- LRU eviction (the current model assumes the working set is bounded).
- Built-in reconnect loop (preserves caller-supplied retry policy).
- Multi-channel listener (currently one listener per channel).

## Testing

```bash
# Unit tests — no Postgres required.
go test ./platform/policycache/... -race -v -count=1

# Integration tests — require DATABASE_URL.
DATABASE_URL=postgres://localhost/eden_dev?sslmode=disable \
  go test -tags integration ./platform/policycache/... -race -v -count=1
```

Integration tests follow the `platform/audit/postgres_buffer_test.go`
convention: `//go:build integration` tag, skip if `DATABASE_URL` is
unset, unique channel names per test to avoid cross-test bleed under
concurrent runs.
