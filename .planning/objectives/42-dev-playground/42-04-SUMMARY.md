---
objective: 42-dev-playground
job: "04"
subsystem: dev-tooling
tags: [connect-rpc, integration-test, readme, auth, rbac, dev-server]

# Dependency graph
requires:
  - objective: 42-dev-playground (TRD 01)
    provides: seeded demo tenant (company/user/membership/owner role) via seedDevTenant
  - objective: 42-dev-playground (TRD 02)
    provides: real dev session minting via auth.Service.Login, guardAgainstProductionConfig, GET /dev/session
  - objective: 42-dev-playground (TRD 03)
    provides: startup banner + working curl, first repo-scoped justfile (dev-quickstart)
provides:
  - "cmd/eden-platform-dev/README.md: what the playground is/is not, the safety model, the live-authorization proof, deferred cross-repo work"
  - "cmd/eden-platform-dev/integration_test.go: start -> seeded session -> authenticated OK -> unauthorized DENIED, against a live in-process server"
  - "Closure of TRD 03's flagged gap — GET /dev/session is now exercised end-to-end, not just the login path"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Integration test mounts the REAL startDevSession entry point (not a reimplementation) on a plain httptest.Server, then issues a real net/http GET against the literal /dev/session route — closing a gap two prior TRDs' file ownership boundaries left open"

key-files:
  created:
    - cmd/eden-platform-dev/README.md
    - cmd/eden-platform-dev/integration_test.go
  modified: []

key-decisions:
  - "The 'unauthorized call is DENIED' leg of the integration test uses a genuinely under-privileged, but validly authenticated, viewer-role user (CodePermissionDenied) rather than a missing-token case (CodeUnauthenticated) — the stronger and more meaningful proof that 'authorization is live, not exempted,' and the one TestUpdateCompany_NoTokenIsUnauthenticated (TRD 02) does not already cover as an end-to-end /dev/session flow."
  - "Reused seedUnderPrivilegedUser and updateCompanyRequest from devsession_test.go directly (same package, same build tag) instead of redefining them — avoids duplicate fixtures drifting from the already-proven RBAC-deny pattern."
  - "Added a second, smaller test (TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests) proving GET /dev/session serves one pre-minted session across repeated hits, not a fresh mint per request — a property a real zero-interaction client depends on and that was previously unverified."
  - "README explicitly credits the pre-existing -db=false in-memory path to before this objective, per the objective's established facts — the objective added the tenant and session, not the in-memory server itself."

patterns-established:
  - "Cross-TRD gap closure: when file ownership boundaries in earlier TRDs leave an endpoint's literal HTTP route unexercised, a later TRD with no such ownership restriction should close it directly against the real entry-point function, not a hand-simulated equivalent."

requirements-completed: [R48]

# Verification evidence
verification:
  gates_defined: 3
  gates_passed: 3
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~35min
completed: 2026-09-08
---

# Objective 42 TRD 04: README + Integration Test Summary

**Dev playground README documenting the safety model and live-authorization guarantee, plus an integration test that closes TRD 03's flagged `GET /dev/session` gap by hitting the literal HTTP route on a live server.**

## Performance

- **Duration:** ~35 min
- **Tasks:** 2 deliverables (integration test, README) + this SUMMARY
- **Files modified:** 2 created

## Accomplishments

- `integration_test.go` stands up a live `httptest.Server` wired the same way `main.go`'s `runServer` wires the in-memory path, and — the piece no earlier TRD's test exercised — mounts `/dev/session` through the **real** `startDevSession` entry point (the exact function `main.go` calls at boot), not a reimplementation.
- `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied` proves the full required chain in one test: a real `net/http` `GET /dev/session` returns a usable session; that session's token is accepted by the real auth+RBAC interceptors for an `UpdateCompany` call the owner genuinely has permission for; a second, genuinely under-privileged (viewer-role) user authenticated through the real login path is rejected with `CodePermissionDenied`.
- `TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests` proves the endpoint serves one pre-minted session across repeated hits, not a fresh mint per request.
- `README.md` states what the playground IS (a real server + real interceptors over an in-memory backend, pre-seeded, pre-authenticated) and what it is NOT (not staging, not durable, not reachable off-box, not present in a production binary), documents the three-layer safety model (build-tag isolation, `guardAgainstProductionConfig`, zero `platform/` dependency), states plainly that authorization is live (not exempted) with concrete evidence, records that the in-memory path predates this objective, documents that the `/dev/session` gap is now closed, and names both deferred cross-repo pieces (eden-docs try-it bridge, Flutter entry point) plus the in-repo deferred item (`devstore` backs 5 of 20 platform `Store` interfaces).

## Closing TRD 03's gap

**CLOSED.** TRD 03's live-server test (`TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx`) proved the login path — `auth.Service.Login` → real interceptors → real handler — works end-to-end, but never issued a request against the literal `GET /dev/session` HTTP route, because `DevSession`/`startDevSession`/`registerDevSessionEndpoint` live in `devsession.go`, outside TRD 03's file ownership.

`integration_test.go` (this TRD) owns no such restriction. `setupIntegrationTestEnv` calls the real `startDevSession(cfg, authService, mux)` — the identical function `main.go` invokes at boot — to mount `/dev/session` on a live `httptest.Server`, and `fetchDevSession` issues a real `http.Get` against that literal route, decoding a usable `DevSession`. That token is then used exactly as a zero-interaction client would use it: one successful authenticated call, one correctly-denied unauthorized call. The gap TRD 03 flagged is closed by this TRD.

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Integration test (`integration_test.go`) | `go test -tags dev ./cmd/eden-platform-dev/... -run TestIntegration -v -count=1` | 0 | PASS |
| 2: README (`README.md`) | manual review against must-have truths (no runnable verify — documentation) | n/a | PASS |
| 3: Full suite + both build modes | `go build ./... && go build -tags dev ./...` then `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS |

## Task Commits

1. **Integration test: live GET /dev/session against real server, closes TRD 03's gap** - `d029e24` (test)
2. **README: boundaries, safety model, live-auth proof** - `d540b3f` (docs)

**Plan metadata:** this SUMMARY commit (docs)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (no tag) | `go build ./...` | 0 | PASS |
| build (dev tag) | `go build -tags dev ./...` | 0 | PASS |
| test + race (full package, 15 tests) | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS |

Full paste of the final runs:

```
$ go build ./...
EXIT:0

$ go build -tags dev ./...
EXIT:0

$ go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/cmd/eden-platform-dev	5.282s
EXIT:0
```

All 15 tests in the package pass under `-race`, including the two new integration tests:

```
--- PASS: TestSeedDevTenant_CoherentIdentity (0.39s)
--- PASS: TestSeedDevTenant_Idempotent (0.19s)
--- PASS: TestMintDevSession_AcceptedByRealAuthInterceptor (0.40s)
--- PASS: TestUpdateCompany_NoTokenIsUnauthenticated (0.20s)
--- PASS: TestUpdateCompany_UnderPrivilegedUserIsDenied (0.58s)
--- PASS: TestGuardAgainstProductionConfig (0.00s)
--- PASS: TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied (0.80s)
--- PASS: TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests (0.41s)
--- PASS: TestBuildQuickstartCurl_Format (0.00s)
--- PASS: TestBuildQuickstartBanner_NamesDevOnlyInMemoryDataLoss (0.00s)
--- PASS: TestBuildQuickstartBanner_ContainsCurl (0.00s)
--- PASS: TestPrintQuickstart_WritesBannerForSeededUser (0.39s)
--- PASS: TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx (0.41s)
--- PASS: TestQuickstartCurl_InvalidTokenIsRejected (0.22s)
PASS
```

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - README explains what the playground IS and is NOT (in-memory, dev-only, not staging) — `README.md` "What this IS" / "What this is NOT"
  - README documents the safety model (build tag, refuse-to-start guard, nothing under `platform/` imports it) — `README.md` "Safety model", verified live: `grep -rn "eden-platform-dev" platform/` returns nothing
  - README states authorization is LIVE, not exempted — `README.md` "Authorization is LIVE, not exempted", backed by `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied`'s CodePermissionDenied assertion
  - Integration test covers start → seeded session → authenticated call succeeds → unauthorized call DENIED — `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied`
  - README names the deferred cross-repo pieces (eden-docs try-it bridge, Flutter entry point) — `README.md` "What's deferred"
- **Gate failures:** None

## Files Created/Modified

- `cmd/eden-platform-dev/integration_test.go` - `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied`, `TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests`, `setupIntegrationTestEnv`, `fetchDevSession` (dev-tagged)
- `cmd/eden-platform-dev/README.md` - what/what-not, safety model, live-auth proof, gap-closure record, deferred work, file map

## Decisions Made

See `key-decisions` in frontmatter above (RBAC-deny over missing-token for the "unauthorized" leg, fixture reuse, the stability test, and the README's pre-existing-in-memory-path credit).

## Deviations from Plan

None — TRD executed exactly as written. Both files stayed within the declared `file_ownership` (`cmd/eden-platform-dev/{README.md,integration_test.go}`); `platform/` and `go.mod` were not touched.

## Issues Encountered

None.

## User Setup Required

None — no external service configuration required. Both build modes and the full test suite (including `-race`) were run and pass in this environment.

## Next Objective Readiness

Objective 42-dev-playground's four TRDs are now all complete:

- TRD 01: seeded demo tenant
- TRD 02: real dev session + `GET /dev/session` + refuse-to-start guard
- TRD 03: startup banner, working curl, first repo-scoped justfile
- TRD 04 (this TRD): README + integration test, closing the `/dev/session` end-to-end gap

Per this worktree's `worktree_protocol`, objective-level bookkeeping (STATE.md/ROADMAP.md) is left to the coordinator — not touched by this TRD.

No blockers. No open gaps remain from TRD 03's SUMMARY.

## Self-Check: PASSED

- FOUND: `cmd/eden-platform-dev/README.md`
- FOUND: `cmd/eden-platform-dev/integration_test.go`
- FOUND: `.planning/objectives/42-dev-playground/42-04-SUMMARY.md`
- FOUND commit: `d029e24` (integration test)
- FOUND commit: `d540b3f` (README)

---
*Objective: 42-dev-playground*
*Completed: 2026-09-08*
