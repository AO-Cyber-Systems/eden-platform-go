---
objective: 40-platform-telephony
trd: "06"
subsystem: telephony
tags: [webhook, hmac, twilio, signalwire, multi-tenant, security]

# Dependency graph
requires:
  - objective: 40-platform-telephony
    provides: "TRD 01 (Provider/Registry/models seam), TRD 02 (SignalWire + Twilio adapters implementing VerifyWebhookSignature/ParseStatusWebhook/ParseInboundSMS), TRD 03 (ConfigStore.LookupBySendingNumber)"
provides:
  - "webhook.go: ReadWebhookBody (bounded raw-body capture that restores r.Body for downstream reads)"
  - "webhook.go: VerifyAndResolve (tenant resolution by receiving number, then per-tenant signature verification)"
  - "webhook.go: WebhookHandler wiring StatusHandler/InboundSMSHandler through capture -> resolve -> verify -> parse -> act"
  - "SMSStatusSink / CallStatusSink / InboundSMSSink / WebhookAuditWriter seams for a caller's persistence layer"
affects: ["40-07 (integration test + README/MIGRATION wiring these handlers end to end)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Webhook tenant resolution reads exactly one body field (To) and only via ConfigStore.LookupBySendingNumber; no other body field is ever consulted for tenant identity"
    - "A single status endpoint serves both SMS and call status callbacks, dispatching by StatusEvent.Kind after one ParseStatusWebhook call, rather than two endpoints that each parse-then-filter"

key-files:
  created:
    - platform/telephony/webhook.go
    - platform/telephony/webhook_test.go
  modified: []

key-decisions:
  - "Tenant resolution necessarily runs before the HMAC check (not after, despite the TRD's mnemonic ordering) because each tenant's Config carries its own signing secret — the secret to verify against cannot be known until a tenant is identified. VerifyAndResolve bundles both steps into one call so no caller can accidentally split them; the security property the TRD cares about (an unverified payload is never parsed or acted on) holds regardless of this internal ordering, and is proven directly by TestWebhookHandler_TamperedBody_RejectedBeforeParse."
  - "Kept SMSStatusSink/CallStatusSink/InboundSMSSink/WebhookAuditWriter as small package-local interfaces rather than depending on 40-05's TCPA/optout types, since 40-04/40-05 are parallel, not-yet-merged waves in this worktree. 40-07 composes a real sink over those types."

patterns-established:
  - "spyProvider test helper: wraps a real Provider built through the package's own factories and counts VerifyWebhookSignature/Parse* calls, giving a direct (not inferred) proof that a rejected webhook's payload was never parsed."

requirements-completed: [R46]

verification:
  gates_defined: 4
  gates_passed: 4
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

duration: ~20min
completed: 2026-09-07
---

# Objective 40 TRD 06: Webhook verify/parse Summary

**Multi-tenant Twilio/SignalWire webhook handler enforcing capture -> resolve-by-receiving-number -> verify -> parse -> act, with the tamper-rejection and tenant-resolution security properties each proven by a dedicated test.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-09-07T22:07:24-04:00
- **Tasks:** 1 (single-deliverable "port" TRD — no discrete `<task>` list in the TRD)
- **Files modified:** 2 (both created)

## Accomplishments

- `ReadWebhookBody` buffers the raw request body once (8 KiB ceiling, matching politihub and justinforme's independently-chosen limit) and restores `r.Body` so every downstream reader — the tenant-resolution form peek, `VerifyWebhookSignature`, and `ParseStatusWebhook`/`ParseInboundSMS` — sees the exact original bytes.
- `VerifyAndResolve` resolves the owning tenant strictly via `ConfigStore.LookupBySendingNumber` keyed on the form's `To` field, builds that tenant's `Provider` from the `Registry`, and only then calls `VerifyWebhookSignature` with the untouched raw bytes — an unverified request never reaches `ParseStatusWebhook`/`ParseInboundSMS`.
- `WebhookHandler.StatusHandler` serves both SMS and call status callbacks from one endpoint, dispatching to `SMSStatusSink` or `CallStatusSink` by `StatusEvent.Kind` after a single parse. `WebhookHandler.InboundSMSHandler` serves inbound SMS, dispatching to `InboundSMSSink` and always responding with empty TwiML.
- Reconciled against justinforme's `call_status_handler.go`: added table-driven test coverage for `busy`/`no-answer`/`canceled`/`ringing`/`in-progress` call-status transitions, which politihub's ported test suite only covered for `completed`. `mapStatus` (models.go, TRD 01/02, not touched here) already maps every one of these correctly — this closes a test-coverage gap, not a code gap.

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: webhook.go + webhook_test.go | `go build ./platform/telephony/...` | 0 | PASS |
| 1: webhook.go + webhook_test.go | `go vet ./platform/telephony/...` | 0 | PASS |
| 1: webhook.go + webhook_test.go | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |
| 1: webhook.go + webhook_test.go | `go build ./...` (full repo regression) | 0 | PASS |

## Task Commits

1. **Task 1: webhook handler + tests** - `c68b215` (feat)

**Plan metadata:** (this commit, `docs(40-06): ...`)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build (package) | `go build ./platform/telephony/...` | 0 | PASS |
| vet | `go vet ./platform/telephony/...` | 0 | PASS |
| test + race | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |
| build (full repo) | `go build ./...` | 0 | PASS |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 4/4
  - "VerifyWebhookSignature requires the RAW, unparsed request body" — `ReadWebhookBody` + `VerifyAndResolve` always restore `r.Body` from the captured `[]byte` before any `Verify`/`Parse` call; `TestReadWebhookBody_RestoresExactBytesForRereading` proves the bytes are unchanged.
  - "An unverified webhook is REJECTED before its payload is parsed or acted on" — `TestWebhookHandler_TamperedBody_RejectedBeforeParse` and `TestWebhookHandler_InboundSMS_TamperedBody_RejectedBeforeParse` use a `spyProvider` that counts `Parse*` calls directly (not inferred from sink behavior) and assert the count is 0 on a rejected request.
  - "ParseStatusWebhook handles both SMS status and call status; Kind disambiguates" — `TestWebhookHandler_StatusHandler_SMSKind` and `TestWebhookHandler_StatusHandler_CallStatusTransitions` (6 sub-cases) each assert the correct sink (and only that sink) fires.
  - "Inbound routing resolves the tenant via ConfigStore.LookupBySendingNumber, not from an untrusted body field" — `TestVerifyAndResolve_ResolvesByReceivingNumberNotBodyField` plants a spurious `CompanyID` form field pointing at a second tenant and proves resolution still keys off `To`, plus a cross-tenant forged-signature case proving the receiving number alone cannot be leveraged to steal a tenant's identity without that tenant's secret.
- **Gate failures:** None

## Required TRD `<verify>` Checks — Output

**1. `go test ./platform/telephony/... -count=1 -race` green:**
```
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	1.942s
```

**2. Tampered body FAILS verification and the payload is never parsed:**
```
=== RUN   TestWebhookHandler_TamperedBody_RejectedBeforeParse
--- PASS: TestWebhookHandler_TamperedBody_RejectedBeforeParse (0.00s)
=== RUN   TestWebhookHandler_InboundSMS_TamperedBody_RejectedBeforeParse
--- PASS: TestWebhookHandler_InboundSMS_TamperedBody_RejectedBeforeParse (0.00s)
```
Both tests assert a non-2xx response, zero sink invocations, and — via `spyProvider.parseCalls` — that `ParseStatusWebhook`/`ParseInboundSMS` were called exactly 0 times.

**3. Both StatusEvent kinds covered (SMS status, call status):**
```
=== RUN   TestWebhookHandler_StatusHandler_SMSKind
--- PASS: TestWebhookHandler_StatusHandler_SMSKind (0.00s)
=== RUN   TestWebhookHandler_StatusHandler_CallStatusTransitions
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/completed_with_duration (0.00s)
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/busy (0.00s)
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/no_answer (0.00s)
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/canceled (0.00s)
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/ringing (0.00s)
    --- PASS: TestWebhookHandler_StatusHandler_CallStatusTransitions/in_progress (0.00s)
```

**4. Tenant resolution uses the receiving number, NOT a body field:**
```
=== RUN   TestVerifyAndResolve_ResolvesByReceivingNumberNotBodyField
--- PASS: TestVerifyAndResolve_ResolvesByReceivingNumberNotBodyField (0.00s)
```

## Files Created/Modified

- `platform/telephony/webhook.go` — `ReadWebhookBody`, `VerifyAndResolve`, `WebhookHandler` (`StatusHandler`, `InboundSMSHandler`), and the `SMSStatusSink`/`CallStatusSink`/`InboundSMSSink`/`WebhookAuditWriter` seams a consumer implements.
- `platform/telephony/webhook_test.go` — `fakeConfigStore`, `spyProvider`, fake sinks/audit writer, and 12 tests covering the happy paths, both rejection paths (413/403/404), the two required security proofs, and the justinforme call-status reconciliation table.

## Decisions Made

1. **Resolve-before-verify, by necessity.** The TRD's mnemonic order is "capture -> verify -> resolve -> parse -> act," but each tenant's `Config` carries its own signing secret (`Config.SignatureToken()`), so the secret to verify against cannot be known until the tenant is resolved. `VerifyAndResolve` therefore internally resolves first, then verifies — matching politihub's `resolveProviderForRequest` (the TRD's cited source) exactly. What the TRD's four `must_haves.truths` actually require — unverified payload never parsed, resolution never trusts an arbitrary body field, raw body always used — all hold regardless of this internal step order, and each is proven by a dedicated test. Documented prominently in `VerifyAndResolve`'s doc comment so a future reader isn't confused by the apparent inversion.
2. **No dependency on 40-04/40-05.** Those waves (resolver*.go, tcpa*/optout_store*.go) are parallel and not yet merged into this worktree. `webhook.go` defines small package-local sink interfaces (`SMSStatusSink`, `CallStatusSink`, `InboundSMSSink`) instead of politihub's concrete `TCPAService`/`SMSStatusUpdater` types, so 40-07's integration test can compose real TCPA/opt-out-backed implementations without this file needing to change.
3. **One status endpoint, not two.** politihub used separate `sms_status`/`call_status` URLs that each ran the full parse and then filtered by `Kind`. Since the must-have truth states `ParseStatusWebhook`... `StatusEvent.Kind disambiguates`, `StatusHandler` parses once and dispatches — avoiding a duplicate parse of the same form.

## Deviations from Plan

None requiring Rule 4. One clarification worth flagging (see Decision 1 above): the literal step order in `<the_order_is_the_security_property>` cannot be implemented verbatim in a multi-tenant, per-tenant-secret design — resolution must precede the HMAC check structurally. The four `must_haves.truths` and all four `<verify>` requirements are met exactly as specified; this is a documentation nuance, not a scope or security gap.

**Total deviations:** 0 auto-fixed.
**Impact on plan:** None — TRD's testable requirements satisfied in full.

## Issues Encountered

None. Build, vet, and race-enabled tests passed on the first run; no fix cycles needed.

## User Setup Required

None — no external service configuration required. `webhook.go` exposes library seams only; wiring into an actual HTTP mux/router is 40-07's scope.

## Next Objective Readiness

`platform/telephony/webhook.go` is ready for 40-07 to compose into an end-to-end integration test (send -> status webhook -> opt-out -> refused send) once 40-04 (resolver) and 40-05 (TCPA/opt-out) land. No blockers.

---
*Objective: 40-platform-telephony*
*Completed: 2026-09-07*
