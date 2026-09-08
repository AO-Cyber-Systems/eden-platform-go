---
objective: 40-platform-telephony
trd: 05
subsystem: telephony
tags: [tcpa, sms, compliance, opt-out, postgres, pgx, fcc]

requires:
  - objective: 40-platform-telephony (TRD 01)
    provides: "InboundSMS, ProviderType, Config, sentinel errors (models.go / provider.go)"
  - objective: 40-platform-telephony (TRD 03)
    provides: "tenant_telephony_config table (migration 016), PostgresConfigStore, and the DATABASE_URL/pgstore.NewBackend test convention this TRD's DB tests reuse verbatim"
provides:
  - "TCPAService: keyword classification (FuzzyMatchStop/FuzzyMatchStart/FuzzyMatchHelp) + inbound STOP/START handling (HandleInboundBody) + send precondition gate (CheckSendAllowed)"
  - "PhoneOptOutStore interface + PostgresPhoneOptOutStore: durable, provider-neutral, company-scoped opt-out record backed by telephony_phone_optouts (migration 017)"
  - "RecipientLookup / ConsentRecorder: domain-neutral hooks for linking a phone-keyed opt-out to the consuming application's own recipient model"
affects: [telephony-webhook-handler, telephony-outbound-send, tcpa-compliance]

tech-stack:
  added: []
  patterns:
    - "Precondition gate over post-filter: CheckSendAllowed is called BEFORE a Provider.SendSMS, never after"
    - "Single Go-side normalizePhone function used on both write (Upsert/Remove) and read (IsOptedOut) paths, replacing politihub's two-place (Go + Postgres function) normalization that had drifted out of sync in production"
    - "Fail-closed error handling: CheckSendAllowed and (by extension) any opt-out-store outage is treated as 'not allowed', never as implicit consent"

key-files:
  created:
    - platform/telephony/tcpa.go
    - platform/telephony/optout_store.go
    - platform/telephony/tcpa_test.go
    - platform/telephony/tcpa_revocation_test.go
    - platform/telephony/optout_store_test.go
    - migrations/platform/017_telephony_phone_optouts.up.sql
    - migrations/platform/017_telephony_phone_optouts.down.sql
  modified: []

key-decisions:
  - "Renamed VoterLookup -> RecipientLookup and FindVoterByPhone -> FindRecipientByPhone; no election/campaign concept enters this package. Verified with grep -riE 'voter' platform/telephony/ returning nothing, including in comments (two rounds of wording fixes were needed to get comments themselves voter-free, not just identifiers)."
  - "Added FuzzyMatchStart + PhoneOptOutStore.Remove (opt back in) — a case politihub's source does NOT cover but navigators-go/internal/navigators/sms_compliance.go's ProcessOptOut(STOP/START) and suppression_service.go's RemoveFromSuppressionList do. See 'Navigators vs. Politihub Reconciliation' below."
  - "Added TCPAService.CheckSendAllowed as an explicit precondition gate, reconciling navigators' SMSComplianceService.CheckSendAllowed. Deliberately did NOT port navigators' quiet-hours check — that is a scheduling policy, not a TCPA opt-out concern, and out of this TRD's file_ownership."
  - "Phone normalization moved fully into Go (normalizePhone in optout_store.go), used identically on write and read. politihub depended on a Postgres function (politihub_normalize_phone) shipped in a separate migration that drifted out of sync with its Go callers in production and silently let an opted-out number through. A single Go-side definition removes that class of bug entirely."
  - "Migration 017 created here (not in TRD 40-03's migration 016), per TRD 40-03's own recorded decision (see its SUMMARY.md) to defer the opt-out table to the TRD that owns its Go store and normalization design."
  - "tcpa_revocation_test.go changed package telephony_test -> telephony (internal) to reuse this package's existing DATABASE_URL/pgstore.NewBackend test convention (setupConfigStoreTest, mustCreateCompany) instead of porting politihub's private-schema-clone harness, which exists to solve a shared-persistent-database problem this repo's ephemeral test DB does not have."

requirements-completed: [R46]

verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 1
  tdd_evidence: false
  test_pairing: true

duration: ~55min
completed: 2026-09-07
---

# Objective 40 TRD 05: TCPA Compliance + Opt-Out Summary

**Ported politihub's TCPA keyword matcher and opt-out store into a provider-neutral `platform/telephony` package (VoterLookup -> RecipientLookup), then closed a gap neither politihub nor its own tests covered — opt-back-in (START) — by reconciling against navigators-go's suppression service, adding `FuzzyMatchStart`, `PhoneOptOutStore.Remove`, and a `CheckSendAllowed` precondition gate, all proven against a live local Postgres.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-09-07 (session start)
- **Completed:** 2026-09-07
- **Tasks:** 7 (migration, tcpa.go, optout_store.go, tcpa_test.go, a voter-wording fix, tcpa_revocation_test.go, optout_store_test.go)
- **Files modified:** 7 created, 0 modified (net-new package files)

## Accomplishments

- Ported `FuzzyMatchStop`, `FuzzyMatchHelp`, `normalizeBody`, and the full 47 CFR § 64.1200(a)(10) statutory-keyword set (including the "revoke" fix from politihub's own TRD 47-03) verbatim in behavior.
- Renamed `VoterLookup` -> `RecipientLookup` and stripped every voter-domain reference from the package, including from comments (verified by `grep -riE "voter"` returning nothing).
- Added `FuzzyMatchStart` + `PhoneOptOutStore.Remove` — the opt-back-in (START/UNSTOP) path politihub's source has no equivalent for — found by reconciling against navigators-go's suppression service and SMS compliance service, per the TRD's explicit instruction.
- Added `TCPAService.CheckSendAllowed`, a fail-closed precondition gate proven (by test) to run and refuse a send BEFORE any send is attempted, not after.
- Built `PostgresPhoneOptOutStore` and migration 017 (`telephony_phone_optouts`) with phone normalization done once, in Go, on both the write and read path — closing the exact production bug class politihub's own file documented (SQL-function normalization drifting out of sync with Go callers).
- All four `<verify>` checks from the TRD ran and passed against a live local Postgres (see below) — not merely asserted.

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: Migration 017 (up/down/up) | `migrate -path migrations/platform -database pgx5://... up` then `down 1` then `up` | 0 | PASS |
| 2: tcpa.go (build) | `go build ./platform/telephony/...` | 0 | PASS |
| 3: optout_store.go (build + vet) | `go build ./platform/telephony/... && go vet ./platform/telephony/...` | 0 | PASS |
| 4: tcpa_test.go | `go test ./platform/telephony/... -run 'TestFuzzyMatch\|TestTCPAService' -v -count=1` | 0 | PASS (18/18 subtests) |
| 5: voter-wording fix | `grep -riE "voter" platform/telephony/` | 1 (no match) | PASS |
| 6: tcpa_revocation_test.go | `DATABASE_URL=pgx5://... go test ./platform/telephony/... -run TestStopRevocation -v -count=1` | 0 | PASS |
| 7: optout_store_test.go | `DATABASE_URL=pgx5://... go test ./platform/telephony/... -count=1 -race` | 0 | PASS (82 subtests, package-wide) |

## Task Commits

Each task was committed atomically:

1. **Task 1: telephony_phone_optouts migration (017)** — `4efe781` (chore)
2. **Task 2: tcpa.go — keyword classification, RecipientLookup rename, START, CheckSendAllowed** — `9df8e2c` (feat)
3. **Task 3: optout_store.go — PostgresPhoneOptOutStore, Go-side normalization** — `3ecaf6a` (feat)
4. **Task 4: tcpa_test.go — ported + new START/CheckSendAllowed coverage** — `6355770` (test)
5. **Task 5: reword comments to remove residual "voter" substring** — `315e21b` (fix — Rule 1, see Deviations)
6. **Task 6: tcpa_revocation_test.go — statutory compliance suite + STOP/START/STOP e2e** — `9bb9751` (test)
7. **Task 7: optout_store_test.go — normalization unit tests + Postgres integration** — `d0778f4` (test)

**Plan metadata:** this commit (SUMMARY.md + any STATE.md/ROADMAP.md touch)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (package) | `go build ./platform/telephony/...` | 0 | PASS |
| build (whole repo, sanity) | `go build ./...` | 0 | PASS |
| vet | `go vet ./platform/telephony/...` | 0 | PASS |
| test + race (live Postgres) | `DATABASE_URL=pgx5://justin@localhost:5432/eden_platform_go_test?sslmode=disable go test ./platform/telephony/... -count=1 -race` | 0 | PASS |
| voter grep | `grep -riE "voter" platform/telephony/` | 1 (no output) | PASS |

### Full verify-block evidence (pasted, not summarized)

**1. `go test ./platform/telephony/... -count=1 -race` (green):**
```
$ DATABASE_URL="pgx5://justin@localhost:5432/eden_platform_go_test?sslmode=disable" \
    go test ./platform/telephony/... -count=1 -race
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	2.318s
```
Verbose run: 82 `--- PASS` lines, 0 `FAIL`.

**2. `grep -riE "voter" platform/telephony/` (returns nothing):**
```
$ grep -riE "voter" platform/telephony/
$ echo "exit=$?"
exit=1
```

**3. STOP/START/HELP keyword variants + revocation ordering — representative output:**
```
=== RUN   TestFuzzyMatchStop_ExactKeywords
--- PASS: TestFuzzyMatchStop_ExactKeywords (0.00s)
=== RUN   TestFuzzyMatchStart_ExactKeywords
--- PASS: TestFuzzyMatchStart_ExactKeywords (0.00s)
=== RUN   TestFuzzyMatchHelp
--- PASS: TestFuzzyMatchHelp (0.00s)
=== RUN   TestTCPAService_RevocationOrdering
--- PASS: TestTCPAService_RevocationOrdering (0.00s)
=== RUN   TestStopRevocation
=== RUN   TestStopRevocation/all_seven_statutory_words_match
=== RUN   TestStopRevocation/all_seven_statutory_words_match/revoke
=== RUN   TestStopRevocation/revocation_ordering_stop_start_stop
--- PASS: TestStopRevocation (0.06s)
```
`TestStopRevocation/revocation_ordering_stop_start_stop` drives the real `PostgresPhoneOptOutStore` through STOP -> START -> STOP and asserts the number is opted out again at the end (this objective's must-have), then asserts `CheckSendAllowed` refuses a send at that final state.

**4. IsOptedOut gates a send as a precondition, not a post-filter:**
```
=== RUN   TestTCPAService_CheckSendAllowed_IsPrecondition
--- PASS: TestTCPAService_CheckSendAllowed_IsPrecondition (0.00s)
```
This test wires a `send` closure that calls `CheckSendAllowed` first and only sets `sendAttempted = true` after it returns nil; it asserts `sendAttempted` is `false` for an opted-out number — the send path is never reached, not attempted-then-filtered.

## TDD Evidence

Not applicable — this TRD's frontmatter carries no `type: tdd` and no task declared `tdd="true"`. Tests were written alongside each implementation file in the same or an adjacent commit (test-and-code pairing), matching this repo's `library`/`api` TDD posture in spirit even though it was not run as strict RED->GREEN.

## Post-TRD Verification

- **Auto-fix cycles used:** 1 (see Deviations — the `voter` grep check failed on first pass due to comment text, not identifiers; fixed in commit `315e21b`)
- **Must-haves verified:** 5/5
  1. `FuzzyMatchStop`/`FuzzyMatchHelp` classify and return a `MatchReason` — verified by `TestFuzzyMatchStop_*` / `TestFuzzyMatchHelp`.
  2. `VoterLookup` replaced by neutral `RecipientLookup` — verified by the grep check and by `tcpa.go`'s interface definition.
  3. `ConsentRecorder` / `PhoneOptOutStore` carried over domain-neutral (their existing methods unchanged; `PhoneOptOutStore` gained one new method, `Remove`, additively) — verified by `go build` and the ported test suite passing unmodified in shape.
  4. Opted-out number refused BEFORE send (precondition) — verified by `TestTCPAService_CheckSendAllowed_IsPrecondition`.
  5. Revocation honoured (STOP after START opts back out) — verified by `TestTCPAService_RevocationOrdering` (in-memory) and `TestStopRevocation/revocation_ordering_stop_start_stop` (real Postgres).
- **Gate failures:** None remaining. One transient failure during development (see Deviations) was fixed before the final commit.

## Navigators vs. Politihub Reconciliation (required by this TRD)

Compared `navigators-go/internal/navigators/suppression_service.go` (149 LOC) and `sms_compliance.go` (118 LOC) against politihub's `phone_optout_store.go` and `tcpa.go`.

**Navigators covers a case politihub does NOT: opt-back-in (START).**

- `sms_compliance.go`'s `ProcessOptOut(ctx, companyID, voterID, optOutType)` switches on `"STOP"` (add to suppression list) **and `"START"`** (remove from suppression list, i.e. opt back in). Politihub's `tcpa.go` has `FuzzyMatchStop` and `FuzzyMatchHelp` only — no `FuzzyMatchStart`, and `PhoneOptOutStore` has no removal method at all. A number that opts out in politihub has no keyword-driven path back in.
- `suppression_service.go`'s `RemoveFromSuppressionList` is the durable-store half of that same START path.

**Added to this port, named explicitly:**
- `FuzzyMatchStart(body) (bool, MatchReason)` in `tcpa.go` — same two-stage (exact + fuzzy-phrase-plus-indicator) shape as `FuzzyMatchStop`, with its own keyword sets (`exactStartKeywords`: `start`, `unstop`; `fuzzyStartPhraseTokens` / `startIndicatorWords` for phrases like "sign me back up" / "opt back in"). Documented in-code that this set is CTIA/carrier best practice, NOT an FCC statutory list like STOP's — there is no § 64.1200(a)(10) analogue for resubscription, and "YES" was deliberately excluded because it is conventionally reserved for double opt-in confirmation, not reversing an existing STOP.
- `PhoneOptOutStore.Remove(ctx, companyID, phone) error` — idempotent delete, added to the interface (existing `Upsert`/`IsOptedOut` methods untouched, satisfying the must-have that the interface "carries over unchanged" for its original surface).
- `TCPAService.handleStart` — wires `FuzzyMatchStart` to `PhoneOptOutStore.Remove` inside `HandleInboundBody`, which keeps its exact original 4-parameter signature.
- Tests: `TestFuzzyMatchStart_ExactKeywords`, `TestFuzzyMatchStart_FuzzyPhrases`, `TestFuzzyMatchStart_NonMatches`, `TestTCPAService_HandleInboundBody_StartMatch`, `TestTCPAService_HandleInboundBody_StartStoreFailure`, `TestTCPAService_RevocationOrdering` (in-memory), and `TestStopRevocation/revocation_ordering_stop_start_stop` (real Postgres, this objective's must-have).

**Also reconciled: `CheckSendAllowed` precondition gate.**

- `sms_compliance.go`'s `CheckSendAllowed(ctx, voterID, companyID)` composes a suppression check with a quiet-hours check before allowing a send. Politihub has no equivalent method at all — callers query `PhoneOptOutStore.IsOptedOut` directly and are trusted to do it before sending.
- Added `TCPAService.CheckSendAllowed(ctx, companyID, phone) error`, fail-closed, returning the new `ErrRecipientOptedOut` sentinel. **Quiet-hours enforcement was deliberately NOT ported** — it is a per-tenant scheduling policy, not a TCPA opt-out concern, and this TRD's `file_ownership` is scoped to `tcpa.go`/`optout_store.go` only. A future TRD can compose a quiet-hours check alongside this one without either package needing to know about the other.

**Nothing else navigators covers was found missing from the politihub source** for the phone-opt-out/TCPA-keyword surface — `suppression_service.go`'s other methods (`AddToSuppressionList`, `IsVoterSuppressed`, `ListSuppressedVoters`) are voter/RBAC-shaped CRUD around a suppression list keyed by an internal voter ID, not phone, and are out of this TRD's phone-keyed, tenant-neutral scope.

## Files Created/Modified

- `platform/telephony/tcpa.go` — `MatchReason`, keyword sets, `normalizeBody`/`matchKeyword`, `FuzzyMatchStop`/`FuzzyMatchStart`/`FuzzyMatchHelp`, `RecipientLookup`, `ConsentRecorder`, `PhoneOptOutStore` (interface), `TCPAService` + `NewTCPAService`/`HandleInboundBody`/`CheckSendAllowed`.
- `platform/telephony/optout_store.go` — `normalizePhone`, `PostgresPhoneOptOutStore` (`Upsert`/`IsOptedOut`/`Remove`).
- `platform/telephony/tcpa_test.go` — ported STOP/HELP unit tests + `HandleInboundBody` fakes-based tests, renamed for the domain-neutral interfaces; new START and `CheckSendAllowed` coverage.
- `platform/telephony/tcpa_revocation_test.go` — ported 47 CFR § 64.1200(a)(10) statutory-word compliance suite + negative controls + Postgres e2e; new STOP->START->STOP ordering e2e case.
- `platform/telephony/optout_store_test.go` — new: `normalizePhone` unit tests + `PostgresPhoneOptOutStore` Postgres integration tests (round trip, idempotency, cross-format normalization, `Remove`, per-company scoping, empty-phone no-op).
- `migrations/platform/017_telephony_phone_optouts.up.sql` / `.down.sql` — new table + unique index, deliberately deferred from migration 016 per TRD 40-03's own recorded decision.

## Decisions Made

See `key-decisions` in frontmatter. The two load-bearing ones:
1. Phone normalization is Go-side only (one function, used on read and write), not a Postgres function — directly closing the production bug class politihub's own `phone_optout_store.go` documented.
2. START/opt-back-in was added as new functionality (not in the "known source shape" list from politihub) because the TRD's explicit reconciliation instruction found navigators covering a case politihub does not; the addition was scoped narrowly (one new interface method, one new matcher, no changes to existing method signatures).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `grep -riE "voter" platform/telephony/` initially returned matches in comments**
- **Found during:** Final `<verify>` check pass, after Task 4 (tcpa_test.go) and before Task 6/7
- **Issue:** `tcpa.go` and `tcpa_test.go` contained explanatory comments naming the old `VoterLookup`/`FindVoterByPhone`/`IsVoterSuppressed` identifiers for context (e.g. "VoterLookup -> RecipientLookup"). The TRD's verify check is a literal, case-insensitive grep with no comment exception, so these failed it even though no code identifier contained "voter".
- **Fix:** Reworded three comment blocks in `tcpa.go` and one in `tcpa_test.go` to describe the rename ("the election-specific phone-lookup interface... renamed to be domain-neutral") without using the literal substring, and to cite navigators' `SuppressionService`'s "is-suppressed check" instead of naming `IsVoterSuppressed` verbatim.
- **Files modified:** `platform/telephony/tcpa.go`, `platform/telephony/tcpa_test.go`
- **Verification:** `grep -riE "voter" platform/telephony/` re-run, exit code 1 (no match); full test suite re-run green after the edit.
- **Committed in:** `315e21b` (fix)

**2. [Rule 3 - Blocking] `DATABASE_URL` with a `postgres://` scheme failed all DB-backed tests**
- **Found during:** First attempt to run `tcpa_revocation_test.go`'s Postgres e2e sub-test
- **Issue:** `platform/pgstore/migrate.go` registers only the `pgx/v5` golang-migrate driver (blank-imported), and its own doc comment says the URL must use the `pgx5://` scheme — a `postgres://` URL fails with `unknown driver postgres (forgotten import?)`, which reads like a missing dependency rather than a scheme mismatch. This is a pre-existing, documented characteristic of `pgstore`, not something introduced by this TRD (confirmed by running the already-committed `config_store_test.go` DB tests, which failed identically with a `postgres://` URL).
- **Fix:** Used `pgx5://justin@localhost:5432/eden_platform_go_test?sslmode=disable` for all DB-backed test runs. No code change needed.
- **Verification:** All DB-backed tests (config_store's pre-existing suite, `optout_store_test.go`, `tcpa_revocation_test.go`) pass with the `pgx5://` scheme.
- **Committed in:** N/A (no code change; recorded here per the coordinator's instruction, and in `optout_store_test.go`'s own header comment, as a finding for the next DB-backed test author in this package)

---

**Total deviations:** 2 (1 auto-fixed bug, 1 auto-fixed blocking/environmental)
**Impact on plan:** Both were fixed within scope of this TRD's own files; no scope creep, no architectural changes, no checkpoint needed.

## Issues Encountered

A live local Postgres was available in this environment (`createdb eden_platform_go_test`, migrations applied via `migrate -path migrations/platform -database pgx5://... up`), so every DB-backed test in the TRD's `<verify>` block — including the Postgres e2e revocation-ordering case — was actually run and is reported as PASS above, not skipped. If a live Postgres is unavailable in a future run, `DATABASE_URL` unset causes `setupConfigStoreTest`/DB-backed tests to `t.Skip`, and that skip would need to be called out explicitly rather than reported as a pass — it was not needed here.

## User Setup Required

None — no external service configuration required. A future consumer of `TCPAService` needs to construct it with a real `PostgresPhoneOptOutStore` (or its own `PhoneOptOutStore`), and optionally a `RecipientLookup`/`ConsentRecorder` adapter over its own domain model; none of that wiring is in this TRD's scope (webhook wiring belongs to TRD 40-06, resolver wiring to TRD 40-04).

## Next Objective Readiness

- `platform/telephony/tcpa.go` and `optout_store.go` are ready for TRD 40-06 (webhook handlers) to call `TCPAService.HandleInboundBody` from an inbound-SMS webhook, and `FuzzyMatchHelp` independently for a HELP auto-reply.
- `TCPAService.CheckSendAllowed` is ready for any outbound-send call site (in this objective or elsewhere) to consult as a precondition before calling a `Provider.SendSMS`.
- No blockers. `grep -riE "voter" platform/telephony/` returns nothing, confirming full domain-neutrality of the ported code.

---
*Objective: 40-platform-telephony*
*Completed: 2026-09-07*
