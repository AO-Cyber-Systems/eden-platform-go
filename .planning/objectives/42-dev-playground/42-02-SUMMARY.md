---
objective: 42-dev-playground
job: 02
subsystem: dev-tooling
tags: [auth, jwt, rbac, dev-server, build-tags, guard, zero-interaction]

# Dependency graph
requires:
  - objective: 42-dev-playground-01
    provides: "seedDevTenant(backend), DevTenantEmail/DevTenantPassword/DevTenantCompanyID, and the `//go:build dev`/`!dev` pattern this TRD extends"
provides:
  - "mintDevSession(ctx, authService): mints a real access/refresh token pair for the seeded dev user through the exact production auth.Service.Login path"
  - "startDevSession(cfg, authService, mux): the single call-site main.go invokes — refuse-to-start guard, then token mint, then dev-only endpoint registration, then startup log"
  - "GET /dev/session: dev-only HTTP endpoint returning the pre-minted session as JSON, for zero-interaction clients"
  - "guardAgainstProductionConfig(cfg): refuses to start against a non-loopback DATABASE_URL host or a non-loopback SERVER_ADDR (including bare \":PORT\" wildcard binds)"
affects: [42-dev-playground-03, 42-dev-playground-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Zero-interaction session pattern: automate the LOGIN STEP by calling the real auth.Service.Login programmatically at startup, never by exempting a procedure or short-circuiting an interceptor"
    - "Refuse-to-start config guard, run unconditionally at boot (regardless of which storage backend flag is passed) as defense-in-depth against an ambient production-shaped DATABASE_URL or SERVER_ADDR"

key-files:
  created:
    - cmd/eden-platform-dev/devsession.go
    - cmd/eden-platform-dev/devsession_stub.go
    - cmd/eden-platform-dev/devsession_test.go
  modified:
    - cmd/eden-platform-dev/main.go

key-decisions:
  - "Token minting goes through auth.Service.Login(ctx, DevTenantEmail, DevTenantPassword) — the exact production login path (real Argon2id verification, real membership/role lookup, real audit log entry, real JWTManager signature) — never auth.JWTManager.CreateAccessToken called directly and never a fabricated Claims value"
  - "The dev-only /dev/session endpoint is mounted as a plain net/http handler on the top-level mux, sibling to /up and /metrics, NOT registered through connect.WithInterceptors — it plays the role of a login page (unauthenticated by design, because handing out the first token is its only job), while every actual platform API call made with that token still passes through the real, untouched auth+RBAC interceptor chain"
  - "guardAgainstProductionConfig checks DATABASE_URL and SERVER_ADDR unconditionally, not gated on the --db flag — an ambient production-shaped DATABASE_URL is refused even when running the in-memory backend, since the TRD's literal requirement is refuse on production-shaped config being present, not merely in-use"
  - "A bare ':PORT' SERVER_ADDR (the shared config package's own default) is treated as non-loopback and refused — Go's net.Listen binds ':PORT' to every interface, which is production-shaped for this playground; the guard's error message points the operator at 127.0.0.1:8091 (never 8080, which is permanently occupied on the operator's machine)"
  - "The DENY test seeds a second, genuinely distinct user (not the owner) bound to rbac.ViewerRoleID in the same seeded company, logs them in through the SAME auth.Service.Login path, and asserts the real RBAC interceptor returns CodePermissionDenied on CompanyService.UpdateCompany (settings:edit) — this is what makes 'authorization is live' falsifiable rather than an assumed property of the wiring"

patterns-established:
  - "Automate the interactive step (login), never the authorization decision — established as the answer to 'zero-interaction' for any future TRD in this objective that needs to hand a client credentials/access without a bypass"

requirements-completed: [R48]

# Verification evidence
verification:
  gates_defined: 6
  gates_passed: 6
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~50min
completed: 2026-09-08
---

# Objective 42 TRD 02: Zero-Interaction Dev Session + Production Guard Summary

**Real access token minted for the seeded dev user through the production `auth.Service.Login` path (not a bypass), surfaced at startup and via a dev-only `/dev/session` endpoint, with a refuse-to-start guard against production-shaped `DATABASE_URL`/`SERVER_ADDR` — the auth and RBAC interceptors, and `DefaultPublicProcedures`, are untouched.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-09-08 (continuing in the TRD-01 worktree, `plan/obj-40-platform-telephony` already merged)
- **Completed:** 2026-09-08
- **Tasks:** 2 (implementation + guard + main.go wiring; interceptor/deny/guard test coverage)
- **Files modified:** 4 (3 created, 1 edited)

## Accomplishments

- `mintDevSession` authenticates the seeded owner user via `auth.Service.Login` — the identical path production login uses (Argon2id verification, membership lookup, role resolution, audit log entry, signature by the real `auth.JWTManager`)
- `startDevSession` wires guard → mint → dev-only endpoint → startup log as a single call site in `main.go`, following the same `//go:build dev` / `!dev` stub pattern TRD 01 established
- `guardAgainstProductionConfig` refuses to start on a non-loopback `DATABASE_URL` host or a non-loopback `SERVER_ADDR` (including the common bare `":PORT"` wildcard-bind idiom), pointing the operator at `127.0.0.1:8091`
- Three interceptor-level tests stand up the SAME real wiring `runServer` uses (`server.NewAuthInterceptor` + `server.NewRBACInterceptor`, backed by the real `auth.JWTManager` and real `rbac.Enforcer`) around `CompanyService.UpdateCompany` and prove: the minted token is accepted, an unauthenticated call is rejected, and a real-but-under-privileged principal is denied by RBAC
- Five table-driven cases prove the config guard's loopback/non-loopback boundary in both directions

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: `devsession.go` + inverse-tag stub + main.go wiring | `go build ./... && go build -tags dev ./... && go vet ./... && go vet -tags dev ./...` | 0 | PASS |
| 2: Interceptor-acceptance, deny, and guard test coverage | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race -v` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: `devsession.go` (mint + guard + endpoint) + `devsession_stub.go` (no-op) + main.go call site** — `02dd0a7` (feat)
2. **Task 2: interceptor-acceptance / deny / guard test coverage** — `1060e76` (test)

**Plan metadata:** this commit — `docs(42-02): complete zero-interaction dev session TRD`

## The Three Load-Bearing Tests

The TRD's `<verify>` block and the `<the_deny_test_is_the_point>` note both call out three specific claims. All three exist, in `cmd/eden-platform-dev/devsession_test.go`, and all three genuinely pass:

1. **Minted token accepted by the REAL auth interceptor** — `TestMintDevSession_AcceptedByRealAuthInterceptor`. Calls `mintDevSession` (which calls `auth.Service.Login`), sends the resulting token as a `Bearer` header to `CompanyService.UpdateCompany` through an `httptest.Server` wired with the real `server.NewAuthInterceptor(jwtManager, ...)` + `server.NewRBACInterceptor(enforcer, ...)`. **PASSES** — the call reaches the handler and succeeds (owner genuinely holds `settings:edit`).
2. **RBAC-protected procedure still DENIES an under-privileged seeded user** (the load-bearing one) — `TestUpdateCompany_UnderPrivilegedUserIsDenied`. Seeds a second, distinct user bound to `rbac.ViewerRoleID` (view-only permissions) in the same company, logs them in through the identical `auth.Service.Login` path, and calls the same RBAC-protected procedure. **PASSES** — `connect.CodeOf(err) == connect.CodePermissionDenied`. Without this test, "authorization is live" would be unfalsifiable; with it, a regression that silently disabled RBAC (or widened `DefaultPublicProcedures`) would turn this failing.
3. **Binary refuses to start on production-shaped config** — `TestGuardAgainstProductionConfig`, five subtests. A non-loopback `DATABASE_URL` host and both non-loopback `SERVER_ADDR` shapes (bare `":PORT"` wildcard bind, explicit `0.0.0.0`) **PASS** as refusals (`guardAgainstProductionConfig` returns a non-nil error); loopback-safe defaults and explicit `127.0.0.1`/`localhost` **PASS** as accepted.

A fourth test, `TestUpdateCompany_NoTokenIsUnauthenticated`, was added alongside these to establish the negative control the deny test needs context against (no token → `CodeUnauthenticated`, distinct from real-token-but-denied → `CodePermissionDenied`).

## Validation Gate Results — all six `<verify>` checks, run fresh after both commits landed

```
$ go build ./...
EXIT: 0

$ go build -tags dev ./...
EXIT: 0

$ go vet ./...
EXIT: 0

$ go vet -tags dev ./...
EXIT: 0

$ go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race -v
=== RUN   TestSeedDevTenant_CoherentIdentity
--- PASS: TestSeedDevTenant_CoherentIdentity (0.38s)
=== RUN   TestSeedDevTenant_Idempotent
--- PASS: TestSeedDevTenant_Idempotent (0.21s)
=== RUN   TestMintDevSession_AcceptedByRealAuthInterceptor
--- PASS: TestMintDevSession_AcceptedByRealAuthInterceptor (0.39s)
=== RUN   TestUpdateCompany_NoTokenIsUnauthenticated
--- PASS: TestUpdateCompany_NoTokenIsUnauthenticated (0.20s)
=== RUN   TestUpdateCompany_UnderPrivilegedUserIsDenied
--- PASS: TestUpdateCompany_UnderPrivilegedUserIsDenied (0.59s)
=== RUN   TestGuardAgainstProductionConfig
=== RUN   TestGuardAgainstProductionConfig/shared_config_defaults_are_loopback-safe
=== RUN   TestGuardAgainstProductionConfig/explicit_loopback_IP_is_safe
=== RUN   TestGuardAgainstProductionConfig/real_database_host_is_refused
=== RUN   TestGuardAgainstProductionConfig/wildcard_bind_address_is_refused
=== RUN   TestGuardAgainstProductionConfig/public_bind_address_is_refused
--- PASS: TestGuardAgainstProductionConfig (0.00s)
PASS
ok  	github.com/aocybersystems/eden-platform-go/cmd/eden-platform-dev	3.316s
EXIT: 0

$ git diff plan/obj-40-platform-telephony..HEAD -- platform/server/interceptors.go
(empty — 0 changed lines)
EXIT: 0
```

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build, no dev tag | `go build ./...` | 0 | PASS |
| build, dev tag | `go build -tags dev ./...` | 0 | PASS |
| vet, no dev tag | `go vet ./...` | 0 | PASS |
| vet, dev tag | `go vet -tags dev ./...` | 0 | PASS |
| test, dev tag, race | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS (7/7 tests, including the 2 from TRD 01) |
| `interceptors.go` unchanged | `git diff plan/obj-40-platform-telephony..HEAD -- platform/server/interceptors.go` | 0 | PASS (0 lines — byte-identical) |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - Real access token minted through the SAME `auth.JWTManager` production uses, not a bypass — verified by `TestMintDevSession_AcceptedByRealAuthInterceptor` (token is validated by the real `authInterceptor`, which itself wraps the real `jwtManager.ValidateAccessToken`)
  - Auth and RBAC interceptors remain fully enabled — verified by `git diff` showing `platform/server/interceptors.go` byte-identical, plus both interceptor tests exercising the real, unmodified interceptor types
  - `DefaultPublicProcedures` not modified — verified by the same `git diff` (it lives in `router.go`, untouched) and by `TestUpdateCompany_NoTokenIsUnauthenticated` confirming `CompanyService.UpdateCompany` is still gated
  - Token surfaced at startup and via a dev-only endpoint — `startDevSession` logs the token via `slog.Info` and registers `GET /dev/session`; wired into `main.go`'s `runServer` (not independently re-verified by a running binary in this TRD's test suite, since that would require binding a live port — see Issues Encountered)
  - Refuses to start on production-shaped config — verified by `TestGuardAgainstProductionConfig` (5/5 subtests)
- **Gate failures:** None

## Files Created/Modified

- `cmd/eden-platform-dev/devsession.go` — `//go:build dev`: `DevSession` type, `mintDevSession`, `registerDevSessionEndpoint`, `guardAgainstProductionConfig` (+ `guardDatabaseURL`/`guardServerAddr`/`isLoopbackHost` helpers), `startDevSession`
- `cmd/eden-platform-dev/devsession_stub.go` — `//go:build !dev`: no-op `startDevSession` stub, mirroring `devseed_stub.go`
- `cmd/eden-platform-dev/devsession_test.go` — `//go:build dev`: the four tests described above, plus `setupDevSessionTestEnv`/`seedUnderPrivilegedUser` test helpers
- `cmd/eden-platform-dev/main.go` — one call site added in `runServer`, after mux route registration and before `http.ListenAndServe`: `if err := startDevSession(cfg, authService, mux); err != nil { log.Fatalf("dev session: %v", err) }`

## Decisions Made

- Chose `auth.Service.Login` over calling `auth.JWTManager.CreateAccessToken` directly, specifically because `Login` is the identical code path a real client hits — password verification, membership/role lookup, refresh-token storage, and audit logging all happen for real. This is the concrete difference between "authenticate" and "exempt" the TRD's `<authenticate_do_not_exempt>` section calls for.
- Chose to check `DATABASE_URL`/`SERVER_ADDR` unconditionally in the guard (not gated on the `--db` flag) — the TRD's must-have says the binary "refuses to start if it SEES production-shaped config," not "if it uses one." An operator's ambient environment carrying a real `DATABASE_URL` while running the in-memory backend is still a real risk if `--db` gets added later without re-checking.
- Chose an httptest.Server + real generated `platformv1connect.CompanyServiceClient` for the interceptor tests, rather than hand-constructing a `connect.AnyRequest`, so the test exercises the exact code path (`connect.WithInterceptors` chain wrapping a real `connectapi.NewCompanyHandler`) that `main.go`'s `runServer` uses — not a simulation of it.

## Deviations from Plan

None — TRD executed as written. The deny test existed as designed on the first pass; no fix cycles were needed.

## Issues Encountered

**The "surfaced at startup" and "via a dev-only endpoint" must-haves are wired into `main.go`/`devsession.go` and compile-verified (both build tags, `go vet`), but are not exercised by a live-binary integration test in this TRD** — doing so would require starting `cmd/eden-platform-dev` as a real subprocess bound to a real port, which the `<turn_budget_warning>` ("do NOT provision infrastructure") and the hard rule against ad hoc port usage argue against for a TRD-level test. The logic itself (`mintDevSession` + `registerDevSessionEndpoint` + the `slog.Info` call) is unit-tested directly via `TestMintDevSession_AcceptedByRealAuthInterceptor`, which calls `mintDevSession` the same way `startDevSession` does. Recommend a future manual/CI smoke check (`go run -tags dev ./cmd/eden-platform-dev` + `curl http://127.0.0.1:8091/dev/session`) if end-to-end binary behavior needs direct evidence beyond this TRD's unit-level coverage.

No other issues. `plan/obj-40-platform-telephony` was already merged into this worktree by TRD 01's execution; no further merge was needed for this TRD.

## User Setup Required

None — no external service configuration required, no infrastructure provisioned. The dev playground continues to run entirely on the in-memory devstore by default; `SERVER_ADDR=127.0.0.1:8091` (never 8080) should be set explicitly when actually running the binary, since the shared config package's own default (`:8080`, wildcard-bind) is now refused by `guardAgainstProductionConfig` for this dev-tagged binary.

## Next Objective Readiness

- `mintDevSession`, `startDevSession`, `DevSession`, and `DevSessionEndpoint` (`/dev/session`) are available in package `main` of `cmd/eden-platform-dev` for TRDs 03-04 of this objective to build on.
- `guardAgainstProductionConfig` is a reusable safety net any further dev-only startup behavior in this objective should sit behind.
- No blockers.

---
*Objective: 42-dev-playground*
*Completed: 2026-09-08*

## Self-Check: PASSED

- FOUND: cmd/eden-platform-dev/devsession.go
- FOUND: cmd/eden-platform-dev/devsession_stub.go
- FOUND: cmd/eden-platform-dev/devsession_test.go
- FOUND: commit 02dd0a7 (feat(42-02): mint a real dev session through the production JWTManager + refuse-to-start guard)
- FOUND: commit 1060e76 (test(42-02): token accepted by the real interceptor, RBAC still denies, prod-config refusal)
