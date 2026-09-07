---
objective: 40
name: platform/telephony
date: 2026-09-07
status: complete
---

# Objective 40 — Research: `platform/telephony`

## Question

Four Eden products send SMS/voice through three different vendors with four separate
implementations. What is the cheapest correct path to one platform package where
**SignalWire is the standard provider and the provider is swappable**?

## Headline finding — this is a lift, not a build

`politihub/go/internal/telephony` **already implements the exact architecture requested**:
a `Provider` interface with SignalWire *and* Twilio adapters behind a registry and a
per-tenant resolver. 21 files, **1,943 impl LOC + 2,399 test LOC**.

The objective is therefore *extract + generalize + port tests*, not green-field design.
Rewriting would discard 2,399 lines of proven test coverage.

### Why SignalWire-as-standard is cheap here

politihub's SignalWire adapter deliberately wraps `github.com/twilio/twilio-go`.
SignalWire's REST API is Twilio-compatible — only the host and signature header differ,
and both use the same HMAC-SHA1-over-reconstructed-URL scheme via twilio-go's
`RequestValidator`. One client library serves both adapters. Standardizing on SignalWire
costs nothing extra and keeping Twilio costs almost nothing, which is what makes the
"swappable" requirement real rather than aspirational.

## Source inventory

| Source | Contributes | LOC |
|---|---|---|
| `politihub/go/internal/telephony` | Provider iface, SignalWire + Twilio adapters, registry, resolver, encrypted config, TCPA + revocation, opt-out store, webhook handler, url_rewrite | 1,943 + 2,399 test |
| `justinforme/app/signalwire` | MMS (181), compliance (337), inbound-SMS handler (150), call-status handler (194), webhook middleware (140), client (343) | ~1,345 |
| `navigators/.../internal/navigators` | suppression_service (149), sms_compliance (118) | ~267 |
| `eden-biz/go/internal/telephonycreds` | credential service + PG store + from-number conflict handling | — |

## Existing interface (politihub `provider.go`) — carries over as-is

```go
type Provider interface {
    Type() ProviderType
    SendSMS(ctx, to, body, fromOverride string) (SMSResult, error)
    InitiateCall(ctx, to, fromOverride, statusCallbackURL string) (CallResult, error)
    InitiateBridge(ctx, toVolunteer, other, fromOverride, statusCallbackURL string) (CallResult, error)
    VerifyWebhookSignature(r *http.Request, bodyBytes []byte) error
    ParseStatusWebhook(r *http.Request, bodyBytes []byte) (StatusEvent, error)
    ParseInboundSMS(r *http.Request, bodyBytes []byte) (InboundSMS, error)
}
```

`NoopProvider` returns `ErrProviderNotConfigured` / `ErrInvalidSignature` — fails loud
rather than silently dropping messages. Keep that behaviour.

Seams: `Registry` (`ProviderFactory func(Config) (Provider, error)`), `Resolver`
(per-tenant, TTL-cached, with `PlatformDefaultStore` + `EntitlementChecker`),
`ConfigStore` (Postgres, `LookupBySendingNumber` for inbound routing).

## Coupling — verified, and low

Non-stdlib imports are only: `github.com/google/uuid`, `github.com/jackc/pgx/v5(+pgxpool)`,
`github.com/twilio/twilio-go`. eden-platform-go **already depends on uuid and pgx/v5**;
`twilio-go` is the one new dependency.

There is exactly **one** politihub-internal import to sever: `internal/text_bank`.

## The three real generalizations

This is the actual intellectual work; everything else is mechanical.

1. **`committeeID uuid.UUID` → `companyID uuid.UUID`.** Verified `platform/company.Company`
   already keys on `uuid.UUID`, so this is a **rename, not a type migration** — the single
   biggest cost saving in this objective.
2. **`VoterLookup` (in `tcpa.go`) is a politihub domain concept.** Must become a neutral
   recipient/consent lookup interface the consumer satisfies. `ConsentRecorder` and
   `PhoneOptOutStore` are already neutral and carry over unchanged.
3. **`field_encrypter.go` wraps a local `encryption.FieldEncryptor`.** Verified
   `platform/encryption.FieldEncryptor` **exists** in eden-platform-go, so this collapses
   to a thin adapter over the platform package rather than vendored crypto.

`Resolver.EntitlementChecker` is already a locally-defined interface — it can optionally be
satisfied by `platform/entitlements` without the package taking a hard dependency.

## Scope decision

**V1:** provider interface, SignalWire (default) + Twilio adapters, registry, resolver,
per-tenant encrypted config, TCPA/opt-out/suppression, webhook verification and parsing.

**Deferred:** campaign orchestration, send-worker queue, message templates, MMS. These are
application concerns layered *on* the transport (navigators' `sms_campaign_service` /
`sms_worker`, justinforme's `mms.go`). Folding them in now would blur the provider seam,
which is the whole point of the objective.

## Pitfalls

- **Do not rewrite.** Port the 2,399 test LOC; they encode webhook-signature and TCPA edge
  cases that are expensive to rediscover.
- **Raw body is load-bearing.** `VerifyWebhookSignature` requires the unparsed body; any
  middleware that consumes the body first breaks signature validation silently.
- **`LookupBySendingNumber`** is how inbound webhooks map back to a tenant — it must survive
  the rename or inbound routing breaks.
- **Fail loud.** Preserve `NoopProvider`; a misconfigured tenant must error, not no-op.
- **twilio-go is a new dependency** on a security-sensitive path (signature validation).
  Pin it and note it in the PR.
