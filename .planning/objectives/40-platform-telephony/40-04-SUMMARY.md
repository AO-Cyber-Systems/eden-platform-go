---
objective: 40-platform-telephony
trd: "04"
subsystem: telephony
tags: [resolver, caching, multi-tenant, entitlements-seam]

# Dependency graph
requires:
  - objective: 40-platform-telephony
    provides: "TRD 01 — Provider interface, NoopProvider, Registry, Config/value types, ProviderSignalWire/ProviderTwilio, CompanyID rename (platform/telephony/models.go, registry.go, provider.go)"
  - objective: 40-platform-telephony
    provides: "TRD 03 — ConfigStore interface (Get/LookupBySendingNumber/Upsert/Deactivate/List), ErrTenantNotConfigured (platform/telephony/config_store.go)"
provides:
  - "Resolver: Resolver.For(ctx, companyID) resolves a tenant to its Provider through a 4-tier ladder (tenant config -> platform default -> caller fallback -> NoopProvider), TTL-cached"
  - "EntitlementChecker — a package-local interface (no platform/entitlements import); noEntitlementCheck default allows everything so the package compiles and passes tests with zero entitlements wiring"
  - "PlatformDefaultStore — a package-local interface a consumer wires via WithPlatformDefaults to supply an org-wide default provider"
  - "Invalidate(companyID) / InvalidateAll() — cache-busting so a credential rotation takes effect without a process restart"
affects: ["40-05", "40-06", "40-07"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Locally-defined narrow interfaces (EntitlementChecker, PlatformDefaultStore) to avoid an import cycle / hard dependency on sibling packages — the resolver never imports platform/entitlements; a consumer supplies a concrete adapter at wiring time"
    - "TTL cache keyed by tenant ID with an injectable clock (r.now func() time.Time) so tests can advance time deterministically instead of sleeping"
    - "4-tier fallback ladder where the final tier (caller-supplied fallback) is NEVER short-circuited away, even when an optional middle tier (platform default) is wired but empty"

key-files:
  created:
    - platform/telephony/resolver.go
    - platform/telephony/resolver_test.go
  modified: []

key-decisions:
  - "Ported 1:1 from politihub/go/internal/telephony/resolver.go with committeeID -> companyID (and CommitteeID -> CompanyID on Config) as the only functional rename — Waves 1-2 had already renamed the Config field, so this TRD's diff is purely the resolver's own parameter/variable names plus doc comments that referenced 'committee'."
  - "Did NOT redefine stubProvider in resolver_test.go — TRD 01's registry_test.go already defines a package-level stubProvider satisfying Provider, so resolver_test.go reuses it (avoids a duplicate-symbol compile error that the source repo doesn't have because its stubProvider lives in registry_test.go there too, just not read as a merge conflict risk here)."
  - "Kept the PlatformDefaultStore method name TwilioPlatformDefault verbatim rather than genericizing it — the TRD's rename scope was committeeID/CompanyID only; broadening the interface shape was out of scope for a 'lift + rename' port and stays a call a future TRD can make if a non-Twilio platform default is ever needed."
  - "Generalized doc comments that referenced politihub-specific concepts (TELE-10, Objective 27/INTEG-04, 'Maine GOP tenant', main.go TRD 27-04) into eden-neutral language describing the same mechanics, since those cross-references don't exist in this codebase."

patterns-established: []

requirements-completed: [R46]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~15min (single agent turn, no interruption)
completed: 2026-09-07
---

# Objective 40 TRD 04: Telephony Resolver Summary

Ported the tenant-to-Provider `Resolver` from politihub's `internal/telephony` into
`platform/telephony`, renaming `committeeID` to `companyID` throughout (the `Config`
field itself was already renamed in Waves 1-2). The resolver caches a per-tenant
`Provider` for a configurable TTL, walks a 4-tier fallback ladder (tenant config →
optional platform-wide default → caller-supplied fallback → `NoopProvider`), and gates
SignalWire behind a package-local `EntitlementChecker` interface so `platform/telephony`
never hard-depends on `platform/entitlements`.

## What Was Built

- **`Resolver.For(ctx, companyID)`** — cache-first lookup; on a cache miss, calls
  `ConfigStore.Get`. `ErrTenantNotConfigured` falls through to `platformDefault()`
  (if a `PlatformDefaultStore` is wired via `WithPlatformDefaults`) and then to the
  fallback `Provider` supplied at construction — the fallback tier is never
  short-circuited away, even when a platform-default store is wired but returns
  no default. A resolved SignalWire config is checked against `EntitlementChecker`
  before the `Registry` builds it; a disallowed tenant gets `ErrTierNotAllowed`.
- **`EntitlementChecker`** — `Allowed(ctx, companyID, featureKey) (bool, string, error)`,
  defined locally. `noEntitlementCheck` (the default when `NewResolver` receives `nil`)
  always allows, so the package compiles and its full test suite passes with no
  entitlements wiring anywhere.
- **`PlatformDefaultStore`** — `TwilioPlatformDefault(ctx) (Config, error)`, also
  defined locally. `nil` (never calling `WithPlatformDefaults`) disables the tier
  entirely, preserving pre-platform-default behavior.
- **`Invalidate(companyID)`** and **`InvalidateAll()`** — the former drops one cache
  entry (used after an admin upserts a per-tenant config); the latter replaces the
  whole cache map (used after a platform-default write, since companies resolving
  via that tier are cached under their own IDs and a targeted `Invalidate` can't
  reach them).
- **`resolver_test.go`** ported intact: TTL caching/expiry (with an injectable
  `r.now` clock), the full 4-tier fallback-ordering matrix, `Invalidate` /
  `InvalidateAll` cache-busting, and the SignalWire tier-gate (blocked/allowed/
  not-checked-for-Twilio) cases.

## Deviations from Plan

None — TRD executed exactly as written. `stubProvider` was reused from TRD 01's
`registry_test.go` rather than redeclared, which is a mechanical consequence of the
lift (the source repo's `stubProvider` also lives in a sibling test file), not a
deviation from the TRD's file scope (`resolver.go`, `resolver_test.go` only).

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Port resolver.go + resolver_test.go, rename companyID | `go build ./platform/telephony/...` | 0 | PASS |
| 1: Port resolver.go + resolver_test.go, rename companyID | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build | `go build ./platform/telephony/...` | 0 | PASS |
| vet | `go vet ./platform/telephony/...` | 0 | PASS |
| test+race | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |
| gofmt | `gofmt -l resolver.go resolver_test.go` | 0 (no output) | PASS |
| dep-boundary | `go list -deps ./platform/telephony \| grep -i entitlements` | 1 (no match) | PASS |

## Post-TRD Verification

- Auto-fix cycles used: 0
- Must-haves verified: 4/4
  - `Resolver.For` TTL-cached, tenant → platform default → NoopProvider ladder: proved by `TestResolver_CompanyRowBeatsPlatformDefault`, `TestResolver_PlatformDefaultWhenNoCompanyRow`, `TestResolver_FallbackWhenNoPlatformDefault`, `TestResolver_NilPlatformStorePreservesBehavior`, `TestResolver_NoFallbackReturnsNoop`, `TestResolver_CachesWithinTTL`, `TestResolver_ExpiresAfterTTL`
  - `EntitlementChecker` is package-local with no `platform/entitlements` dependency: proved by `go list -deps` (below) plus `TestResolver_TierGate_*` passing with `nil` and with `fakeEnt`
  - `PlatformDefaultStore` supplies an org-wide default: proved by `TestResolver_PlatformDefaultWhenNoCompanyRow`
  - `Invalidate` / `InvalidateAll` bust the cache: proved by `TestResolver_InvalidateBustsCache`, `TestResolver_InvalidateAllClearsCache`
- Gate failures: None

### Verify checks (raw output)

**`go test ./platform/telephony/... -count=1 -race`:**
```
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	1.969s
```

**Fallback-ladder + Invalidate tests (verbose, subset):**
```
=== RUN   TestResolver_CompanyRowBeatsPlatformDefault
--- PASS: TestResolver_CompanyRowBeatsPlatformDefault (0.00s)
=== RUN   TestResolver_PlatformDefaultWhenNoCompanyRow
--- PASS: TestResolver_PlatformDefaultWhenNoCompanyRow (0.00s)
=== RUN   TestResolver_FallbackWhenNoPlatformDefault
--- PASS: TestResolver_FallbackWhenNoPlatformDefault (0.00s)
=== RUN   TestResolver_NilPlatformStorePreservesBehavior
--- PASS: TestResolver_NilPlatformStorePreservesBehavior (0.00s)
=== RUN   TestResolver_InvalidateAllClearsCache
--- PASS: TestResolver_InvalidateAllClearsCache (0.00s)
=== RUN   TestResolver_InvalidateBustsCache
--- PASS: TestResolver_InvalidateBustsCache (0.00s)
PASS
```

**`go list -deps ./platform/telephony | grep -i entitlements`:** (no output — confirmed absent)

## Self-Check: PASSED

- FOUND: platform/telephony/resolver.go
- FOUND: platform/telephony/resolver_test.go
- FOUND commit: eb04006
