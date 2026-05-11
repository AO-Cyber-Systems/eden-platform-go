# Eden Family Launch-Readiness Checklist (M9)

**Milestone:** M9 — objective 33 (Eden Family Platform Integration)
**Status:** Platform infrastructure complete; product / ops work tracked
below.
**Verification:** `platform/integration/eden_family_test.go`

This checklist closes the M9 milestone in
`PORTFOLIO_STANDARDIZATION_PLAN.md` §11. It lists what the shared platform
stack already provides, what the Eden Family product team must still build
on top, and the production-gate operations actions the user runs.

## Section A — Platform infrastructure (READY)

Everything below is merged on `eden-platform-go` `origin/main` and
verified by the integration test suite.

- [x] **Family signup flow** (households + parents-of-record + member
  capabilities + roles) — `platform/household` (objective 24,
  `eden-platform-go#8`)
- [x] **Verifiable parental consent ledger** (append-only, COPPA /
  GDPR-K, audit-fan-out on read) — `platform/consent` (objective 25,
  `eden-platform-go#11`)
- [x] **Identity issuer with household-aware claims** — `cmd/aoid` +
  `platform/auth.CreateHouseholdAccessToken` (objectives 29 / 30 / 24a /
  31, PRs `eden-platform-go#13`, `#14`, `#16`, `#17`)
- [x] **Parental-control middleware** (`RequireHousehold`,
  `RequireParentMode`, `RequireChildMode`) — `platform/auth` (objective
  24a)
- [x] **Tier-aware feature gating runtime** (boolean + variant flags,
  overrides on subject / household / tenant / environment, percent
  rollouts) — `platform/feature-flags` (objective 27,
  `eden-platform-go#7`)
- [x] **Billing-rail adapter contract** (Rail interface, EdenBizSink
  interface, Dispatcher wiring, MockRail + MockSink for testing) —
  `platform/billing-rail` (objective 27, `eden-platform-go#7`)
- [x] **1:1 video / voice calls + multi-party meetings** (call lifecycle
  state machine, room mapping, webhook intake, recording, signaling) —
  `platform/livekit` (objective 28, `eden-platform-go#5`)
- [x] **Audit + observability fan-out** (async batched audit logger;
  Sentry / slog wrappers) — `platform/audit`, `platform/observability`
  (objective 16, `eden-platform-go#2`)
- [x] **Append-only audit trail across household / consent / billing** —
  verified by `TestEdenFamily_AuditTrailEndToEnd`
- [x] **AOFamily product backends migrated to platform/auth + shared
  Dio** — objective 32 (aofamily-{ai,browser,connect}#1, Phase A Go + B
  Flutter both merged 2026-05-11)
- [x] **AOFamily aigateway swap (single AI client across AOFamily-AI /
  Browser / Connect)** — objective 32a (aofamily-{ai,browser,connect}#2 /
  #3 merged 2026-05-11)
- [x] **End-to-end integration test** proving the contract —
  `platform/integration/eden_family_test.go` (this objective, 33-01)

## Section B — Product work Eden Family must build (NOT shared platform)

These are deliberately product-side concerns; the platform is shaped to
support them but does not provide them. Rationale included for each.

- [ ] **Family OS UI / mobile shells** (iOS, Android, Web)
  - *Rationale:* product surfaces, not infrastructure. The platform's
    role ends at the JWT + service-call boundary.
- [ ] **Family onboarding screens** (signup wizard, child-add flow,
  consent capture UI, parent-of-record selection screens)
  - *Rationale:* product UX. The contracts are in place
    (`household.CreateHousehold`, `household.AddMember`, `consent.Grant`)
    but the screens, copy, and flow design are Eden Family's product
    differentiation.
- [ ] **Family-specific business logic** (parent dashboards, screen-time
  rules, content filters, allowance / chores, location-sharing UX, etc.)
  - *Rationale:* this is the Eden Family product. The platform provides
    the *plumbing* (household, consent, audit, calls); the *features* are
    Eden Family's.
- [ ] **Concrete billing-rail adapters** (Apple StoreKit, Google Play
  Billing, Stripe Checkout integrations)
  - *Rationale:* the `Rail` interface (objective 27) is shipped;
    concrete adapters are launch-deliverables tracked as a separate
    workstream. See `PORTFOLIO_STANDARDIZATION_PLAN.md` §13
    production-gate streams ("ws-platform-billing-flags — Stripe / Apple
    IAP / Google Play credentials" — user action).
- [ ] **Marketing site + onboarding marketing copy**
  - *Rationale:* product launch deliverables, not platform.
- [ ] **Mobile platform abstractions** (push notifications, deep links,
  signing certificates, app store releases)
  - *Rationale:* deferred per `PORTFOLIO_STANDARDIZATION_PLAN.md` §13;
    Apple App Store / Google Play Store account setup is a separate user
    workstream.
- [ ] **Eden Family backend HTTP / Connect surface**
  - *Rationale:* product-specific routes and DTOs. The platform packages
    provide the building blocks; assembling them into Eden Family's API
    is product work. The integration test suite is the canonical example
    of how to compose them.

## Section C — Operations checklist (PRE-LAUNCH gates the user runs)

Each item is a user action — Claude does not have authority to perform
these (per the objective 33 brief: "DO NOT touch DNS / customer data /
external creds").

- [ ] Provision `JWT_SECRET` (32-byte ML-DSA-65 key seed file) on Eden
  Family production cluster. *Reference:* `platform/auth/jwt.go`
  `loadKeySeed`; same pattern as AOFamily-AI in Obj 32.
- [ ] Provision Apple App Store Connect production credentials.
  *Reference:* `PORTFOLIO_STANDARDIZATION_PLAN.md` §13 — user action.
- [ ] Provision Google Play Console credentials. *Reference:*
  `PORTFOLIO_STANDARDIZATION_PLAN.md` §13 — user action.
- [ ] Provision Stripe live-mode API keys. *Reference:*
  `PORTFOLIO_STANDARDIZATION_PLAN.md` §13 — user action.
- [ ] Run `platform/pgstore` migrations on Eden Family production DB:
  - `households`
  - `household_members`
  - `parent_of_record`
  - `consent_ledger` (with append-only row triggers)
  - `audit_logs` (already exists in foundational schema)
- [ ] Configure feature-flag source (Eden-Biz tier projection or
  `MemorySource` at boot). *Reference:* `platform/feature-flags/source.go`
  — `MemorySource` + `EnvSource` ship; remote source is a custom
  consumer wrapper.
- [ ] Configure LiveKit URL + API keys for the Eden Family environment.
  *Reference:* `platform/livekit/README.md` for the adapter shape.
- [ ] Wire `platform/billing-rail.EdenBizSink` to Eden-Biz's customer +
  subscription tables. *Reference:* `platform/billing-rail/sink.go`.
- [ ] Run the integration test suite against the deployed environment
  to catch wiring regressions. *Reference:*
  `platform/integration/eden_family_test.go` (in-memory backends — run
  locally against deployed binaries by swapping in real services as a
  smoke-test extension).
- [ ] Smoke-test end-to-end signup → consent → subscribe → call on
  staging.
- [ ] Confirm audit + observability pipelines deliver events to the
  collectors (`platform/audit` → Postgres `audit_logs` ; Sentry /
  metrics endpoints).

## Section D — Reference

### Verification artifacts

- Integration test suite: [`platform/integration/`](../platform/integration/)
- Architecture diagram: [`docs/eden-family-integration.md`](./eden-family-integration.md)

### Planning artifacts

- TRDs: [`.planning/objectives/33-aofamily-eden-family-integration/`](../.planning/objectives/33-aofamily-eden-family-integration/)
- Objective REQUIREMENTS: [`REQUIREMENTS.md`](../.planning/objectives/33-aofamily-eden-family-integration/REQUIREMENTS.md)
- Roadmap entry: `.planning/ROADMAP.md` §"Objective 33"
- Workstream tracker: `.planning/workstreams/STATUS.md` Wave 6

### Plan reference

- Portfolio standardization plan: `PORTFOLIO_STANDARDIZATION_PLAN.md`
  §11 milestone M9
- Production-gate workstreams (user actions): `PORTFOLIO_STANDARDIZATION_PLAN.md`
  §13

### Per-package READMEs

- [`platform/household/README.md`](../platform/household/README.md)
- [`platform/consent/README.md`](../platform/consent/README.md)
- [`platform/auth/README.md`](../platform/auth/README.md)
- [`platform/feature-flags/README.md`](../platform/feature-flags/README.md)
- [`platform/billing-rail/README.md`](../platform/billing-rail/README.md)
- [`platform/livekit/README.md`](../platform/livekit/README.md)
- [`platform/audit/README.md`](../platform/audit/README.md)

## M9 exit statement

M9 is complete when every "READY" item above is on `origin/main` (✅) and
the integration test suite passes (✅). Section B and C items are
*product / ops* work that the platform unblocks but does not own. Eden
Family can launch on the shared infra as soon as those product / ops items
complete.
