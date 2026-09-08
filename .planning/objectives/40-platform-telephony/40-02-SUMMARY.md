---
objective: 40-platform-telephony
job: "02"
subsystem: telephony
tags: [telephony, twilio-go, signalwire, twilio, webhook-signature, provider-abstraction]

# Dependency graph
requires: ["40-01"]
provides:
  - "signalwireProvider: default Provider adapter wrapping twilio-go against SignalWire's Twilio-compatible REST API"
  - "twilioProvider: second Provider adapter wrapping twilio-go directly against Twilio's REST API"
  - "swBaseClient host-rewrite shim (url_rewrite.go) making twilio-go usable against a non-Twilio host"
affects: [40-03-config-resolver, 40-04-webhook-handler]

# Tech tracking
tech-stack:
  added: ["github.com/twilio/twilio-go v1.30.4 (pinned exact version)"]
  patterns:
    - "Both adapters wrap twilio-go's RestClient + RequestValidator; the only divergence is transport host (SignalWire rewrites via swBaseClient) and the accepted signature header (SignalWire also accepts X-Twilio-Signature as a fallback)"
    - "Signature validation always delegates to twilio-go's RequestValidator.ValidateBody — this package never reimplements the HMAC-SHA1 comparison"
    - "ProviderFactory functions (NewSignalWireFactory, NewTwilioFactory) are the only integration point a third adapter needs — no registry/resolver code changes required to add one"

key-files:
  created:
    - platform/telephony/url_rewrite.go
    - platform/telephony/signalwire.go
    - platform/telephony/signalwire_test.go
    - platform/telephony/twilio.go
    - platform/telephony/twilio_test.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "Pinned github.com/twilio/twilio-go to the EXACT version politihub's source already vetted against, v1.30.4, not @latest — the url_rewrite.go shim's entire justification (twilio-go hardcodes https://api.twilio.com per-service with no ClientParams.BaseURL) is a version-specific fact confirmed against this exact release. go.sum hashes for v1.30.4 match politihub's go.sum byte-for-byte, confirming this is genuinely the same vetted release, not a coincidental version match."
  - "InitiateBridge params renamed toVolunteer/voterNumber -> toA/toB in both adapters, matching the seam's neutral vocabulary already established in platform/telephony/provider.go by TRD 40-01. Comments describing the masked two-leg bridge now say 'tenant DID' instead of 'campaign DID'."
  - "Stripped every external-app/source-repo reference from shipped doc comments (no 'politihub', 'justinforme', or 'Obj 25' mentions) per the precedent TRD 40-01 set in its SUMMARY: the source is an unrelated non-portfolio product. Where the source commented '10DLC gate ... NOT carried over,' this TRD generalizes to 'compliance/consent gating ... is application policy' without naming the app it was lifted out of."
  - "Did NOT touch platform/telephony/registry.go or models.go — both are TRD 40-01's files and outside this TRD's declared file_tree/files_modified. The 'default provider' and 'third provider extends the seam' verify requirements are satisfied entirely from within signalwire_test.go and twilio_test.go, using only the already-exported Registry/Provider/ProviderFactory/ProviderSignalWire/ProviderTwilio symbols."
  - "go mod tidy could not run cleanly in this repo — it fails on a pre-existing, unrelated issue (platform/audit's test-only import of go.opentelemetry.io/otel/sdk/internal/internaltest, which the pinned otel/sdk version no longer ships). Verified this is pre-existing, not something this TRD caused, by stashing this TRD's go.mod/go.sum changes and re-running go mod tidy against the unmodified tree — it fails identically. Used go get + go mod tidy -e to correctly promote twilio-go from an indirect, unused entry to a direct pinned require without needing a full tidy pass."

patterns-established:
  - "Provider-adapter file pattern for this package: {provider}.go (adapter + factory), {provider}_test.go (ported unit tests + one seam-integrity proof test), shared plumbing (url_rewrite.go) factored out rather than duplicated between adapters."

requirements-completed: []

# Verification evidence
verification:
  gates_defined: 5
  gates_passed: 5
  auto_fix_cycles: 0
  tdd_evidence: false
  test_pairing: true

# Metrics
duration: 30min
completed: 2026-09-08
---

# Objective 40 TRD 02: Twilio + SignalWire Adapters Summary

**Ported both telephony adapters from a proven production implementation — SignalWire (default, wrapping twilio-go via a host-rewrite shim) and Twilio (second adapter, proving the Provider seam is real) — pinning `github.com/twilio/twilio-go` to the exact v1.30.4 release the source was vetted against, with all signature validation delegated to twilio-go's own `RequestValidator`.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-09-08T00:55:00Z
- **Completed:** 2026-09-08T01:25:28Z
- **Tasks:** 5
- **Files modified:** 7 (5 created, 2 modified: go.mod, go.sum)

## Accomplishments

- `platform/telephony/url_rewrite.go`: `swBaseClient`, a twilio-go `BaseClient` implementation that rewrites `api.twilio.com` to the tenant's SignalWire space host on every outbound request — the only transport-level difference between the two adapters. Also `rewriteHost`/`stripScheme` helpers.
- `platform/telephony/signalwire.go`: `signalwireProvider` — the default Provider adapter. Constructs a twilio-go `RestClient` wired through `swBaseClient`; validates webhook signatures via twilio-go's `RequestValidator`, accepting `X-SignalWire-Signature` with `X-Twilio-Signature` as a compatibility fallback.
- `platform/telephony/twilio.go`: `twilioProvider` — the second Provider adapter, wired directly against Twilio's REST API with no host rewrite. Exists specifically to prove the `Provider` interface is a real abstraction: adding it required zero changes to `Provider`, `NoopProvider`, or `Registry`.
- `platform/telephony/signalwire_test.go` + `twilio_test.go`: both source test files ported intact (unconfigured-config cases, `rewriteHost`/`stripScheme`, webhook signature verify including bad/missing-signature and header-fallback paths, status-webhook parsing for SMS and call, inbound-SMS parsing), plus two new tests this TRD's `<verify>` section required and the source never had (see below).
- `go.mod`/`go.sum`: `github.com/twilio/twilio-go` added as a direct dependency, pinned to the **exact version `v1.30.4`** — matching the version politihub's source was vetted against, confirmed by identical go.sum hashes. `github.com/golang/mock v1.6.0` came along as twilio-go's own indirect dependency.

## Task Evidence

| Task | Verify Command | Exit Code | Status |
|---|---|---|---|
| 1: go.mod/go.sum + url_rewrite.go | `go build ./platform/telephony/...` | 0 | PASS |
| 2: signalwire.go | `go build ./platform/telephony/...` | 0 | PASS |
| 3: signalwire_test.go (ported + default-provider proof) | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |
| 4: twilio.go | `go build ./platform/telephony/...` | 0 | PASS |
| 5: twilio_test.go (ported + third-provider proof) | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS |

## Task Commits

Each task was committed atomically:

1. **Task 1: go.mod/go.sum + url_rewrite.go** - `28f6c73` (chore)
2. **Task 2: signalwire.go** - `2611782` (feat)
3. **Task 3: signalwire_test.go** - `1c0cbf1` (test)
4. **Task 4: twilio.go** - `2b9fb13` (feat)
5. **Task 5: twilio_test.go** - `f6a3bb0` (test)

**Plan metadata:** (this commit, appended after final state updates)

## Validation Gate Results

| Gate | Command | Exit Code | Status |
|---|---|---|---|
| build | `go build ./platform/telephony/...` | 0 | PASS |
| vet | `go vet ./platform/telephony/...` | 0 | PASS |
| test | `go test ./platform/telephony/... -count=1 -race` | 0 | PASS (26 tests) |
| gofmt | `gofmt -l platform/telephony/` | 0 (no output) | PASS |
| repo-wide build | `go build ./...` | 0 | PASS |
| grep (domain leakage) | `grep -riE "politihub\|justinforme\|committee\|voter\|campaign" platform/telephony/` | 1 (no match) | PASS |

## Post-TRD Verification

- **Auto-fix cycles used:** 0
- **Must-haves verified:** 4/4
  1. "SignalWire is the DEFAULT provider" — `TestSignalWire_IsDefaultProviderInRegistry` (new test, see below)
  2. "Both adapters wrap github.com/twilio/twilio-go" — `platform/telephony/signalwire.go`, `platform/telephony/twilio.go` both import `github.com/twilio/twilio-go{,/client,/rest/api/v2010}`
  3. "Signature validation delegates to twilio-go's RequestValidator for BOTH adapters" — both `VerifyWebhookSignature` methods call `p.valid.ValidateBody(...)`, `p.valid` constructed via `twilioClient.NewRequestValidator` in both factories; no HMAC code exists anywhere in this package
  4. "Adding a third provider requires implementing Provider and calling Registry.Register — no edits to registry, resolver or callers" — `TestRegistry_ThirdProviderExtendsWithoutRegistryChanges` (new test, see below)
- **Gate failures:** None

## `<verify>` Section — Exact Evidence

**1. `go test ./platform/telephony/... -count=1 -race` green, including both adapter suites:**

```
=== RUN   TestSignalWire_NewProvider_Unconfigured / RewriteHost / StripScheme / VerifyWebhookSignature / ParseStatusWebhook_SMS_AndCall / ParseInboundSMS / TypeIsSignalwire / IsDefaultProviderInRegistry  — all PASS
=== RUN   TestTwilio_NewProvider_Unconfigured / VerifyWebhookSignature / ParseStatusWebhook_SMS / ParseStatusWebhook_Call / ParseStatusWebhook_FailedMaps / ParseInboundSMS / ParseStatusWebhook_MissingSIDs / TestRegistry_ThirdProviderExtendsWithoutRegistryChanges — all PASS
PASS
ok  	github.com/aocybersystems/eden-platform-go/platform/telephony	1.280s
```
26 tests total (10 from TRD 40-01's seam + 16 new/ported in this TRD), all green under `-race`.

**2. Registry resolves `signalwire` as the default provider type:**

`TestSignalWire_IsDefaultProviderInRegistry` (in `signalwire_test.go`) registers both `NewSignalWireFactory()` and `NewTwilioFactory()` on a fresh `Registry`, asserts `r.Has(ProviderSignalWire)`, resolves `r.For(Config{Provider: ProviderSignalWire, ...})` and asserts the returned `Provider.Type() == ProviderSignalWire`. It also asserts that an unregistered tenant provider type surfaces `ErrUnsupportedProvider` from `Registry.For` rather than a silent `NoopProvider` substitution — matching the must_haves truth that Noop is only reached when no config resolves at all. **This test did not exist in the politihub source; it was written for this TRD's `<verify>` requirement.**

**3. A third, in-test fake Provider registers and resolves without registry/resolver edits:**

`TestRegistry_ThirdProviderExtendsWithoutRegistryChanges` (in `twilio_test.go`) registers `signalwire` and `twilio` via their real factories, then registers an in-test-only `fakeThirdProvider` under a novel `ProviderType("fake-third")` via an inline factory closure. It asserts the two shipped adapters remain registered (`r.Has` for both), resolves the third provider through the same `Registry.For` call path, and calls `SendSMS` on it. No line of `registry.go`, `provider.go`, or any resolver code exists in this TRD or was touched by it. **This test did not exist in the politihub source; it was written for this TRD's `<verify>` requirement.**

## Files Created/Modified
- `platform/telephony/url_rewrite.go` - `swBaseClient` twilio-go `BaseClient` shim + `rewriteHost`/`stripScheme` helpers
- `platform/telephony/signalwire.go` - `signalwireProvider`, `NewSignalWireFactory`, `newSignalWireProvider`
- `platform/telephony/signalwire_test.go` - ported adapter tests + `TestSignalWire_IsDefaultProviderInRegistry`
- `platform/telephony/twilio.go` - `twilioProvider`, `NewTwilioFactory`, `newTwilioProvider`
- `platform/telephony/twilio_test.go` - ported adapter tests + `TestRegistry_ThirdProviderExtendsWithoutRegistryChanges` + `fakeThirdProvider`
- `go.mod` / `go.sum` - `github.com/twilio/twilio-go v1.30.4` (direct, pinned), `github.com/golang/mock v1.6.0` (indirect, twilio-go's own dependency)

## Decisions Made

- **Pinned twilio-go to the exact vetted version, not latest.** Ran `go get github.com/twilio/twilio-go@v1.30.4` explicitly rather than `go get github.com/twilio/twilio-go` (which would have resolved to a newer release — confirmed v1.31.0 exists and was what an un-pinned `go mod tidy` on this repo tried to pull in during a diagnostic run). `url_rewrite.go`'s entire justification comment is a version-specific claim about v1.30.4's lack of `ClientParams.BaseURL`; pinning any other version would silently invalidate that comment's premise. Confirmed the pin is the *same* release the source was vetted against by diffing go.sum hashes — they match politihub's go.sum byte-for-byte for both the `h1:` and `/go.mod` lines.
- **`go mod tidy` does not run cleanly in this repo** — it fails on a pre-existing, unrelated break in `platform/audit`'s test dependency on `go.opentelemetry.io/otel/sdk/internal/internaltest`, a package the currently-resolved otel/sdk version no longer ships. Verified this is pre-existing and not caused by this TRD: stashed this TRD's `go.mod`/`go.sum` changes (leaving the new `.go` files in place) and re-ran `go mod tidy` against the otherwise-unmodified tree — it fails with the identical otel error (plus a transient ambiguous-import error from the still-unpinned new `.go` files pulling an unconstrained twilio-go). Worked around this by using `go get @v1.30.4` (which correctly resolves and pins) followed by `go mod tidy -e` (tolerates the one pre-existing failure while still promoting twilio-go from indirect-unused to direct-used in `go.mod`). This pre-existing otel/audit break is **not part of this TRD's scope** and is not fixed here — flagged for whoever next needs a clean `go mod tidy` in this repo.
- **Renamed `toVolunteer, voterNumber` → `toA, toB`** in both adapters' `InitiateBridge`, matching the neutral vocabulary TRD 40-01 already established in `provider.go`'s interface doc comments. Comment text describing the masked two-leg bridge says "tenant DID" instead of "campaign DID."
- **Removed every external-app/source-repo name from shipped comments** (`politihub`, `justinforme`, `Obj 25`) — same call TRD 40-01's SUMMARY made for its own files, extended here for consistency. Where the source's comment said the 10DLC compliance gate "is NOT carried over" and named the specific app it came from, this TRD keeps the substantive point (compliance/consent gating is application-layer policy, not transport-adapter logic) but drops the app name.
- **Added two tests beyond a literal port** (`TestSignalWire_IsDefaultProviderInRegistry`, `TestRegistry_ThirdProviderExtendsWithoutRegistryChanges`) because the TRD's `<verify>` section requires them and the politihub source has no equivalent — SignalWire being "the default" and a third-provider extensibility proof are both properties of *this* package's design contract, not something the source needed to demonstrate. Placed them in `signalwire_test.go`/`twilio_test.go` respectively (the TRD's declared file_tree lists exactly 5 files, no 6th test file was added) rather than touching `registry_test.go`, which belongs to TRD 40-01 and is outside this TRD's declared `files_modified`.
- **Did not touch `registry.go` or `models.go`.** Both are already-merged TRD 40-01 files, outside this TRD's `file_tree`/`files_modified`, and file ownership rules in the executor prompt say to touch only `{signalwire,signalwire_test,twilio,twilio_test,url_rewrite}.go` + `go.mod`. All new must-have proofs use only symbols those two files already export (`Registry`, `Provider`, `ProviderFactory`, `ProviderSignalWire`, `ProviderTwilio`, `ErrUnsupportedProvider`).

## Deviations from Plan

None that change scope — the two additional test functions above are TRD-required (`<verify>` explicitly calls for both) and are documented as net-new (not ported) rather than silently presented as ports. No Rule 1-4 deviation triggered: no bug found, no missing critical functionality, no blocking issue, no architectural decision needed. The `go mod tidy` workaround above is a deviation from a literal "run go mod tidy" step, applying Rule 3 (auto-fix blocking issue) — worked around with `go get` + `go mod tidy -e` rather than attempting to fix the unrelated, out-of-scope `platform/audit`/otel break.

## Issues Encountered

- `go mod tidy` (no flags) fails in this repo on a pre-existing, unrelated issue: `platform/audit` imports `go.opentelemetry.io/otel/sdk/log`, whose test package imports `go.opentelemetry.io/otel/sdk/internal/internaltest`, a package absent from the currently-resolvable `go.opentelemetry.io/otel/sdk@v1.46.0`. Confirmed pre-existing via a stash-and-retry against the unmodified tree. Not fixed here (out of this TRD's scope — `platform/audit` is untouched by this objective); documented so a future `go mod tidy` run in this repo doesn't get misattributed to TRD 40-02.

## User Setup Required

None — no external service configuration required. Twilio/SignalWire account credentials (`AccountSID`, `AuthToken`, `SpaceURL`, `SendingNumber`) are runtime `Config` values a later TRD's resolver will populate from encrypted per-tenant storage (TRD 40-03); this TRD ships only the adapters that consume that `Config`.

## Next Objective Readiness

- `platform/telephony/{signalwire,twilio}.go` are ready to be wired into a real `Registry` at process start-up (`registry.Register(telephony.ProviderSignalWire, telephony.NewSignalWireFactory())`, same for Twilio) once TRD 40-03's config-driven resolver exists to supply per-tenant `Config` values.
- The webhook-signature verification path (`VerifyWebhookSignature` on both adapters) is ready for TRD 40-04's webhook handler to call directly — no further plumbing needed on the adapter side.
- `go mod tidy`'s pre-existing failure (see Issues Encountered) is unrelated to telephony but will surface again for the next TRD or objective that needs a clean tidy pass in this repo; worth a dedicated fix at some point, not blocking here.

## Self-Check: PASSED

- All 5 created files verified present on disk: `url_rewrite.go`, `signalwire.go`, `signalwire_test.go`, `twilio.go`, `twilio_test.go`.
- All 5 task commit hashes verified present in `git log --oneline --all`: `28f6c73`, `2611782`, `1c0cbf1`, `2b9fb13`, `f6a3bb0`.
- No missing items.
