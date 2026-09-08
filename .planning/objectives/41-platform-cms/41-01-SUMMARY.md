---
objective: 41-platform-cms
job: "01"
subsystem: content-model
tags: [go, cms, jsonb, tolerant-parsing, uuid]

# Dependency graph
requires:
  - objective: 40-platform-telephony
    provides: "CompanyID uuid.UUID tenant-key convention (platform/telephony.Config.CompanyID), house package doc/error/test style"
provides:
  - "Block{ID,Type,Data} — the opaque, order-preserving, fault-tolerant content unit"
  - "ParseBlocks(extra map[string]any) []Block — tolerant projection ported verbatim from eden-biz"
  - "Page{ID,CompanyID,Slug,Title,Blocks,Status,ScheduledPublishAt,PublishedAt,...} — the content-carrying, non-rendering page shell"
  - "PageStatus closed enum (draft/scheduled/published/archived) with wire-locked string values"
affects: [41-02-store, 41-03-publish-preview, 41-04-media, 41-05-settings, 41-06-docs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tolerant-parse-by-construction: a malformed element is skipped, never fails its siblings (ported from eden-biz block_schema.go)"
    - "Data-carrier domain struct with NO transition/rendering methods — later TRDs (store.go, publish.go) own behavior"

key-files:
  created:
    - platform/cms/block.go
    - platform/cms/block_test.go
    - platform/cms/page.go
    - platform/cms/page_test.go
  modified: []

key-decisions:
  - "ParseBlocks keeps eden-biz's exact signature (extra map[string]any, reading extra[\"blocks\"]) rather than a narrower any-typed parameter, to port the tolerant semantics verbatim in shape as instructed"
  - "eden-biz's ResolveBlocks/shimLegacyBlocks (legacy-frontmatter migration shim) was deliberately NOT ported — it exists to bridge eden-biz's own pre-migration keyed storage, has no analog in a fresh platform/cms package, and isn't named in this TRD's must_haves"
  - "PageStatus constants (draft/scheduled/published/archived) are declared here as data, matching justinforme/smartWellness's precedent of colocating the status enum with the domain struct; the actual transition/validation logic is left to publish.go (41-03) per file ownership"
  - "Page carries ScheduledPublishAt and PublishedAt as data fields (*time.Time) but implements no due-at evaluation or transition guard — that enforcement is explicitly 41-03's scope"

patterns-established:
  - "Package files declare `package cms` with no package doc comment; the full package doc.go arrives in 41-06 once the whole surface exists (matches how platform/telephony's doc.go was the last file written, not the first)"

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: 20min
completed: 2026-09-08
---

# Objective 41 TRD 01: Content Model (Block + Page) Summary

**Opaque, order-preserving, fault-tolerant Block ported verbatim from eden-biz's block_schema.go, plus a Page data-carrier with a wire-locked PageStatus enum and zero rendering concerns.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-09-08T08:20:00-04:00 (approx, after merge)
- **Completed:** 2026-09-08T08:34:30-04:00
- **Tasks:** 2 (Block + ParseBlocks; Page + PageStatus)
- **Files modified:** 4 (all new)

## Accomplishments
- Ported `Block{ID, Type, Data}` and `ParseBlocks` from eden-biz's `internal/website/block_schema.go` with the tolerant-by-construction contract preserved exactly: absent/non-array `blocks` → nil, non-object/untyped elements skipped, null/missing `data` → nil map, order preserved, duplicate `Type`s with distinct `ID`s all survive
- Built `Page` as a pure data carrier — ordered `Blocks`, `CompanyID uuid.UUID` tenant key (matching `platform/company`/`platform/telephony`), and a closed `PageStatus` enum — with zero transition logic or rendering knowledge, leaving that to `publish.go` (41-03)
- 18 tests across both files; every skip-don't-fail case from the TRD's verify list has a dedicated test, plus order-preservation and duplicate-type survival tests
- Confirmed no concrete block kind (hero/practitioner/bio/accordion/membership_grid) appears anywhere in the package

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Block + ParseBlocks | `go test ./platform/cms/... -count=1 -race -run TestParseBlocks` | 0 | PASS |
| 2: Page + PageStatus | `go test ./platform/cms/... -count=1 -race -run TestPage` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: Block + ParseBlocks** - `8649d7e` (feat)
2. **Task 2: Page + PageStatus** - `30d6f6e` (feat)

**Plan metadata:** (this commit) `docs(41-01): complete content-model TRD`

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build | `go build ./platform/cms/...` | 0 | PASS |
| vet | `go vet ./platform/cms/...` | 0 | PASS |
| test (race) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS |
| catalog-leak grep | `grep -riE "hero\|practitioner\|bio\|accordion\|membership_grid" platform/cms/` | 1 (no matches) | PASS |

Full-module sanity check: `go build ./...` — exit 0, no regressions from the obj-40 merge.

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - Block shape is `{ID string, Type string, Data map[string]any}`, Type opaque — verified by `block.go` + `TestParseBlocks_*`
  - ParseBlocks tolerant on all five documented cases — verified by dedicated tests for each case
  - Order preserved, duplicate Types with distinct IDs survive — `TestParseBlocks_OrderPreservedExactly`, `TestParseBlocks_DuplicateTypesDistinctIDsAllSurvive`
  - Page carries ordered blocks + publication state, no rendering concerns — `page.go` has no HTML/template import, no render method
  - No concrete block kind named in the package — grep gate returns nothing
- **Gate failures:** None

## Files Created/Modified
- `platform/cms/block.go` - `Block` struct and tolerant `ParseBlocks`
- `platform/cms/block_test.go` - 13 tests covering every tolerance case, order, and duplicates
- `platform/cms/page.go` - `Page` struct, `PageStatus` enum (draft/scheduled/published/archived)
- `platform/cms/page_test.go` - 5 tests covering wire-locked status strings, block-order pass-through, zero-value behavior, UTC-instant carrying, and tenant field

## Decisions Made
- Kept `ParseBlocks(extra map[string]any)` reading `extra["blocks"]` rather than a bare `any` parameter — matches the TRD's explicit "port ... EXACTLY" instruction and eden-biz's own signature, so the "absent blocks" and "non-array blocks" language in the TRD's verify list maps directly onto documented behavior.
- Did not port `ResolveBlocks`/`shimLegacyBlocks` — that machinery bridges eden-biz's own pre-migration keyed-frontmatter storage (`extra.pillars`, `extra.blocks_order`, etc.), has no counterpart in a fresh `platform/cms` package, and is outside this TRD's must_haves/verify list.
- Declared `PageStatus` constants in `page.go` (not deferred to 41-03) since the "publication state" truth requires *some* concrete representation for `Page.Status` to hold; the state machine's transition/validation logic is left entirely to `publish.go` per file ownership — `page.go` only declares the enum and carries the fields.

## Deviations from Plan

None - TRD executed exactly as written. Both files created match the `<file_tree>` exactly; no additional files were created (no `doc.go` — that's explicitly owned by 41-06's file_tree, so it was deliberately left out despite `<file_ownership>` permitting it).

## Issues Encountered

Initial draft of `block_test.go` used the word "hero" as an example block-type string in test fixtures and in a block.go doc comment. This is exactly the failure mode the TRD's `<verify>` grep gate exists to catch. Caught before commit by running the grep gate proactively; replaced every occurrence with the neutral name `"banner"` (not in the forbidden list) and re-ran the full verify suite before committing. No commit ever contained a forbidden catalog word.

## Next Objective Readiness

- 41-02 (store) can build directly on `Block`/`Page`: `Store` will persist `Page.Blocks` as JSONB and must round-trip byte-stably, including unknown `Data` keys — `Block`'s JSON tags already match the on-disk shape.
- 41-03 (publish/preview) has `PageStatus` and the two `*time.Time` fields (`ScheduledPublishAt`, `PublishedAt`) ready to build its state machine and due-at evaluation against, without needing to touch `page.go`.
- No blockers.

## Self-Check: PASSED

All claimed files found on disk; both task commits (`8649d7e`, `30d6f6e`) found in git log.

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
