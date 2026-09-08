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
//
// This TRD (Objective 40, TRD 01) establishes the seam only. Concrete
// backends (Twilio, SignalWire) and the config-driven resolver that turns a
// tenant ID into a live Provider arrive in later TRDs of the same objective.
package telephony
