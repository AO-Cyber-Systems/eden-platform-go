# Migration playbook — moving onto platform/telephony

This package consolidates four independent telephony integrations built up
across the AOC portfolio, each correct for its consumer at the time it was
written. This is documentation only: **no consumer is migrated by this
objective.** Each migration below is its own PR in its own repo, owned by
that consumer's own DevFlow setup, on its own schedule.

## 1. Find your integration

| Repo | Location | Contributes |
|---|---|---|
| politihub | `go/internal/telephony/` | `Provider` interface, SignalWire + Twilio adapters, registry, resolver, encrypted config, TCPA + revocation matcher, phone opt-out store, webhook handler, `url_rewrite.go` — this is the majority source; `platform/telephony` is substantially a lift-and-generalize of this package (`committeeID` → `companyID`, `VoterLookup` → `RecipientLookup`, no other functional change) |
| justinforme | `app/signalwire/` — `client.go` (343 LOC), `mms.go` (181), `compliance.go` (337), `sms_inbound_handler.go` (150), `call_status_handler.go` (194), `webhook_middleware.go` (140) | MMS sending, an inbound-SMS handler and call-status handler reconciled into this package's webhook test coverage (busy/no-answer/canceled/ringing/in-progress transitions), and the 8 KiB webhook body ceiling this package also uses |
| navigators | `internal/navigators/` — `suppression_service.go` (149), `sms_compliance.go` (118) | The opt-back-in (START) path and the `CheckSendAllowed` precondition-gate shape — both reconciled INTO `platform/telephony` (see §4); also `sms_campaign_service.go`, `sms_worker.go`, `sms_template_service.go`, which are **not** reconciled — see §3 |
| eden-biz | `go/internal/telephonycreds/` | Credential service, Postgres store, and the from-number-conflict guard (`uq_telephony_credentials_from_number`) — carried forward as migration 016's `uq_tenant_telephony_config_active_number` partial unique index and `ErrSendingNumberClaimed` |

## 2. Generic migration steps

For every consumer:

1. Add the dependency: `go get github.com/aocybersystems/eden-platform-go@<pinned-version>`.
2. Stand up per-tenant config rows in `tenant_telephony_config` (migration
   016) for every company currently configured against your own
   credential store, encrypting `auth_token`/`webhook_secret` via a
   `FieldEncrypter` built from your platform data key.
3. Register `telephony.NewSignalWireFactory()` and/or
   `telephony.NewTwilioFactory()` on a `telephony.Registry` at process
   start-up, matching whichever backend(s) your tenants are actually
   configured against today.
4. Replace your integration's send call sites with the `Resolver.For` +
   `Provider.SendSMS`/`InitiateCall`/`InitiateBridge` pattern shown in the
   package [`README.md`](./README.md)'s Quick start — **including the
   `TCPAService.CheckSendAllowed` precondition call**, wired against your
   own `PhoneOptOutStore`/`RecipientLookup`/`ConsentRecorder`
   implementations (or the shipped `PostgresPhoneOptOutStore` for the
   phone-keyed half).
5. Point your provider's webhook configuration (Twilio/SignalWire console,
   or however your provisioning does it today) at a `WebhookHandler`
   mounted per the README, wiring `SMSStatus`/`CallStatus`/`Inbound` sinks
   to your existing persistence — the sink *interfaces* are new, but
   nothing stops a sink implementation from writing to the exact same
   tables your current handler already writes to.
6. Run `go test ./...` and `go vet ./...` in your repo.
7. Cut over one tenant, verify a real send + a real inbound webhook
   end-to-end, then cut over the rest.
8. Delete your integration's code once every tenant has moved.
9. Open a PR titled `chore(telephony): migrate to platform/telephony` with
   a link back to Objective 40.

## 3. What this package does NOT cover — and where that logic lives today

**Do not expect these to migrate.** They are application concerns layered
*on top of* the transport this package provides, not something
`platform/telephony` V1 attempts to absorb — folding them in now would blur
the `Provider` seam, which is the actual point of this objective.

| Deferred capability | Current home |
|---|---|
| Campaign orchestration | navigators — `internal/navigators/sms_campaign_service.go` |
| Send-worker queue | navigators — `internal/navigators/sms_worker.go` |
| Message templates | navigators — `internal/navigators/sms_template_service.go` |
| MMS | justinforme — `app/signalwire/mms.go` |

If your consumer needs one of these today, keep it in your own repo, built
on top of `platform/telephony.Provider` underneath it — exactly the pattern
navigators' and justinforme's own code already follows relative to their
current Twilio/SignalWire clients. A capability that proves useful to two or
more consumers is a candidate for a follow-up objective, not something to
smuggle into this migration.

## 4. Per-consumer reconciliation notes

These are the places where this package's behavior is not a pure port of
politihub's source — read them before you migrate, since they are real
functional deltas from what politihub itself does.

### 4.1 navigators — opt-back-in (START) and CheckSendAllowed

politihub's `tcpa.go` has `FuzzyMatchStop`/`FuzzyMatchHelp` only — no
opt-back-in path, and `PhoneOptOutStore` has no removal method. navigators'
`sms_compliance.go` (`ProcessOptOut`, switching on `"STOP"`/`"START"`) and
`suppression_service.go` (`RemoveFromSuppressionList`) both cover a
resubscribe flow politihub does not. `platform/telephony` adds
`FuzzyMatchStart` and `PhoneOptOutStore.Remove` to close that gap — if your
integration currently has no keyword-driven opt-back-in path (as politihub's
did not), you gain one by migrating; if you already have one (as navigators
does), map it onto `FuzzyMatchStart` + `Remove` directly.

navigators' `sms_compliance.go` also has `CheckSendAllowed(ctx, voterID,
companyID)`, composing a suppression check with a **quiet-hours** check.
politihub had no equivalent method at all — callers queried
`PhoneOptOutStore.IsOptedOut` directly. `platform/telephony.TCPAService.CheckSendAllowed`
covers the suppression half only; the quiet-hours check is **deliberately
not ported** (see the README's TCPA section) — if you rely on it, keep it as
a wrapper you compose alongside `CheckSendAllowed` in your own send path.

### 4.2 justinforme — webhook body ceiling, call-status transitions, MMS

`MaxWebhookBodyBytes = 8192` matches the ceiling justinforme's
`webhook_middleware.go` and politihub's own handler independently converged
on — no functional change needed on your side if you were already at 8 KiB.

`platform/telephony`'s call-status test coverage was extended (busy /
no-answer / canceled / ringing / in-progress) specifically reconciling
against justinforme's `call_status_handler.go`, which covered transitions
politihub's ported suite originally did not. `mapStatus` already handled
every one of these correctly; this closed a test gap, not a behavior gap —
your call-status webhook consumer should map onto `StatusEvent.Kind ==
"call"` from a single `StatusHandler` endpoint rather than the
separate-endpoint-per-kind shape `call_status_handler.go` used.

MMS (`mms.go`) has no equivalent in `platform/telephony` at all — see §3.
Keep it in justinforme, built against `Provider.SendSMS`'s sibling call once
(if ever) this package adds MMS support.

### 4.3 eden-biz — from-number conflict semantics

politihub's original config store had **no** from-number-conflict guard: any
tenant could claim a `sending_number` another tenant's active config already
owned, and `LookupBySendingNumber`'s unordered `LIMIT 1` would then resolve
inbound webhook traffic to whichever row Postgres happened to return —
non-deterministically routing a status callback or inbound SMS to the wrong
tenant's secret. `platform/telephony` carries eden-biz's
`uq_telephony_credentials_from_number` invariant forward as migration 016's
`uq_tenant_telephony_config_active_number` partial unique index
(`(provider, sending_number) WHERE is_active = true`), with
`PostgresConfigStore.Upsert` mapping the resulting Postgres `23505` conflict
to `ErrSendingNumberClaimed`. If eden-biz migrates, this is a strictly
stronger guarantee than `telephonycreds` had reason to build against a
single-row-per-company model — no code change needed on your side beyond
handling the new sentinel error on `Upsert`.

### 4.4 politihub — the source, with two known limitations NOT carried forward as-is

- **Phone normalization moved fully into Go.** politihub's
  `IsOptedOut` compared against `politihub_normalize_phone`, a Postgres
  function shipped in a separate migration from the opt-out table itself —
  in production, `Upsert` stored a provider's `From` verbatim while at least
  one caller queried with a differently-formatted string, and the mismatch
  silently let an already-opted-out number through. `platform/telephony`
  normalizes once, in Go (`normalizePhone` in `optout_store.go`), used
  identically on both the write and read path — there is no second
  definition to drift out of sync with. No action needed on your side; this
  is strictly safer than politihub's own behavior.
- **Webhook resolve-then-verify ordering is intentional, not a bug carried
  from politihub.** See the README's "webhook order-of-operations" section
  for the full reasoning — per-tenant HMAC cannot verify before the tenant
  is known, so resolution necessarily precedes verification. If your
  integration currently assumes a global (not per-tenant) webhook secret,
  read that section before wiring your webhook config.

## 5. Verification checklist

After your migration PR:

- [ ] `go vet ./...` clean
- [ ] `go test ./...` green
- [ ] A real inbound webhook (status + inbound SMS) verified against a live
      tenant, not just an httptest fixture
- [ ] `TCPAService.CheckSendAllowed` is on the hot path of every outbound
      send call site, not just called somewhere in the codebase
- [ ] Old integration code deleted (`git diff --stat` shows the removed
      files) once every tenant has cut over
- [ ] If you relied on navigators' quiet-hours check or any of the four
      deferred capabilities in §3, you have kept that logic in your own repo
      layered on top of `Provider` — it was not silently dropped

## 6. Out of scope for migrations

Don't expand the surface during migration. The four deferred capabilities in
§3 stay where they are. New capabilities that prove useful to two or more
consumers are candidates for a follow-up objective on `platform/telephony`
itself, not something to add inline while migrating.
