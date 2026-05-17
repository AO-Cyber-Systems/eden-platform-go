// Package observability is the canonical observability surface for Eden
// services.
//
// Two complementary pipelines:
//
//  1. Sentry tier — the "wake me at 3am" operator-pager pipeline. Configured
//     via Setup + Config.SentryDSN. errortrack handles event capture and PII
//     scrubbing; the slog default handler is composed with the Sentry slog
//     handler so error-level logs auto-promote into Sentry events.
//
//  2. OTLP tier — the audit/SLO/observability pipeline. Configured via
//     SetupOTLP + OTLPConfig, or by setting Config.OTLP before calling
//     Setup. Wires OTel TracerProvider, MeterProvider, and (in a follow-up)
//     LoggerProvider to an OTLP/HTTP collector. EP-AUDIT-OTLP (Obj 9) will
//     consume the LoggerProvider to ship audit events.
//
// # OTLP availability guarantee
//
// SetupOTLP NEVER blocks boot and NEVER panics when the collector is
// unreachable. Three degrade paths:
//
//   - cfg.Endpoint == ""  → no-op providers, info log, nil error
//   - exporter init fails → no-op providers, warning log, nil error
//   - collector down at runtime → bounded retry (2-minute max-elapsed),
//     bounded queue (2048 spans), bounded shutdown (5-second deadline)
//
// This guarantee is the load-bearing piece of Objective 1 success criterion 4
// (absence of collector must not crash the service).
//
// # Deferred to follow-up TRDs
//
//   - Logger provider via otlploghttp (OTel SDK log API is still beta in
//     v1.43.0 and the canonical wiring path has churned across minor
//     versions). Tracer + Meter ship now; logs land when otlploghttp is
//     stable.
package observability
