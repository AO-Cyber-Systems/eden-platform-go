# platform/integration — Eden Family launch-surface tests

This package is the M9-milestone verification artifact for objective 33 in
the portfolio standardization plan. It composes every platform package the
Eden Family launch surface depends on and proves — with `go test` — that
the contract between them holds end-to-end.

## Why a separate package

The household, consent, auth, feature-flags, billing-rail, and livekit
packages each ship their own unit tests. This package is for the
**cross-package** wiring that no single package owns:

- A handler that issues a household-aware JWT and then calls a consent
  service. Neither `platform/auth` nor `platform/consent` should know about
  the other.
- A billing webhook that lands in `EdenBizSink` while the household it
  affects is stored in `platform/household` and gated by
  `platform/feature-flags`. The composition is the Eden Family caller's
  problem; this package proves the composition shape.

## What it covers

| Test | Scenario |
|---|---|
| `TestEdenFamily_SignupFlow` | primary contact → household → parents → child → parent-of-record |
| `TestEdenFamily_ChildAccountWithConsent` | COPPA-style consent grant for child principal across multiple purposes |
| `TestEdenFamily_ParentChildJWTSession` | household-aware JWT (parent + child mode) and middleware enforcement |
| `TestEdenFamily_FeatureFlagGate` | tier-aware flags with household / subject overrides and rollouts |
| `TestEdenFamily_BillingRailSubscription` | rail webhook → dispatcher → `EdenBizSink.RecordSubscriptionEvent` |
| `TestEdenFamily_BillingRailChargeAndRefund` | charge + refund + cancel webhooks; bad-signature reject |
| `TestEdenFamily_ConsentRevocationGatesAI` | revoke → `IsValid` flips → AI tutor endpoint denies |
| `TestEdenFamily_OneToOneVideoCall` | InitiateCall → AcceptCall → MarkConnected → EndCall via livekit |
| `TestEdenFamily_NegativeChildCannotActAsParent` | `RequireParentMode` returns `ErrNotParentMode` on a child token |
| `TestEdenFamily_NegativeChildAccountRequiresEligibleParent` | adult-non-parent rejected as POR; child without birthdate rejected |
| `TestEdenFamily_AuditTrailEndToEnd` | every mutation emits an audit event with correct tenant scope |
| `TestEdenFamily_FullJourney` | signup → consent → JWT → flag → billing webhook → call, end-to-end |

## How the harness is wired

See `harness_test.go`. The `edenFamilyHarness` struct holds every service
constructed on in-memory backends:

```
JWT          *auth.JWTManager       (ephemeral ML-DSA-65 key)
Household    *household.Service     (memHouseholdStore + auditLogger)
Consent      *consent.Service       (memConsentStore + auditLogger)
FlagsClient  *featureflags.Client   (MemorySource)
Rail         *billingrail.MockRail  (stripe name)
Sink         *billingrail.MockSink
Dispatcher   *billingrail.Dispatcher
LiveKit      *livekit.Service       (InMemoryStore + roomsStub + stubTokens + ChannelSignaler)
Audit        *recordingAuditStore   (captures every audit.Event for assertions)
```

The harness installs `t.Cleanup` to stop the audit logger. `DrainAudit()`
flushes the async pipeline (250 ms wait covers two 100 ms ticker windows)
and returns the events recorded so far.

## What this does NOT cover

- Postgres-backed integration — `platform/pgstore` tests in each package
  cover persistence-layer correctness.
- Real Apple / Google / Stripe SDK calls — `billingrail.MockRail` is
  programmable for every shape the dispatcher inspects.
- Real LiveKit server — `roomsStub` + `stubTokens` produce deterministic
  output; signaling captured via the package's `ChannelSignaler`.
- HTTP / Connect-level middleware wiring — `auth.RequireParentMode` /
  `RequireChildMode` are exercised via `auth.WithClaims(ctx, claims)` so
  the test mirrors what a real handler does after parsing the JWT.

## How to extend

Add a new scenario as a top-level `TestEdenFamily_*` function. Use
`signupFamily(t, h)` for the canonical signup and
`grantStandardConsents(t, h, res)` for the canonical consent grant set if
your scenario starts from a configured household.

## Stability contract

These tests run as part of `go test ./...` and gate any PR that changes
cross-package wiring. They run in <2 s with no external dependencies.

## References

- TRD: `.planning/objectives/33-aofamily-eden-family-integration/33-01-TRD.md`
- Architecture: `docs/eden-family-integration.md`
- Launch checklist: `docs/eden-family-launch-checklist.md`
- Plan: `PORTFOLIO_STANDARDIZATION_PLAN.md` §11 milestone M9
