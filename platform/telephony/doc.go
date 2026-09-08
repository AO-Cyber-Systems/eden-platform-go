// Package telephony provides a provider-neutral abstraction over telephony
// backends: outbound SMS, outbound calls, masked two-leg call bridging, and
// inbound webhook parsing (status callbacks + inbound SMS).
//
// Tenants (identified by company ID) pick a concrete backend — Twilio or
// SignalWire — and the same calling code drives either one through the
// Provider seam. A Registry maps ProviderType to a ProviderFactory so
// adapters register themselves once, typically at process start-up, and
// callers resolve a per-tenant Provider from a Config.
//
// # Package layout
//
//   - models.go — the value types that cross the Provider boundary
//     (SMSResult, CallResult, StatusEvent, InboundSMS), the resolved
//     per-tenant Config, and the package's sentinel errors.
//   - provider.go — the Provider interface every backend implements, plus
//     NoopProvider, a fail-loud fallback for a misconfigured or unresolved
//     tenant.
//   - registry.go — Registry, the ProviderType -> ProviderFactory map.
//   - twilio.go, signalwire.go, url_rewrite.go — the two concrete Provider
//     backends. Both wrap github.com/twilio/twilio-go (pinned v1.30.4);
//     SignalWire is Twilio-API-compatible, so its adapter differs from the
//     Twilio one only in request host (rewritten per-tenant via
//     url_rewrite.go's swBaseClient) and webhook signature header.
//   - resolver.go — Resolver, which turns a company ID into a live Provider
//     via a tiered lookup (per-tenant ConfigStore, then a platform default,
//     then a caller-supplied fallback, then NoopProvider), with a TTL cache
//     and an EntitlementChecker gate in front of non-default backends.
//   - config_store.go, field_encrypter.go — per-tenant Config persistence
//     (Postgres, migration 016) with auth tokens and webhook secrets
//     encrypted at rest via platform/encryption, and the from-number
//     uniqueness guard that keeps two tenants from claiming one number.
//   - webhook.go — ReadWebhookBody (an 8 KiB ceiling) and VerifyAndResolve,
//     the security gate that resolves the owning tenant from the target
//     number, then verifies that tenant's own webhook signature, before a
//     status callback or inbound SMS is ever handed to caller-supplied
//     sinks. WebhookHandler wires that gate to
//     SMSStatusSink/CallStatusSink/InboundSMSSink.
//   - tcpa.go, optout_store.go — TCPAService (47 CFR § 64.1200(a)(10)
//     STOP/START/HELP keyword matching, fail-closed CheckSendAllowed
//     precondition gate) and PostgresPhoneOptOutStore (migration 017), the
//     package's only compliance primitive — campaign-level consent and
//     quiet-hours policy remain the caller's responsibility.
//
// # Scope
//
// This package covers the transport and per-tenant plumbing four AOC
// consumers (politihub, justinforme, navigators, eden-biz) each built
// independently: provider abstraction, per-tenant encrypted config, webhook
// verification and parsing, and the STOP/START/HELP compliance primitive.
// It deliberately does not cover campaign orchestration, a send-worker
// queue, message templates, or MMS — those stay in the consumers that
// already have them. See README.md for how to wire the package and
// MIGRATION.md for how each consumer moves onto it.
package telephony
