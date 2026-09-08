---
objective: 41-platform-cms
job: "02"
subsystem: database
tags: [postgres, pgx, jsonb, uuid, cms, multi-tenant]

# Dependency graph
requires:
  - objective: 41-platform-cms TRD 01
    provides: "platform/cms.Block, platform/cms.ParseBlocks, platform/cms.Page, platform/cms.PageStatus"
provides:
  - "platform/cms.Store — the persistence interface for Page (+ ordered Blocks)"
  - "platform/cms.PostgresStore — the production Store implementation over cms_pages (JSONB blocks)"
  - "migration 018_cms_pages — company-scoped page table with a per-tenant unique slug index"
affects: [41-platform-cms TRD 03 (publish/preview_token), 41-platform-cms TRD 04, 41-platform-cms TRD 05, 41-platform-cms TRD 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Store as a package-level interface with one Postgres implementation (var _ Store = (*PostgresStore)(nil) compile-time assertion) — mirrors platform/telephony.ConfigStore/PostgresConfigStore from Wave 1"
    - "Blocks persisted as ONE JSONB array column (not one row per block) — a reorder is a single UPDATE, and array index IS render order with no separate sort column to drift"
    - "Tenant scoping enforced in the SQL predicate itself (WHERE company_id = $1 AND id = $2), not as a post-hoc filter — a cross-tenant lookup collapses to ErrNotFound, never a leak"
    - "Per-tenant unique slug via a composite unique index (company_id, slug), mapped from Postgres 23505 to ErrSlugTaken by isSlugConflict"

key-files:
  created:
    - platform/cms/store.go
    - platform/cms/store_pg.go
    - platform/cms/store_pg_test.go
    - migrations/platform/018_cms_pages.up.sql
    - migrations/platform/018_cms_pages.down.sql
  modified: []

key-decisions:
  - "Store's method set is minimal CRUD (Create/Get/GetBySlug/List/Update/Delete) — no publish-lifecycle or lease methods, since publish.go/preview_token.go (TRD 03) own that state machine and were not shown to depend on any particular Store surface"
  - "Blocks stored as a single JSONB array per page, not normalized into a child table — matches eden-biz's frontmatter-JSONB approach and keeps reorder atomic"
  - "Reordering has no dedicated method — a caller reorders p.Blocks client-side and calls UpdatePage; index position IS the persisted order, so this needed no separate API"
  - "CreatePage generates p.ID via uuid.New() only when the caller leaves it uuid.Nil, so a caller may also supply a pre-generated ID"
  - "Live-Postgres verification was explicitly skipped per the coordinator's override of the original migration_verification_note — see Post-TRD Verification below"

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 3
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~45min
completed: 2026-09-08
---

# Objective 41 TRD 02: Store + Postgres Summary

**`platform/cms.Store` interface + `PostgresStore`: JSONB-backed, company-scoped page persistence where Blocks round-trip byte-stably (including keys this package has never heard of) and reordering survives a save/load cycle — migration 018 NOT verified against a live database in this run (see Post-TRD Verification).**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-09-08T08:36:00Z (Wave 1 merge)
- **Completed:** 2026-09-08
- **Tasks:** 4 (Store interface, migration 018, PostgresStore, tests)
- **Files modified:** 5 (all new)

## Accomplishments
- `platform/cms.Store`, an interface any consumer can implement, tenant-scoped by `companyID uuid.UUID` on every method
- `platform/cms.PostgresStore`, the production implementation: Blocks marshal/unmarshal through `encoding/json` into a single JSONB column, so unknown `Data` keys and block order survive untouched — proved directly against the production `scanPage` function via a hand-built `rowScanner` double, no live database required
- Migration `018_cms_pages` (`up`/`down`), following the exact style of migrations 016/017: `company_id` FK to `companies(id) ON DELETE CASCADE`, per-tenant unique slug index
- `ErrSlugTaken` mapped from Postgres `23505` on the `(company_id, slug)` unique index, mirroring `platform/telephony`'s `ErrSendingNumberClaimed` pattern

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: `store.go` (Store interface) | `go build ./platform/cms/...` | 0 | PASS |
| 2: migration 018 (up/down) | reviewed against 016/017 for style; **not applied** — see Post-TRD Verification | n/a | NOT VERIFIED |
| 3: `store_pg.go` (PostgresStore) | `go build ./platform/cms/... && go vet ./platform/cms/...` | 0 | PASS |
| 4: `store_pg_test.go` | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: `cms.Store` interface** - `bc15065` (feat)
2. **Task 2: migration 018 (up/down)** - `dc14c15` (chore)
3. **Task 3: `PostgresStore` implementation** - `c5c875a` (feat)
4. **Task 4: `store_pg_test.go`** - `46b5b4f` (test)
5. **Fixup: rename test fixture to clear forbidden-vocabulary grep** - `c1ff28b` (fix)

**Plan metadata:** this file's own commit (docs, made immediately after this summary is written)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build | `go build ./platform/cms/...` | 0 | PASS |
| vet | `go vet ./platform/cms/...` | 0 | PASS |
| test (race) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |
| migration apply/rollback | `migrate -source file://migrations/platform -database pgx5://... up` / `down` | — | **NOT RUN** (no live Postgres provisioned in this run — see below) |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 3/4 directly; 1/4 (ordering/round-trip through an actual JSONB column read back from Postgres) verified only at the marshal/unmarshal boundary, not end-to-end through a live database
- **Gate failures:** None among the gates that ran

### Migration 018 apply/rollback — explicitly NOT verified against a live Postgres

The original brief's `migration_verification_note` permitted skipping the live-apply check
when Postgres isn't reachable, stating plainly if so. Partway through this run, a live
Postgres container was in fact reachable and one was provisioned (`cms-trd02-pg`,
`postgres:17-alpine`, port 5546) to run the apply/rollback check for real. The coordinator
then intervened and explicitly instructed stopping that provisioning to conserve turn
budget for the actual deliverables (`store_pg.go` did not exist yet at that point). The
container was torn down (`docker rm -f cms-trd02-pg`) without ever applying migration 018
to it.

**As a direct result: migration 018's apply-then-rollback (`up` then `down`) has NOT been
run against any Postgres instance in this execution.** What WAS done instead, in place of
that check:

- The migration SQL was hand-reviewed line-by-line against the style of migrations 016 and
  017 (same repo, Wave 1) — table shape, index shape, `ON DELETE CASCADE`, `IF NOT EXISTS` /
  `IF EXISTS` guards, and comment style all match.
- `store_pg_test.go` includes a full suite of `DATABASE_URL`-gated integration tests
  (`TestPostgresStore_*_Integration`) that DO exercise `CreatePage` → `GetPage` /
  `GetPageBySlug` / `ListPages` → `UpdatePage` (reorder) → `DeletePage` against a real
  `cms_pages` table via `pgstore.NewBackend`, which itself calls `RunMigrations` (i.e. running
  those tests with `DATABASE_URL` set to a `pgx5://` URL WOULD apply migration 018 as a side
  effect). In this run, `DATABASE_URL` was unset, so all six of those tests reported `SKIP`
  (see the test run below) rather than a false PASS.
- The core round-trip / ordering / unknown-key-preservation properties that the TRD's
  `<verify>` section cares about were instead proved at the `encoding/json` boundary,
  directly against `scanPage` (the same function the real Postgres path calls), using a
  hand-built `rowScanner` fake. This proves the Go-side of the contract exhaustively; it does
  NOT prove the SQL migrated cleanly, that the unique index enforces what it claims, or that
  pgx's wire encoding of a `JSONB` column round-trips identically to what
  `encoding/json.Marshal` produced. Those three specifically require a live database and are
  the residual gap.

**Honest bottom line:** three of the four `<verify>` bullets are proved directly by the
committed test suite; the fourth ("migration applies and rolls back cleanly") is not proved
in this execution. A follow-up run with `DATABASE_URL` set to a `pgx5://...` connection
string would exercise it via the already-written integration tests without any code changes.

### Full test run

```
$ go build ./platform/cms/... && go vet ./platform/cms/...
(no output — both clean)

$ go test ./platform/cms/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/platform/cms	1.343s

$ grep -riE "hero|practitioner|bio|accordion|membership_grid" platform/cms/
(no output — exit 1, no match)
```

All 26 tests pass; the 6 `*_Integration` tests report `SKIP` (DATABASE_URL unset), not PASS —
see the verbose run captured during execution:

```
--- SKIP: TestPostgresStore_CreateGetRoundTrip_Integration (0.00s)
--- SKIP: TestPostgresStore_ReorderPersists_Integration (0.00s)
--- SKIP: TestPostgresStore_TenantScoping_Integration (0.00s)
--- SKIP: TestPostgresStore_SlugConflict_Integration (0.00s)
--- SKIP: TestPostgresStore_DeletePage_Integration (0.00s)
--- SKIP: TestPostgresStore_ListPages_Integration (0.00s)
```

## Files Created/Modified
- `platform/cms/store.go` - `Store` interface (Create/Get/GetBySlug/List/Update/Delete Page), `ErrNotFound`, `ErrSlugTaken`
- `platform/cms/store_pg.go` - `PostgresStore`, `scanPage`, `isSlugConflict`; JSONB marshal/unmarshal of `[]Block`
- `platform/cms/store_pg_test.go` - unit tests against `scanPage` via a fake `rowScanner` (no DB needed) + `DATABASE_URL`-gated integration tests (skip cleanly when unset)
- `migrations/platform/018_cms_pages.up.sql` / `.down.sql` - `cms_pages` table, company FK, per-tenant unique slug index

## Decisions Made
- Kept `Store`'s method set to plain CRUD; publish-state and preview-token concerns stay entirely in TRD 03's `publish.go` / `preview_token.go`, which this TRD does not touch
- No dedicated "reorder" method — `UpdatePage` with a reordered `p.Blocks` slice is the reorder path, since Block order is positional (array index), not a stored rank column
- `isSlugConflict` / `ErrSlugTaken` added beyond the TRD's literal must-haves (Rule 2 — missing critical functionality): a JSONB `Store` with a real unique index needs SOME typed error for the conflict case rather than callers pattern-matching raw Postgres error text

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `ErrSlugTaken` + `isSlugConflict` conflict mapping**
- **Found during:** Task 1 (`store.go` design) / Task 3 (`store_pg.go` implementation)
- **Issue:** The TRD's must-haves don't mention slug uniqueness, but migration 018 needs a per-tenant unique index for `GetPageBySlug` to be meaningful, and an unmapped `23505` would otherwise surface as an opaque wrapped Postgres error to every caller
- **Fix:** Added `uq_cms_pages_company_slug` unique index in migration 018, `ErrSlugTaken` sentinel in `store.go`, and `isSlugConflict` mapping in `store_pg.go`, applied in both `CreatePage` and `UpdatePage`
- **Files modified:** `platform/cms/store.go`, `platform/cms/store_pg.go`, `migrations/platform/018_cms_pages.up.sql`
- **Verification:** `TestIsSlugConflict` (unit, no DB) + `TestPostgresStore_SlugConflict_Integration` (DB-gated, not run this session)
- **Committed in:** `bc15065`, `c5c875a`, `dc14c15`

**2. [Turn-budget correction, coordinator-directed] Stopped live-Postgres provisioning mid-task**
- **Found during:** after Task 2 (migration files committed), before Task 3 existed
- **Issue:** Per the original brief's `migration_verification_note`, a live Postgres was
  actually reachable in this environment (`pg_isready` on 5432, plus several unrelated
  project containers running), so a dedicated container was provisioned to genuinely verify
  migration 018's apply/rollback. The coordinator flagged this as consuming turn budget the
  still-missing `store_pg.go` / `store_pg_test.go` / `SUMMARY.md` deliverables needed more,
  and pointed out the brief's own instruction NOT to block on live-Postgres verification.
- **Fix:** Tore down the provisioned container without applying migration 018 to it, and
  proceeded straight to the missing deliverables. Migration verification is documented here
  as explicitly NOT run, rather than silently skipped or falsely claimed.
- **Files modified:** none (infrastructure-only; no repo files affected)
- **Verification:** n/a — this is a process correction, not a code change
- **Committed in:** n/a

---

**Total deviations:** 2 (1 auto-fixed missing-critical, 1 process correction)
**Impact on plan:** The slug-uniqueness addition is necessary for `GetPageBySlug` to be a
sound API and required no scope beyond this TRD's own files. The process correction leaves
one `<verify>` bullet ("migration applies and rolls back cleanly") genuinely unverified in
this run — flagged explicitly above and in Post-TRD Verification, not glossed over.

## Issues Encountered
Live Postgres migration apply/rollback was not exercised — see "Migration 018 apply/rollback
— explicitly NOT verified against a live Postgres" above for the full account and the
recovery path (set `DATABASE_URL` to a `pgx5://` URL and re-run `go test ./platform/cms/...`).

## User Setup Required
None - no external service configuration required. A follow-up verification pass needs a
`pgx5://user:pass@host:port/dbname?sslmode=disable`-style `DATABASE_URL` pointed at a
disposable Postgres instance; no other setup.

## Next Objective Readiness
- `platform/cms.Store` and `platform/cms.PostgresStore` are ready for TRD 03
  (`publish.go` / `preview_token.go`) to build the publish-lifecycle state machine on top of
  — TRD 03's own scope description does not reference specific `Store` methods, so no
  further coordination was needed here.
- Outstanding: migration 018's live apply/rollback should be exercised the next time a
  `DATABASE_URL` is available in an execution environment, via the integration tests already
  committed in `store_pg_test.go`.

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
