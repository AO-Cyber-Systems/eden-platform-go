---
objective: 41-platform-cms
job: "06"
subsystem: cms
tags: [go, documentation, migration-guide, integration-test, multi-tenant, cms]

# Dependency graph
requires:
  - objective: 41-platform-cms (TRD 01)
    provides: "Block, ParseBlocks, Page, PageStatus"
  - objective: 41-platform-cms (TRD 02)
    provides: "Store interface, PostgresStore, migration 018_cms_pages"
  - objective: 41-platform-cms (TRD 03)
    provides: "publish.go lifecycle (Schedule/PublishNow/Archive/VisibleAt), preview_token.go (MintPreviewToken/Authorize)"
  - objective: 41-platform-cms (TRD 04)
    provides: "MediaPipeline (media.go)"
  - objective: 41-platform-cms (TRD 05)
    provides: "Settings, SettingsStore, MemorySettingsStore"
provides:
  - "platform/cms/doc.go — package-level godoc establishing the opaque-Type / catalog-is-consumer decision"
  - "platform/cms/README.md — layering doctrine, per-file API map, both open gaps documented"
  - "platform/cms/MIGRATION.md — four-source adoption map, deferred-capability homes, Obj 35 unblocked-not-performed record"
  - "platform/cms/integration_test.go — draft -> schedule -> preview-token read -> due -> public read, proven against an in-memory Store"
affects: [any future TRD/objective adopting platform/cms, a future Obj 35 (eden-biz -> eden-web cutover, still unplanned)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "memStore: an in-memory Store fake, test-only, proving Store's interface contract composes with publish.go/preview_token.go independent of PostgresStore -- direct proof of store.go's own claim that any Store implementation works"

key-files:
  created:
    - platform/cms/doc.go
    - platform/cms/README.md
    - platform/cms/MIGRATION.md
    - platform/cms/integration_test.go
  modified: []

key-decisions:
  - "Integration test runs against an in-memory Store (memStore, defined in integration_test.go), not PostgresStore -- proves the LIFECYCLE composes across any Store implementation, and does not touch a live database (no infrastructure was provisioned per this TRD's explicit instruction, and migration 018's live-apply status stays exactly as unverified as TRD 02 left it)"
  - "doc.go carries the package-level godoc comment (previously absent -- no file in the package had one); README.md carries the same doctrine in prose with a per-file API map; the two are complementary, not duplicates -- doc.go is what `go doc` shows, README.md is what a human browsing the repo reads first"
  - "MIGRATION.md documents Obj 35 (eden-biz -> eden-web cutover) as unblocked by this objective but explicitly NOT performed -- no eden-biz file was touched, no eden-biz import of platform/cms exists"

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~40min
completed: 2026-09-08
---

# Objective 41 TRD 06: Docs + Integration Test Summary

**`platform/cms/{doc.go,README.md,MIGRATION.md}` documenting the opaque-Type/catalog-is-the-consumer layering doctrine and the four-source adoption map, plus `integration_test.go` proving draft -> schedule -> preview-token read -> due -> public read composes against an in-memory Store — the FINAL TRD of Objective 41, closing out 8 impl files / 1,614 LOC / 84 passing tests.**

## Performance

- **Duration:** ~40 min
- **Started:** merge of `plan/obj-40-platform-telephony` (Waves 1-3)
- **Completed:** 2026-09-08
- **Tasks:** 4 (doc.go, README.md, MIGRATION.md, integration_test.go)
- **Files modified:** 4 (all new)

## Accomplishments
- `platform/cms/doc.go` — the package's first `// Package cms` godoc comment, leading with the opaque-`Block.Type` / catalog-belongs-to-the-consumer decision and naming both open gaps
- `platform/cms/README.md` — per-file API map, the layering diagram (`[]Block` → seam → renderer), a verifiable `go list -deps ./platform/cms | grep eden-web` claim, and both open gaps (settings persistence, migration 018) documented plainly rather than implied closed
- `platform/cms/MIGRATION.md` — all four surveyed sources named with LOC counts, all deferred capabilities mapped to their current homes, and the Obj 35 (eden-biz → eden-web cutover) unblocked-but-not-performed record
- `platform/cms/integration_test.go` — `TestLifecycle_DraftScheduleDuePreviewPublicRead` (the required flow) plus `TestLifecycle_CrossTenantReadCollapsesToNotFound` (tenant-isolation proof), both run against a new `memStore` test fixture that implements `Store` in-memory

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: doc.go | `go build ./platform/cms/... && go vet ./platform/cms/...` | 0 | PASS |
| 2: README.md | `go list -deps ./platform/cms \| grep eden-web` | 1 (no match, as documented) | PASS |
| 3: MIGRATION.md | manual cross-check against 41-RESEARCH.md's source table + TRD constraints | n/a | PASS |
| 4: integration_test.go | `go test ./platform/cms/... -run TestLifecycle -v` | 0 | PASS |

## Task Commits

Each deliverable was committed atomically, as it completed:

1. **Task 1: doc.go** - `073a935` (docs)
2. **Task 2: README.md** - `16531f0` (docs)
3. **Task 3: MIGRATION.md** - `7f8406b` (docs)
4. **Task 4: integration_test.go** - `2900d24` (test)

**Plan metadata:** this file's own commit (docs, made immediately after this summary is written)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (full repo) | `go build ./...` | 0 | PASS |
| vet (full repo) | `go vet ./...` | 0 | PASS |
| test + race (platform/cms) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |
| test (full platform/... regression) | `go test ./platform/... -count=1` | 0 | PASS (60 packages, no failures) |
| eden-web dependency check | `go list -deps ./platform/cms \| grep eden-web` | 1 (no output) | PASS |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - README states the layering (content/lifecycle here, rendering in eden-web, seam is `Block.Type` → component name) — YES, README.md "Layering" section
  - `platform/cms` has NO dependency on eden-web — YES, verified live: `go list -deps ./platform/cms | grep eden-web` returns nothing
  - MIGRATION.md addresses all four sources and names what V1 does NOT cover — YES, table with eden-biz (46,097 LOC), justinforme (8,772), smartWellness (4,288), politihub (681), plus the six-row deferred-capability table
  - MIGRATION.md records Obj 35 is unblocked but not performed — YES, "What this unblocks — without performing it" section
  - Integration test covers draft → schedule → preview-token read → due → public read — YES, `TestLifecycle_DraftScheduleDuePreviewPublicRead`, passing
- **Gate failures:** None

## Full Verification Output

```
$ go build ./...
(no output — clean)

$ go vet ./...
(no output — clean)

$ go test ./platform/cms/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	5.479s

$ go test ./platform/cms/... -count=1 -race -v | grep -c '^--- PASS'
84

$ go test ./platform/... -count=1
ok  	.../platform/auth/session         ok  	.../platform/kms/softkey
ok  	.../platform/auth/social          ok  	.../platform/livekit
ok  	.../platform/auth/totp            ok  	.../platform/logingov
ok  	.../platform/auth/webauthn        ok  	.../platform/membership
ok  	.../platform/billing-rail         ok  	.../platform/mtls
ok  	.../platform/bridge               ok  	.../platform/mtls/piv
ok  	.../platform/cms                  ok  	.../platform/notification
ok  	.../platform/company               ok  	.../platform/observability
ok  	.../platform/config                ok  	.../platform/oidcrp
ok  	.../platform/connectapi            ok  	.../platform/pgstore
ok  	.../platform/consent               ok  	.../platform/pki
?   	.../platform/devstore [no test files]  ok  .../platform/policycache
ok  	.../platform/email                 ok  	.../platform/ratelimit
ok  	.../platform/encryption            ok  	.../platform/rbac
ok  	.../platform/entitlements          ok  	.../platform/realtime
ok  	.../platform/errortrack            ok  	.../platform/registry
ok  	.../platform/experience            ok  	.../platform/risk
ok  	.../platform/experience/fixtures   ok  	.../platform/rpauth
ok  	.../platform/feature-flags         ok  	.../platform/saml
ok  	.../platform/fipsmode              ok  	.../platform/scheduler
ok  	.../platform/hashutil              ok  	.../platform/search
ok  	.../platform/household             ok  	.../platform/server
ok  	.../platform/httputil              ok  	.../platform/spiffe
ok  	.../platform/identity              ok  	.../platform/statemachine
ok  	.../platform/integration           ok  	.../platform/storage
ok  	.../platform/jobs                  ok  	.../platform/telephony
ok  	.../platform/kms                   ok  	.../platform/upload
ok  	.../platform/kms/awskms            ok  	.../platform/webfetch
ok  	.../platform/kms/azkv              ok  	.../platform/webhook
ok  	.../platform/kms/pkcs11
(all ok, no FAIL lines — full output captured during execution)

$ go list -deps ./platform/cms | grep eden-web
(no output — exit 1, confirms no dependency)
```

## Files Created/Modified
- `platform/cms/doc.go` — package-level godoc: leads with the opaque-`Block.Type`/catalog-is-consumer decision, states the layering boundary, names both open gaps
- `platform/cms/README.md` — human-facing doc: layering diagram, per-file API map, lifecycle/preview-token/store/settings/media sections, both open gaps in their own headed section, an "Adopting this package" checklist
- `platform/cms/MIGRATION.md` — four-source table with LOC, per-source provenance of what was ported vs. reconciled, six-row deferred-capability table, the Obj 35 unblocked-not-performed record, an explicit "not performed" adoption checklist for a future migration
- `platform/cms/integration_test.go` — `memStore` (in-memory `Store` fixture, `var _ Store = (*memStore)(nil)`), `TestLifecycle_DraftScheduleDuePreviewPublicRead`, `TestLifecycle_CrossTenantReadCollapsesToNotFound`

## Decisions Made
- The integration test proves composition (`Store` + `publish.go` + `preview_token.go` working together), not `PostgresStore`'s SQL correctness — those are deliberately different claims, and conflating them would have misrepresented migration 018's actual (unverified) status. `memStore` is a small, self-contained test fixture that honors `store.go`'s documented contracts (default `Status`, `ErrNotFound` on cross-tenant lookup, `ErrSlugTaken` on per-tenant collision) rather than a loosened stand-in.
- `doc.go` and `README.md` are deliberately complementary, not duplicates: `doc.go` is what `go doc ./platform/cms` and godoc.org show; `README.md` is what a human opens first in the repo browser. Both lead with the same central decision (Type is opaque) because that decision is the one fact every future reader — human or `go doc` — most needs first.
- Chose not to provision any Postgres infrastructure in this TRD, per explicit instruction. This means open gap #3 from the objective's tracked facts (migration 018 unverified against a live database) remains exactly as unverified as TRD 02 left it — this TRD documents that status plainly rather than attempting (and risking a turn-budget failure on) a live-DB verification that was out of scope.

## Deviations from Plan

None — TRD executed exactly as written. No Rule 1-4 deviations were needed: no bugs were found in the merged Waves 1-3 code, no missing critical functionality was discovered, and no blocking issues arose during doc/test authorship.

## Issues Encountered

None. The worktree-base merge (`git merge plan/obj-40-platform-telephony`) applied cleanly on the first attempt, and the pre-merge verification (`ls platform/cms/` showing all 8 impl files, `go test ./platform/cms/... -count=1 -race` showing 82 passing tests) matched the worktree-base warning's expectations exactly before any new code was written.

## User Setup Required

None — no external service configuration required. This TRD added documentation and one in-memory-backed test file; no database, no environment variables, no dashboard configuration.

## Next Objective Readiness

- `platform/cms` is documentation-complete and lifecycle-proven for any future consumer to adopt: `README.md` gives a `go doc`-adjacent orientation plus an "Adopting this package" checklist, `MIGRATION.md` gives the four-source provenance and deferred-capability map, `integration_test.go` proves the core lifecycle composes.
- **Two open gaps carry forward, both documented, neither silently closed:**
  1. No `PostgresSettingsStore` exists — `SettingsStore`/`MemorySettingsStore` (TRD 05) has no Postgres-backed implementation because `migrations/` was out of that TRD's file-ownership scope. A future TRD adding a `cms_site_settings` migration + `PostgresSettingsStore` is a natural, small follow-up.
  2. Migration `018_cms_pages` has never been applied against a live Postgres across this entire objective's execution history (TRD 02 → TRD 06). The next execution with a `DATABASE_URL` set to a `pgx5://` connection string would exercise this for real via `store_pg_test.go`'s already-written `*_Integration` tests, with no code changes required.
- Objective 41 (`platform-cms`) is now feature-complete across all six TRDs: 8 impl files, 1,614 LOC (adds `doc.go`'s ~54 lines to the 1,612 baseline), and 84 passing tests (82 baseline + 2 new lifecycle tests) in `platform/cms`.
- A future Obj 35 (eden-biz → eden-web cutover) is unblocked but not started — see MIGRATION.md's dedicated section. No eden-biz code was touched by this objective.

## Self-Check: PASSED

- FOUND: `platform/cms/doc.go`
- FOUND: `platform/cms/README.md`
- FOUND: `platform/cms/MIGRATION.md`
- FOUND: `platform/cms/integration_test.go`
- FOUND commit: `073a935`
- FOUND commit: `16531f0`
- FOUND commit: `7f8406b`
- FOUND commit: `2900d24`

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
