---
objective: 42-dev-playground
job: 01
subsystem: dev-tooling
tags: [devstore, seeding, rbac, auth, dev-server, build-tags, idempotency]

# Dependency graph
requires:
  - objective: none (first TRD of 42-dev-playground)
    provides: n/a
provides:
  - "seedDevTenant(backend): a coherent demo tenant (company + user + membership + owner-role binding) seeded into the in-memory devstore on every dev-server start"
  - "The `//go:build dev` / `//go:build !dev` dev-only build-tag pattern, established for the first time in cmd/, ready for TRD 02-04 to extend"
affects: [42-dev-playground-02, 42-dev-playground-03, 42-dev-playground-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dev-only build tag pair in cmd/: `//go:build dev` real file + `//go:build !dev` no-op stub file, called unconditionally from an untagged main.go call site"

key-files:
  created:
    - cmd/eden-platform-dev/devseed.go
    - cmd/eden-platform-dev/devseed_stub.go
    - cmd/eden-platform-dev/devseed_test.go
  modified:
    - cmd/eden-platform-dev/main.go

key-decisions:
  - "Reused the fixed company ID (20000000-0000-0000-0000-000000000001) that seedSSOForDev and seedWebhookData already seed against, so the demo company also carries SSO config and a webhook — one coherent tenant instead of disconnected fixtures"
  - "Did not create a separate RBAC membership row. platform/devstore/rbac_store.go's GetUserRole/GetMembership/GetUserPermissions already fall back to the auth-store membership when no rbacMemberships entry exists — this is the exact same path production signup (auth.Service.SignUp) relies on, so seeding stays byte-for-byte coherent with how a real signup would look"
  - "User idempotency is keyed on email (GetUserByEmail before CreateUser), not a fixed user ID — AuthStore.CreateUser always mints its own uuid.New() ID, matching production signup, and has no explicit-ID parameter to seed against"
  - "Added devseed_stub.go (`//go:build !dev` no-op) so main.go's unconditional `seedDevTenant(backend)` call compiles in both build modes without needing a build tag on main.go itself"

patterns-established:
  - "Dev-only build tag pattern: `//go:build dev` implementation + `//go:build !dev` stub, first use in cmd/ — TRDs 02-04 of this objective should follow the same pair if they add more dev-only seeding/behavior"

requirements-completed: [R48]

# Verification evidence
verification:
  gates_defined: 5
  gates_passed: 5
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~25min
completed: 2026-09-08
---

# Objective 42 TRD 01: Seed Demo Tenant Summary

**Coherent demo tenant (company + Argon2id-hashed user + membership + owner-role binding) seeded into the in-memory devstore behind a new `//go:build dev` / `!dev` build-tag pair, idempotent across repeated calls.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-09-08 (merge of plan/obj-40-platform-telephony, ~10:5x local)
- **Completed:** 2026-09-08T11:04:16-04:00
- **Tasks:** 2 (implementation + build-tag wiring; test coverage)
- **Files modified:** 4 (3 created, 1 edited)

## Accomplishments
- `seedDevTenant` seeds a company, a user with a real Argon2id password hash (`auth.NewPasswordHasher`), a company membership, and (via the devstore RBAC fallback) an owner-role binding that grants every seeded permission
- Established the dev-only build-tag pattern for `cmd/`: `devseed.go` (`//go:build dev`) + `devseed_stub.go` (`//go:build !dev`), so `main.go` compiles and behaves correctly with or without `-tags dev`
- Two tests prove the two must-haves that matter most: coherence (membership + real permissions) and idempotency (seeding twice yields one tenant)

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Implement seedDevTenant + build-tag stub + main.go wiring | `go build ./... && go build -tags dev ./... && go vet ./... && go vet -tags dev ./...` | 0 | PASS |
| 2: Coherence + idempotency test coverage | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race -v` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: seedDevTenant implementation + inverse-tag stub + main.go call site** - `e28946b` (feat)
2. **Task 2: coherence + idempotency test coverage (+ dead-code cleanup of an unused fixed user-ID var)** - `b7cd6af` (test)

**Plan metadata:** (this commit) - `docs(42-01): complete demo-tenant TRD`

_Note: Task 2's commit also includes a small in-place edit to devseed.go — removed an unused `DevTenantUserID` var that couldn't actually be honored by `AuthStore.CreateUser` (see Deviations below)._

## Validation Gate Results

All five `<verify>` gates from the TRD, run fresh after both commits landed:

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build, no dev tag | `go build ./...` | 0 | PASS |
| vet, no dev tag | `go vet ./...` | 0 | PASS |
| build, dev tag | `go build -tags dev ./...` | 0 | PASS |
| vet, dev tag | `go vet -tags dev ./...` | 0 | PASS |
| test, dev tag | `go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS |
| test, no tag (literal TRD command) | `go test ./cmd/eden-platform-dev/... -count=1 -race` | 0 | PASS (`[no test files]` — expected: devseed_test.go carries `//go:build dev`, so meaningful coverage requires `-tags dev`, run above) |
| platform/ isolation | `grep -rn "eden-platform-dev" platform/` | 1 (no match) | PASS (returns nothing, as required) |

Actual pasted output:

```
$ go build ./...
EXIT:0

$ go vet ./...
EXIT:0

$ go build -tags dev ./...
EXIT:0

$ go vet -tags dev ./...
EXIT:0

$ go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race -v
=== RUN   TestSeedDevTenant_CoherentIdentity
2026/09/08 11:04:33 INFO seeded RBAC data roles=4 permissions=7
2026/09/08 11:04:33 INFO seeded dev tenant company_id=20000000-0000-0000-0000-000000000001 user_id=b5973f58-b80e-47af-9bd7-853851ddbd12 email=dev@eden.local role=owner
--- PASS: TestSeedDevTenant_CoherentIdentity (0.38s)
=== RUN   TestSeedDevTenant_Idempotent
2026/09/08 11:04:33 INFO seeded RBAC data roles=4 permissions=7
2026/09/08 11:04:34 INFO seeded dev tenant company_id=20000000-0000-0000-0000-000000000001 user_id=106fa255-1cec-42f4-82da-4b5296acc42a email=dev@eden.local role=owner
2026/09/08 11:04:34 INFO seeded dev tenant company_id=20000000-0000-0000-0000-000000000001 user_id=106fa255-1cec-42f4-82da-4b5296acc42a email=dev@eden.local role=owner
--- PASS: TestSeedDevTenant_Idempotent (0.19s)
PASS
ok  	github.com/aocybersystems/eden-platform-go/cmd/eden-platform-dev	1.926s
EXIT:0

$ go test ./cmd/eden-platform-dev/... -count=1 -race
?   	github.com/aocybersystems/eden-platform-go/cmd/eden-platform-dev	[no test files]
EXIT:0

$ grep -rn "eden-platform-dev" platform/
EXIT:1   (no output — no matches, as required)
```

**Test-to-must-have mapping:**
- `TestSeedDevTenant_CoherentIdentity` proves: the seeded user is a member of the seeded company (`AuthStore.GetCompanyMembershipByUser`) AND holds a role granting at least one real permission (`RBACStore.GetUserPermissions` len > 0, role name == "owner"). It also proves the password hash is real (Argon2id, verifies via `hasher.Verify` against the known seed password — not a placeholder string).
- `TestSeedDevTenant_Idempotent` proves: calling `seedDevTenant` twice against the same backend yields exactly one company (`CompanyStore.ListCompanies` len == 1) and one unambiguous membership for the user, not two.

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - Complete demo tenant seeded (company, user w/ real hash, membership, RBAC role binding) — verified
  - Seeded identity coherent (member + real permissions) — verified by `TestSeedDevTenant_CoherentIdentity`
  - Idempotent (twice = one tenant) — verified by `TestSeedDevTenant_Idempotent`
  - Dev-only build tag, not compiled into production binary — verified by build/vet in both modes + the tag itself
  - Nothing under platform/ imports this code — verified by `grep -rn "eden-platform-dev" platform/` returning nothing
- **Gate failures:** None

## Files Created/Modified
- `cmd/eden-platform-dev/devseed.go` - `//go:build dev` real implementation of `seedDevTenant(backend *devstore.Backend)`
- `cmd/eden-platform-dev/devseed_stub.go` - `//go:build !dev` no-op stub so main.go compiles without the dev tag
- `cmd/eden-platform-dev/devseed_test.go` - `//go:build dev` tests proving coherence and idempotency
- `cmd/eden-platform-dev/main.go` - one-line call-site addition: `seedDevTenant(backend)` after the existing seed calls in the in-memory-backend branch

## Decisions Made
- Reused devstore's existing RBAC-membership-fallback behavior instead of adding an explicit RBAC membership write, since that fallback is precisely what lets the in-memory backend mirror production signup (`auth.Service.SignUp` only ever calls `AuthStore.CreateCompanyMembership`, never a separate RBAC store call). This keeps the seeded tenant's authorization path genuinely identical to a real signup, per the TRD's coherence requirement.
- Established the `//go:build dev` + `//go:build !dev` stub pattern as the answer to "how does an untagged main.go call dev-only code safely" — this is now precedent for TRDs 02-04 of this objective if they add further dev-only behavior.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed an unused, misleading `DevTenantUserID` fixed-ID var**
- **Found during:** Task 2 (writing tests), while first drafting a `DevTenantUserID` constant intended to make the seeded user's ID fixed/idempotent by construction, mirroring `DevTenantCompanyID`.
- **Issue:** `AuthStore.CreateUser` (the existing, unmodifiable devstore helper) always mints its own `uuid.New()` ID — there is no parameter to pin the created user to a caller-supplied ID. The declared `DevTenantUserID` var was therefore never actually wired to anything and would have been dead, inaccurate documentation (implying user-ID-based idempotency that wasn't real).
- **Fix:** Removed the var and its doc comment claim; replaced with a doc comment on `DevTenantCompanyID` explaining that the user's idempotency is achieved via email lookup (`GetUserByEmail` before `CreateUser`) instead, which is what the code actually does.
- **Files modified:** `cmd/eden-platform-dev/devseed.go`, `cmd/eden-platform-dev/devseed_test.go` (dropped a placeholder test that only existed to reference the unused var)
- **Verification:** `go build ./... && go build -tags dev ./... && go vet ./... && go vet -tags dev ./...` all green after the change; `go test -tags dev ./cmd/eden-platform-dev/...` green.
- **Committed in:** `b7cd6af` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — dead/misleading code caught before it shipped, not a functional regression)
**Impact on plan:** No scope creep. Tightened accuracy of the seeding code's own documentation; no behavior change to the seeded tenant.

## Issues Encountered

**Pre-existing gofmt drift in `cmd/eden-platform-dev/main.go` (import ordering + map-literal alignment), unrelated to this TRD.** Confirmed via `git show e28946b -- cmd/eden-platform-dev/main.go`: my only change to that file is the single added line `seedDevTenant(backend)`; the gofmt-flagged import ordering and struct-literal column alignment predate this TRD. Left untouched per file_ownership ("minimal call-site edit only") — reformatting the whole file was out of scope and not requested.

No other issues. The initial `git merge plan/obj-40-platform-telephony` (required by the worktree_base_warning, run before any code changes) was a clean fast-forward with no conflicts, bringing in `platform/cms/` and `platform/telephony/` untouched by this TRD's work.

## User Setup Required

None - no external service configuration required. No infrastructure (Postgres, Docker) was provisioned or is needed; the dev server continues to run entirely on the in-memory devstore.

## Next Objective Readiness

- `seedDevTenant` and the demo tenant's identifiers (`DevTenantCompanyID`, `DevTenantEmail`, `DevTenantPassword`) are available in package `main` of `cmd/eden-platform-dev` for TRDs 02-04 of this objective to build on (e.g., surfacing the credentials in a dev UI, wiring a login shortcut).
- The `//go:build dev` / `!dev` pattern is precedent for any further dev-only additions in this objective.
- No blockers.

---
*Objective: 42-dev-playground*
*Completed: 2026-09-08*

## Self-Check: PASSED

- FOUND: cmd/eden-platform-dev/devseed.go
- FOUND: cmd/eden-platform-dev/devseed_stub.go
- FOUND: cmd/eden-platform-dev/devseed_test.go
- FOUND: commit e28946b (feat(42-01): seed dev tenant)
- FOUND: commit b7cd6af (test(42-01): demo tenant coherence + idempotency coverage)
