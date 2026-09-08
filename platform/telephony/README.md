# platform/telephony

Provider-neutral abstraction over telephony backends: outbound SMS, outbound
calls, masked two-leg call bridging, and inbound webhook handling (status
callbacks + inbound SMS). Four Eden products (politihub, justinforme,
navigators, eden-biz) each maintain their own vendor integration against
Twilio/SignalWire today; this package is the union of that work, extracted
and generalized so new consumers wire one interface instead of forking a
fifth client.

## Status

Beta. Ported and generalized from `politihub/go/internal/telephony`
(the most complete of the four source implementations), reconciled against
justinforme, navigators, and eden-biz where they covered a case politihub
did not (see [`MIGRATION.md`](./MIGRATION.md) for the per-consumer detail).
13 implementation files / ~2,220 LOC, 12 test files / ~3,485 LOC, 93 top-level
test functions (81 `--- PASS` lines counting subtests). The source it was
lifted from was 1,943 impl + 2,399 test LOC — test coverage grew during the
port, it did not shrink.

## Quick start

```go
import "github.com/aocybersystems/eden-platform-go/platform/telephony"

// 1. Register the backends you support. Adding a third backend later
//    requires no changes to Provider, Registry, or Resolver — see
//    "The Provider seam" below.
registry := telephony.NewRegistry()
registry.Register(telephony.ProviderSignalWire, telephony.NewSignalWireFactory())
registry.Register(telephony.ProviderTwilio, telephony.NewTwilioFactory())

// 2. Per-tenant config, encrypted at rest.
enc, err := telephony.NewFieldEncrypter(dataKey) // 32 bytes; NopEncrypter in dev
configStore := telephony.NewPostgresConfigStore(pool, enc)

// 3. Resolver: company ID -> live Provider, with an optional platform-wide
//    default tier and an optional entitlement gate on SignalWire.
resolver := telephony.NewResolver(configStore, registry, nil /* fallback */, nil /* entitlements */)

// 4. TCPA precondition gate — every send call site MUST consult this
//    BEFORE calling a Provider. See "TCPA / opt-out" below.
tcpa := telephony.NewTCPAService(recipientLookup, consentRecorder, phoneOptOutStore)

func sendSMS(ctx context.Context, companyID uuid.UUID, to, body string) error {
    if err := tcpa.CheckSendAllowed(ctx, companyID, to); err != nil {
        return err // ErrRecipientOptedOut — refuse before the Provider is ever touched
    }
    provider, err := resolver.For(ctx, companyID)
    if err != nil {
        return err
    }
    _, err = provider.SendSMS(ctx, to, body, "" /* use tenant's SendingNumber */)
    return err
}

// 5. Inbound webhooks.
handler := &telephony.WebhookHandler{
    Store:      configStore,
    Registry:   registry,
    SMSStatus:  yourSMSStatusSink,
    CallStatus: yourCallStatusSink,
    Inbound:    yourInboundSink, // typically wraps tcpa.HandleInboundBody — see below
    Audit:      yourAuditWriter, // optional; best-effort, never blocks the response
}
mux.HandleFunc("/webhooks/telephony/signalwire/status", handler.StatusHandler(telephony.ProviderSignalWire))
mux.HandleFunc("/webhooks/telephony/signalwire/sms_inbound", handler.InboundSMSHandler(telephony.ProviderSignalWire))
```

## The Provider seam

```go
type Provider interface {
    Type() ProviderType
    SendSMS(ctx context.Context, to, body, fromOverride string) (SMSResult, error)
    InitiateCall(ctx context.Context, to, fromOverride, statusCallbackURL string) (CallResult, error)
    InitiateBridge(ctx context.Context, toA, toB, fromOverride, statusCallbackURL string) (CallResult, error)
    VerifyWebhookSignature(r *http.Request, bodyBytes []byte) error
    ParseStatusWebhook(r *http.Request, bodyBytes []byte) (StatusEvent, error)
    ParseInboundSMS(r *http.Request, bodyBytes []byte) (InboundSMS, error)
}
```

A `Registry` maps `ProviderType -> ProviderFactory` (`func(Config) (Provider,
error)`); a `Resolver` turns a company ID into a live `Provider` by looking up
that company's `Config` and calling `Registry.For`. Everything above the
`Provider` interface — the resolver's tiering, the webhook handler, TCPA — is
written against `Provider` and `Config` only, never against a concrete
backend.

**`NoopProvider`** is what a misconfigured or unresolved tenant gets: every
outbound method returns `ErrProviderNotConfigured`, every webhook verify
returns `ErrInvalidSignature`. It exists so a missing configuration fails
loud — a silently-dropped SMS is a worse failure mode than a visible error.

**Adding a third backend** costs exactly two things: an implementation of
`Provider`, and one `registry.Register(yourType, yourFactory)` call. Nothing
in `provider.go`, `registry.go`, or `resolver.go` changes —
`TestRegistry_ThirdProviderExtendsWithoutRegistryChanges` (in
`twilio_test.go`) proves this directly by registering an in-test-only third
provider alongside the two shipped adapters and driving a send through it.

### SignalWire is the standard, and why it wraps twilio-go

SignalWire ships a Twilio-compatible REST API — same request shapes, same
HMAC-SHA1-over-(URL + sorted form fields) webhook signature scheme, just a
different host and signature header. `signalwireProvider` and
`twilioProvider` both wrap `github.com/twilio/twilio-go` (pinned at exactly
`v1.30.4` — the version politihub's source was vetted against, confirmed by
byte-identical `go.sum` hashes) rather than each hand-rolling REST calls.
The only place they genuinely differ in transport terms:

- **Host.** `twilio-go` v1.30.4 hardcodes `https://api.twilio.com` inside
  every generated API service, with no `ClientParams.BaseURL` to override
  it. `url_rewrite.go`'s `swBaseClient` wraps twilio-go's `BaseClient`
  interface and rewrites the host to the tenant's configured
  `SpaceURL` on every outbound request — the one injection point that
  version of the library exposes.
- **Signature header.** `signalwireProvider.VerifyWebhookSignature` checks
  `X-SignalWire-Signature` (falling back to `X-Twilio-Signature` for
  compatibility tooling); `twilioProvider` checks `X-Twilio-Signature`
  only. Both delegate the actual HMAC comparison to twilio-go's
  `RequestValidator` — neither adapter reimplements it.

One client library serving two adapters is what makes "provider is
swappable" a real, cheap property of this package rather than an aspiration:
standardizing on SignalWire (`ProviderSignalWire`) costs nothing extra, and
keeping Twilio (`ProviderTwilio`) available costs almost nothing.

## Per-tenant configuration and credential encryption

`ConfigStore` persists one active `Config` row per company
(`tenant_telephony_config`, migration 016) plus `telephony_phone_optouts`
(migration 017) for the TCPA store below. Both migrations were applied,
rolled back, and reapplied against a live local Postgres during their
respective TRDs (40-03 and 40-05) — `migrate ... up`, `down 1`, `up`, each
reported PASS in those TRDs' SUMMARY.md files. This has not been re-run as
part of this TRD (07); it is carried forward from those records, not
re-verified here.

`Config.AuthToken` and `Config.WebhookSecret` are encrypted at rest via the
`Encrypter` seam:

- **`NopEncrypter`** — plaintext passthrough. Default for local dev and for
  tests that don't need to exercise the encryption seam itself.
- **`FieldEncrypter`** — the production adapter over
  `platform/encryption.FieldEncryptor` (AES-256-GCM, nonce embedded in the
  ciphertext so two encryptions of the same plaintext never collide). This
  package vendors no crypto of its own; it is a thin wrapper that reuses the
  same 32-byte key for both of `platform/encryption.New`'s key arguments,
  since telephony credentials are write-only and never blind-indexed.

**`LookupBySendingNumber(provider, number)`** is the inbound-webhook
tenant-resolution chokepoint: it must return **at most one** active tenant
per `(provider, sending_number)` pair, because a status callback or inbound
SMS carries only the destination number in its body, never a tenant
identifier. Migration 016's partial unique index
(`uq_tenant_telephony_config_active_number`, `WHERE is_active = true`)
enforces this at the database level; `PostgresConfigStore.Upsert` maps the
resulting Postgres `23505` conflict to `ErrSendingNumberClaimed` rather than
letting two tenants silently race for the same number.

## The webhook order-of-operations security property

Every webhook handler in this package runs `VerifyAndResolve` before a
payload is parsed or acted on in any way. Its actual order, as implemented:

```
capture raw body
  -> parse form to read "To"
  -> resolve tenant (ConfigStore.LookupBySendingNumber(provider, To))
  -> get that tenant's own secret (Config.SignatureToken())
  -> restore the exact raw bytes captured at step 1
  -> verify the signature against the resolved tenant's secret
  -> only on success, return that tenant's Provider + Config
```

**Resolution happens before verification, of necessity — not by choice.**
Every tenant's `Config` carries its *own* signing secret. There is no global
secret to check a signature against before a tenant has been identified, so
the secret to verify with cannot be known until `LookupBySendingNumber` has
run. A verify-then-resolve order is not merely a different implementation
choice here; it is not something HMAC-per-tenant can do at all.

What actually stops a forged request is the **verification step**, not the
resolution step: resolving by `To` only picks *which* tenant's secret to
check against. An attacker who addresses a forged request at a victim
tenant's number, or plants a spurious tenant/company field elsewhere in the
body, still fails at the signature check — they do not hold that tenant's
secret. `To` is a routing key; the signature is the actual authority. Either
failure (unknown number, bad signature) returns before `ParseStatusWebhook`
/ `ParseInboundSMS` ever runs, so an unverified payload is never parsed or
acted on. `TestVerifyAndResolve_ResolvesByReceivingNumberNotBodyField` and
the tampered-body tests in `webhook_test.go` (and this package's own
`integration_test.go`) exercise both halves of that claim directly.

**Residual consideration, not a defect.** `To` is read *before*
verification succeeds, because resolution has to happen first — so an
unsigned or freshly-forged request can still drive a `ConfigStore` lookup
(a `LookupBySendingNumber` call) even though it can never drive a `Provider`
call or reach a sink. This is inherent to any per-tenant-secret HMAC scheme,
not something this package does wrong, but it is a real enumeration/DB-load
surface: an attacker can cheaply probe which numbers resolve to an active
tenant, and can generate one `ConfigStore` read per request without ever
holding a valid signature. Rate-limit the webhook endpoints at the edge
(reverse proxy / API gateway) rather than trying to solve this inside the
package — that is where a generic abuse-rate control belongs, and it
protects every tenant's endpoint uniformly.

`ReadWebhookBody` (which every handler calls first) additionally bounds the
body at `MaxWebhookBodyBytes` (8 KiB, matching the limit politihub and
justinforme's webhook middleware independently converged on) and restores
`r.Body` to the exact bytes it read — any upstream middleware that reads the
body first (logging, tracing, a generic body-dumper) silently breaks every
signature check downstream, so this package assumes it runs first in the
handler chain.

## TCPA / opt-out — what this package does, and what the consumer still owns

**What ships here:**

- `FuzzyMatchStop` / `FuzzyMatchStart` / `FuzzyMatchHelp` — the exact-keyword
  and fuzzy-phrase matchers for the FCC's per se reasonable STOP-reply
  keywords (47 CFR § 64.1200(a)(10)) plus a CTIA-conventional START
  (opt-back-in) path.
- `TCPAService.HandleInboundBody` — runs those matchers against a verified
  inbound SMS and persists the result: STOP upserts a phone-keyed opt-out
  (always) and a consent-ledger entry (when the phone is linked to a
  recipient); START clears the phone-keyed opt-out.
- `TCPAService.CheckSendAllowed` — the precondition gate, **fails closed**
  (a store error is treated as "not allowed," never as implicit consent).

**What the consumer still owns:**

- **Calling `CheckSendAllowed` at every send call site.** This package
  cannot force it — see the `sendSMS` example above. A caller that queues a
  send and filters opted-out numbers afterward has already leaked the
  message into a queue this package does not control; the check has to run
  *before* `Provider.SendSMS`, not after.
- **Wiring `InboundSMSSink` to `TCPAService.HandleInboundBody`.** `webhook.go`
  defines `InboundSMSSink` as a small package-local interface precisely so a
  consumer can compose it with a real `TCPAService` without this package
  needing a hard dependency on TCPA — see `tcpaInboundSink` in
  `integration_test.go` for the intended shape.
- **HELP auto-replies.** `FuzzyMatchHelp` only detects the keyword; sending
  an actual HELP response (with the consumer's own support contact info) is
  the consumer's job.
- **Quiet-hours / time-of-day send restrictions.** Deliberately not ported
  from navigators' `SMSComplianceService.CheckSendAllowed`, which composes a
  suppression check with a quiet-hours check — quiet hours are a scheduling
  policy, not a TCPA opt-out concern, and don't belong in a transport-layer
  package.
- **10DLC / campaign registration compliance.** Application policy layered
  on top of a `Provider`, not a transport-adapter concern.

## Testing

```sh
go test ./platform/telephony/...
go test ./platform/telephony/... -race
```

Every unit exercised in isolation (matchers, resolver tiering, registry
extensibility, config store against a real Postgres when `DATABASE_URL` is
set) plus `integration_test.go`, which drives the full consumer-facing
lifecycle — send, status webhook, STOP opt-out, refused resend — against a
fake `Provider`, and a second test proving a tampered status webhook never
reaches a sink.

## Migration

Four products currently maintain their own vendor integration against this
package's problem space. See [`MIGRATION.md`](./MIGRATION.md) for where each
fork lives, what maps to what, and what four pieces of functionality
(campaign orchestration, the send-worker queue, templates, MMS) are
deliberately out of scope for this package and remain owned by their current
homes.
