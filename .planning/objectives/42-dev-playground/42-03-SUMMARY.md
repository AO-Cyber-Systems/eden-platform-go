---
objective: 42-dev-playground
job: "03"
subsystem: dev-tooling
tags: [connect-rpc, curl, quickstart, dev-server, justfile, auth]

# Dependency graph
requires:
  - objective: 42-dev-playground (TRD 01)
    provides: seeded demo tenant (company/user/membership/owner role) via seedDevTenant
  - objective: 42-dev-playground (TRD 02)
    provides: real dev session minting via auth.Service.Login + guardAgainstProductionConfig
provides:
  - "cmd/eden-platform-dev/quickstart.go: BuildQuickstartCurl / BuildQuickstartBanner / printQuickstart"
  - "A startup banner naming the seeded identity, token, base URL, and a working curl"
  - "A live-server test proving the printed curl actually returns 2xx against CompanyService.GetCompany"
  - "eden-platform-go's first repo-scoped justfile (dev-quickstart recipe)"
affects: [42-dev-playground TRD 04 (README/integration docs)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dev-only build tag pair (//go:build dev + //go:build !dev stub), continuing the pattern from TRDs 01-02"
    - "Curl-builder-as-pure-function: BuildQuickstartCurl/BuildQuickstartBanner take plain string args so they can be unit-tested without a server AND reused verbatim by the live-server test — one code path generates both the printed text and the test's request"

key-files:
  created:
    - cmd/eden-platform-dev/quickstart.go
    - cmd/eden-platform-dev/quickstart_stub.go
    - cmd/eden-platform-dev/quickstart_test.go
    - justfile
  modified:
    - cmd/eden-platform-dev/main.go

key-decisions:
  - "eden-platform-go had NO justfile before this TRD. The TRD's phrase 'root justfile' referred to eden-libs' workspace-root justfile, but the orchestrator's explicit CRITICAL_REPO_TARGETING instruction forbids touching eden-libs from this worktree. Created a NEW repo-scoped justfile in eden-platform-go itself, with a `dev-quickstart` recipe following the parent's `dev-*` naming convention, and a header comment explaining the relationship to the eden-libs root justfile."
  - "The printed curl targets CompanyService.GetCompany: it requires a valid bearer token (not in server.DefaultPublicProcedures) but carries no entry in main.go's defaultProcedurePermissions map, so any authenticated tenant member — not only the owner — can call it. Its only input is DevTenantCompanyID, already fixed by TRD 01's seeding."
  - "printQuickstart performs its own auth.Service.Login call rather than reusing devsession.go's mintDevSession/DevSession, to keep quickstart.go's file ownership independent of TRD 02's devsession.go (this TRD's file_ownership explicitly did not include devsession.go). A second real Login against the in-memory devstore is inexpensive and exercises the production login path a second time."
  - "The curl-executing test shells out to a real `curl` binary via exec.Command against a real httptest.Server (not a hand-simulated HTTP request), because the TRD requires the test to execute the PRINTED command, not merely assert its shape."

patterns-established:
  - "Any future printed/copy-pasteable command in this codebase should follow the same shape: a pure string-building function taking plain args, reused unmodified by both the print path and the test that executes it."

requirements-completed: [R48]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~55min
completed: 2026-09-08
---

# Objective 42 TRD 03: One-Command Quickstart Summary

**Startup banner + `BuildQuickstartCurl`/`BuildQuickstartBanner` in `cmd/eden-platform-dev/quickstart.go`, proven live by a test that shells out `curl` against a real in-process server and asserts 2xx on `CompanyService.GetCompany`.**

## Performance

- **Duration:** ~55 min
- **Tasks:** 4 deliverables (banner/curl builder + wiring, live-curl test, justfile, this SUMMARY)
- **Files modified:** 5 (4 created, 1 modified)

## Accomplishments

- `BuildQuickstartCurl(baseURL, token, companyID)` renders the exact copy-pasteable curl; `BuildQuickstartBanner(...)` embeds that same string (no hand-typed duplicate) alongside the seeded identity, base URL, and the required dev-only/in-memory/data-loss notice.
- `printQuickstart` logs in the seeded dev tenant user through the real `auth.Service.Login` path and prints the banner to stdout; wired into `main.go` right after `startDevSession`, guarded by the standard `//go:build dev` / `//go:build !dev` stub pair.
- `TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx` starts a real `httptest.Server` wired with the real `auth.JWTManager`, real auth + RBAC ConnectRPC interceptors, and the real `CompanyHandler`; mints a real token via `auth.Service.Login`; builds the curl with the SAME `BuildQuickstartCurl` function the banner uses; executes it with a real `curl` subprocess; asserts HTTP 2xx and that the response body contains the seeded company name.
- `TestQuickstartCurl_InvalidTokenIsRejected` is the negative control proving the 2xx result above is not an artifact of auth being disabled.
- Created eden-platform-go's first justfile with a `dev-quickstart` recipe (`SERVER_ADDR=127.0.0.1:8091 go run -tags dev ./cmd/eden-platform-dev`).

## Closing TRD 02's gap

TRD 02 left an honest gap: the `/dev/session` endpoint and the startup log line were compile- and unit-verified but never exercised against a live, running server.

**This TRD's live-server test closes the login-path half of that gap, but not the `/dev/session` HTTP endpoint itself.** `TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx` proves, against a real in-process server, that a token minted via the real `auth.Service.Login` path is accepted by the real auth + RBAC interceptors and lets an authenticated call through to a real handler — the same login mechanism `mintDevSession` uses. It does **not** start a server that mounts `registerDevSessionEndpoint`/`DevSessionEndpoint` (`/dev/session`) and issue an HTTP GET against that literal path, because doing so would have required either importing `devsession.go`'s `DevSession`/`registerDevSessionEndpoint` (outside this TRD's file ownership) or duplicating them. So: the **login → token → real interceptors → real handler** path is now proven live; the **literal `/dev/session` GET endpoint** is still only unit-verified (per TRD 02's SUMMARY), not exercised end-to-end. A future TRD (04, or a small follow-up) that owns/extends `devsession.go` could close that remaining slice by adding a live GET against `/dev/session` in the full `runServer` wiring.

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Curl/banner builder + main.go wiring | `go build ./... && go build -tags dev ./...` | 0 | PASS |
| 2: Live-server curl test | `go test -tags dev ./cmd/eden-platform-dev/... -run TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx -v -count=1` | 0 | PASS |
| 3: justfile recipe | `just --list` (parses; `dev-quickstart` recipe listed) | 0 | PASS |
| 4: Full suite | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS |

## Task Commits

1. **quickstart banner + curl builder, wired at startup** - `7b573d5` (feat)
2. **printed curl executed against a live server, asserts 2xx** - `c8022a7` (test)
3. **repo-scoped justfile with dev-quickstart recipe** - `2f91275` (chore)

**Plan metadata:** this SUMMARY commit (docs)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (no tag) | `go build ./...` | 0 | PASS |
| build (dev tag) | `go build -tags dev ./...` | 0 | PASS |
| test + race | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS |
| justfile syntax | `just --list` | 0 | PASS |

Full paste of the final `go build ./... && go build -tags dev ./...` and `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` runs (both exit 0, all 11 tests PASS) is in this TRD's final chat report to the orchestrator.

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 4/4
  - One command starts the playground with a seeded tenant and a ready session — `just dev-quickstart`
  - Startup prints identity, token, base URL, and a working curl — `BuildQuickstartBanner` / `printQuickstart`
  - The printed curl is correct, proven by a test executing it against a running server — `TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx`
  - The banner states dev-only/in-memory/data-loss — `TestBuildQuickstartBanner_NamesDevOnlyInMemoryDataLoss`
- **Gate failures:** None

## Files Created/Modified

- `cmd/eden-platform-dev/quickstart.go` - `BuildQuickstartCurl`, `BuildQuickstartBanner`, `printQuickstart` (dev-tagged)
- `cmd/eden-platform-dev/quickstart_stub.go` - `!dev` no-op stub for `printQuickstart`
- `cmd/eden-platform-dev/quickstart_test.go` - unit tests for the builders + the live-server curl-execution test + negative control
- `cmd/eden-platform-dev/main.go` - one call-site addition: `printQuickstart(cfg, authService)` after `startDevSession`
- `justfile` - new, repo-scoped, `dev-quickstart` recipe

## Decisions Made

See `key-decisions` in frontmatter above (justfile placement, GetCompany choice, independent Login call, real-curl-subprocess test).

## Deviations from Plan

### Auto-fixed / Necessary Deviations

**1. [Deviation - Scope Correction] justfile created in eden-platform-go, not the workspace-root justfile the TRD named**
- **Found during:** Task 3 (justfile recipe)
- **Issue:** The TRD's `key_links` said "Root justfile already has dev-* recipes; follow their style" and its `<verify>` block expects `just <recipe>` to work. The only justfile with `dev-*` recipes (`dev-go`, `dev-ui`, `dev-web`, `dev-docs`) lives in `eden-libs/justfile` — a separate repo. eden-platform-go itself has no justfile at all.
- **Fix:** Per the orchestrator's explicit `CRITICAL_REPO_TARGETING` instruction ("Work ONLY in eden-platform-go. NOT eden-libs"), created a new justfile scoped to eden-platform-go's own root, with a `dev-quickstart` recipe following the `dev-*` naming convention and a header comment explaining that eden-libs' root justfile holds the cross-package recipes. Confirmed with `just --list` that the new justfile parses and lists the recipe correctly.
- **Files modified:** `justfile` (new)
- **Verification:** `just --list` output shows `dev-quickstart` with its doc comment; recipe body is a single `SERVER_ADDR=127.0.0.1:8091 go run -tags dev ./cmd/eden-platform-dev` line.
- **Committed in:** `2f91275`
- **Not done:** did NOT start the server via `just dev-quickstart` and poll it live in this session — per the coordinator's explicit instruction, the live-server verification already performed by `TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx` (which starts an equivalent real server in-process and hits it with a real `curl` subprocess) was treated as the check that matters, to avoid burning turn budget on manual server polling.

---

**Total deviations:** 1 (justfile scope correction, required by explicit orchestrator repo-targeting constraint)
**Impact on plan:** No scope creep — the deviation is a repository-boundary correction, not a feature change. The `dev-quickstart` recipe does what the TRD's verify block asks (`SERVER_ADDR` set to loopback `:8091`, `-tags dev`, runs `./cmd/eden-platform-dev`); it just lives one directory up from where the TRD assumed a justfile already existed.

## Issues Encountered

None beyond the justfile-location deviation above.

## User Setup Required

None - no external service configuration required. `curl` and `just` were both already present on the operator's machine (confirmed via `command -v`).

## Next Objective Readiness

- TRD 04 (README/integration docs for the playground) can proceed. It should be aware of the remaining `/dev/session` live-endpoint gap noted above (login path is now proven live; the literal `GET /dev/session` HTTP path is still only unit-verified) if it wants to document coverage precisely, or close it with a small addition.
- No blockers.

---
*Objective: 42-dev-playground*
*Completed: 2026-09-08*
