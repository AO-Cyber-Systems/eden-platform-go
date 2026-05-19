// Package risk is the AUTH-07 risk-evaluation primitive for the Eden platform.
//
// It provides a Signal-based, in-process rules engine that produces a 0-100
// risk score on every authentication attempt. Each Signal is a pluggable Go
// type with a name, weight, and trigger condition. The Evaluator sums
// triggered weights, clips to [0, 100], and returns a Result with the full
// triggered-signal trace for audit.
//
// Donor: none — fresh primitive for AOID Obj 9.
//
// Goals:
//   - In-process, sub-10ms p99 latency (with warm GeoIP cache).
//   - Interpretable (rules-based, not ML — auditor preference; see FedRAMP
//     audit posture in AOID's research §F.2).
//   - Pluggable (operators choose the signal set per tenant via Evaluator
//     config).
//   - Reusable (any Eden service that authenticates can adopt).
//
// Non-goals (future-extensible via the Signal interface):
//   - ML / ensemble models — a Signal implementation can wrap a model and
//     call it from inside Evaluate; the orchestrator stays the same.
//   - Remote behavior-analytics services — same wrapping pattern applies.
//
// The package exposes three core types:
//   - Signal: the pluggable trigger contract.
//   - Evaluator: the orchestrator that sums weights and clips.
//   - Request / Result: input + output value types.
//
// The package additionally exposes a GeoIPLookup interface and two
// implementations (MaxMindGeoIP, NoOpGeoIP) so geo-aware signals can degrade
// gracefully in air-gapped IL5 deployments where no MaxMind DB is mounted.
package risk
