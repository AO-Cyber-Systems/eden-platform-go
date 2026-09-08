---
objective: 41-platform-cms
job: "03"
subsystem: cms
tags: [go, cms, publish-lifecycle, preview-tokens, hmac, crypto-rand, constant-time]

# Dependency graph
requires:
  - objective: 41-01
    provides: "Page{Status,ScheduledPublishAt,PublishedAt} and the PageStatus enum (draft/scheduled/published/archived) this TRD extends with transition/visibility behavior"
provides:
  - "Page.Schedule/Unschedule/PublishNow/Archive — validated draft<->scheduled->published->archived transitions"
  - "Page.DueForPublish/PublishIfDue — due-at evaluation for an optional reconciliation worker"
  - "Page.VisibleAt/FilterVisible — the public-read-path visibility gate, decoupled from any background publisher"
  - "MintPreviewToken/VerifyPreviewToken/Authorize — crypto/rand-seeded, HMAC-SHA256-signed, constant-time-verified, page+tenant-scoped preview bearer tokens"
affects: [41-04-media, 41-05-settings, 41-06-docs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Validated mutator methods on the domain struct (*Page) for a small closed state graph, instead of composing platform/statemachine — see key-decisions"
    - "Read-path visibility computed from now-vs-due-at directly (VisibleAt), so correctness never depends on a background scheduler having run"
    - "Self-contained signed bearer token (HMAC-SHA256 over a crypto/rand-seeded payload) requiring no persistence/Store — see key-decisions"

key-files:
  created:
    - platform/cms/publish.go
    - platform/cms/publish_test.go
    - platform/cms/preview_token.go
    - platform/cms/preview_token_test.go
  modified: []

key-decisions:
  - "Did NOT compose platform/statemachine for the draft/scheduled/published/archived transitions. statemachine.Machine.Transition couples the state change to persistence through its own Store[S,E] keyed by an opaque instance ID; Page.Status already lives as a field on the Page row that 41-02's Store/PostgresStore (a parallel, not-yet-merged TRD this file does not own) persists in one write. Adopting statemachine would mean either a second, redundant persisted state store (two sources of truth, plus a migration this TRD is barred from adding) or coupling this file to 41-02's not-yet-defined Store interface. Five states / five hand-checked transitions do not need a generic engine to stay correct; validated *Page methods keep exactly one source of truth. Full rationale is inline in publish.go's package-level doc comment."
  - "Did NOT port the donor Scheduler (justinforme/smartWellness app/cms/scheduler.go) — it is not in this TRD's file list. VisibleAt/FilterVisible make it unnecessary for CORRECTNESS: the public read path compares `now` to ScheduledPublishAt directly, so a due page is visible even if no worker has run, and a not-yet-due page is invisible regardless of Status bookkeeping lag. PublishIfDue is still provided as the mutating half for an optional out-of-band reconciliation worker (admin-UI Status display), but nothing in this TRD's must-haves depends on it running."
  - "Preview tokens are a self-contained HMAC-SHA256-signed credential (nonce + pageID + companyID + expiresAt, verified by recomputing the signature) rather than an opaque random token plus a server-side lookup store. This TRD does not own platform/cms/store.go/store_pg.go or migrations/platform (41-02's seam), so a persisted grants table was not available to build against. A self-verifying signed token needs no store at all. The donor sources use the same signed-token shape but derive it deterministically (no crypto/rand at all); this file adds a crypto/rand nonce into the signed payload specifically to satisfy this TRD's explicit 'unguessable (crypto/rand)' requirement — proven by TestMintPreviewToken_Unguessable_UsesRandomNonce, which shows two mints for the identical (secret, page, tenant) never produce the same token."
  - "Chose crypto/subtle.ConstantTimeCompare explicitly (over hmac.Equal, which wraps the same primitive) so the constant-time property required by this TRD's <constraints> is visible directly in the code, not implied through a library call."
  - "PublishedAt is set only the FIRST time a page is ever published, matching page.go's existing doc comment ('left unchanged by a later Archive') — verified by TestPublishNow_FromDraft_SetsPublishedAtOnce, which archives then re-drafts-and-republishes a page and asserts PublishedAt did not move."

patterns-established:
  - "A package doc comment can carry an architectural-decision rationale (why NOT X) inline, not just describe what the file does — done here for the statemachine and scheduler decisions so a future reader hits the reasoning before re-proposing either."

requirements-completed: [R47]

# Verification evidence
verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: ~45min
completed: 2026-09-08
---

# Objective 41 TRD 03: Publish Lifecycle + Preview Tokens Summary

**Draft/scheduled/published/archived transitions with a due-at read-path gate independent of any background worker, plus crypto/rand-seeded HMAC-SHA256 preview bearer tokens with constant-time verification and page+tenant scoping.**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-09-08 (after merging Wave 1 / plan/obj-40-platform-telephony into worktree)
- **Completed:** 2026-09-08
- **Tasks:** 2 (publish.go + publish_test.go; preview_token.go + preview_token_test.go)
- **Files modified:** 4 (all new)

## Accomplishments
- `Page.Schedule/Unschedule/PublishNow/Archive` — five validated transitions over the four `PageStatus` values already declared in `page.go` (41-01), each rejecting an invalid current state with `ErrInvalidTransition`
- `Page.Schedule` normalizes any input `time.Time` to UTC before storing — proven with a test that passes a `UTC-5`-located instant and asserts the stored `ScheduledPublishAt` is both UTC-located and the correct absolute instant
- `Page.VisibleAt`/`FilterVisible` — the public-read-path gate compares `now` against `ScheduledPublishAt` directly, so a scheduled-but-not-due page is invisible and a due scheduled page is visible, in both cases regardless of whether any background worker has flipped `Status`
- `MintPreviewToken`/`VerifyPreviewToken`/`Authorize` — a self-contained bearer token: 16 bytes of `crypto/rand` nonce + pageID + companyID + expiry, HMAC-SHA256-signed, verified with `crypto/subtle.ConstantTimeCompare`, refused if expired, tampered, wrong-secret, or scoped to a different page/tenant
- 31 new tests (19 in `publish_test.go`, 12 in `preview_token_test.go`) on top of the 18 inherited from 41-01 — 49 total, all green under `-race`

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: publish.go (transitions + due-at gate) | `go test ./platform/cms/... -count=1 -race -run 'TestSchedule\|TestUnschedule\|TestPublishNow\|TestArchive\|TestDueForPublish\|TestPublishIfDue\|TestVisibleAt\|TestFilterVisible'` | 0 | PASS |
| 2: preview_token.go (mint/verify/authorize) | `go test ./platform/cms/... -count=1 -race -run 'PreviewToken\|Authorize'` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: publish.go + publish_test.go** - `25c2a67` (feat)
2. **Task 2: preview_token.go + preview_token_test.go** - `21f1a0f` (feat)

**Plan metadata:** (this commit) `docs(41-03): publish lifecycle + preview tokens SUMMARY`

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build | `go build ./platform/cms/...` | 0 | PASS |
| vet | `go vet ./platform/cms/...` | 0 | PASS |
| test (race) | `go test ./platform/cms/... -count=1 -race` | 0 | PASS (49 tests) |
| catalog-leak grep | `grep -riE "hero\|practitioner\|bio\|accordion\|membership_grid" platform/cms/` | 1 (no matches) | PASS |
| full-module build | `go build ./...` | 0 | PASS (no regressions from obj-40/41-01 merge) |

## Required <verify> Checks (from TRD)

1. **`go test ./platform/cms/... -count=1 -race` green** — confirmed, exit 0, 49/49 tests pass.
2. **A test proves a scheduled-but-not-due page is NOT returned by the public path** — `TestVisibleAt_ScheduledNotDue_NotServed` (schedules 1h in the future, asserts `VisibleAt(now)` is false) and `TestFilterVisible_MixedStatuses` (asserts a not-due page is absent from the filtered slice).
3. **A test proves an expired preview token is refused** — `TestVerifyPreviewToken_ExpiredToken_Refused` (mints a token whose expiry is 1h in the past, asserts `errors.Is(err, ErrPreviewTokenExpired)`).
4. **A test proves a token scoped to page A does not grant access to page B, or to another tenant's page** — `TestAuthorize_ScopedToWrongPage_Refused` and `TestAuthorize_ScopedToWrongTenant_Refused`, both asserting `errors.Is(err, ErrPreviewTokenScope)`.

## TDD Evidence

Not applicable — this TRD's tasks are `type: auto`-equivalent (no `tdd="true"` markers in the plan text); implementation and its test file were written and verified together per deliverable, matching this repo's `test_pairing` convention rather than a strict RED/GREEN/REFACTOR cadence. `test_pairing: true` in frontmatter reflects that every source file has a corresponding, currently-green test file.

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 5/5
  - Publication state is explicit (draft/scheduled/published) — `PageStatus` (41-01) + this TRD's validated transitions
  - Scheduled publish stores an absolute UTC instant — `Page.Schedule` normalizes with `.UTC()`; proven by `TestSchedule_FromDraft_SetsScheduledStateAndUTCInstant` using a non-UTC input zone
  - Preview token grants time-bounded access to unpublished content, scoped to one page + one tenant — `PreviewGrant{PageID,CompanyID,ExpiresAt}` + `Authorize`
  - Preview tokens are unguessable (crypto/rand) and expire; an expired token is refused — `crypto/rand.Read` nonce + `PreviewTokenTTL`; `TestVerifyPreviewToken_ExpiredToken_Refused`, `TestMintPreviewToken_Unguessable_UsesRandomNonce`
  - Scheduled-but-not-due content is not served by the public path — `VisibleAt`/`FilterVisible`; `TestVisibleAt_ScheduledNotDue_NotServed`
- **Gate failures:** None

## Files Created/Modified
- `platform/cms/publish.go` - `Page` transition methods (`Schedule`, `Unschedule`, `PublishNow`, `Archive`), due-at evaluation (`DueForPublish`, `PublishIfDue`), and the public-read-path gate (`VisibleAt`, `FilterVisible`); inline doc comment explains why `platform/statemachine` and the donor `Scheduler` were not used
- `platform/cms/publish_test.go` - 19 tests: every transition's happy path and its rejected-from-wrong-state case, UTC-zone normalization, due-at boundary cases (not due / exactly due / past due), and the two must-have visibility proofs
- `platform/cms/preview_token.go` - `MintPreviewToken`, `VerifyPreviewToken`, `Authorize`, `PreviewGrant`; `crypto/rand` nonce, HMAC-SHA256 signing, `crypto/subtle.ConstantTimeCompare` verification, page+tenant scope enforcement
- `platform/cms/preview_token_test.go` - 12 tests: round-trip, nonce-uniqueness (unguessability proof), expired/not-yet-expired boundary, wrong-secret, tampered-payload, malformed-token, empty-secret, nil-ID rejection, and the two must-have scope-isolation proofs

## Decisions Made

See `key-decisions` in frontmatter for the full rationale on each. Summary:
- Hand-rolled `*Page` transition methods instead of composing `platform/statemachine` — persistence-coupling mismatch with 41-02's not-yet-available `Store`, documented inline in `publish.go`.
- Did not port the donor `Scheduler` (background polling/lease worker) — out of this TRD's file list, and unnecessary for correctness given `VisibleAt`'s synchronous due-at check.
- Preview tokens are a stateless, self-verifying signed credential (no `Store`/persistence seam needed), strengthened over the donor's deterministic-HMAC design with a `crypto/rand` nonce to satisfy this TRD's explicit "unguessable (crypto/rand)" requirement.

## Deviations from Plan

None from the TRD's required scope. Two internal test-authoring corrections, both caught and fixed before the task-2 commit:

1. **[Test bug, not a deviation from the plan] `TestMintAndVerifyPreviewToken_RoundTrips` compared `grant.ExpiresAt` for exact equality against the mint-time value.** The token encodes expiry at Unix-seconds precision (`previewExpiresLen = 8` bytes, `time.Unix(...,0)`), so the decoded `PreviewGrant.ExpiresAt` legitimately loses sub-second precision relative to what `MintPreviewToken` returned in-process. Fixed the assertion to a sub-second tolerance; no production-code change.
2. **[Test bug, not a deviation from the plan] `TestVerifyPreviewToken_TamperedPayload_Refused` originally flipped only the LAST character of the base64url token.** 88 raw bytes (56-byte payload + 32-byte HMAC) encode to a base64 string whose final symbol carries only 2 real bits of the last byte; Go's non-strict `base64.RawURLEncoding` decoder ignores that symbol's unused low bits, so flipping only the last character round-tripped to the SAME decoded bytes and the "tampered" token verified successfully — a false pass, caught by running the full suite under `-race` after the individual `-run` pass looked green. Fixed by flipping a character in the middle of the token instead (always inside a fully-significant byte); re-ran 10x under `-race` to confirm it's not flaky. `crypto/subtle.ConstantTimeCompare` and the signing logic in `preview_token.go` were never in question — this was purely a test-harness bug in how tampering was simulated.

**Impact on plan:** None — both were caught and fixed before their respective commit; no production code changed as a result.

## Issues Encountered

The base64-encoding edge case above (issue 2) is worth flagging for any future work in this file: a single-bit/character mutation test against the LAST symbol of a `RawURLEncoding` string of this length is not a valid tamper simulation. Documented in the test's own comment so it isn't rediscovered the hard way again.

## User Setup Required

None - no external service configuration required. `MintPreviewToken`/`VerifyPreviewToken`/`Authorize` take `secret []byte` as a caller-supplied parameter; wiring an env-configured signing key belongs to whichever future TRD builds the HTTP handler around this package (out of this TRD's scope).

## Next Objective Readiness

- `platform/cms/publish.go` and `preview_token.go` are ready for a handler layer to call directly — no persistence dependency, so nothing here blocks on 41-02's `Store`/`PostgresStore` landing.
- 41-02 (parallel, store.go/store_pg.go) is unaffected by this TRD: no shared symbols, no shared files. `Page.Status`/`ScheduledPublishAt`/`PublishedAt` remain plain fields 41-02's `Store.Save` can persist as-is; this TRD only added validated ways to MUTATE those fields before a caller persists them.
- Gap flagged (not a blocker): if a later TRD needs preview-token revocation-before-expiry or a listing of currently-live preview links, that requires a persisted grants table this file deliberately does not define — see the `key-decisions` note in `preview_token.go`'s doc comment.
- No blockers for 41-04/41-05/41-06.

## Self-Check: PASSED

All four claimed files found on disk (`platform/cms/publish.go`, `platform/cms/publish_test.go`, `platform/cms/preview_token.go`, `platform/cms/preview_token_test.go`); both task commits (`25c2a67`, `21f1a0f`) found in `git log`.

---
*Objective: 41-platform-cms*
*Completed: 2026-09-08*
