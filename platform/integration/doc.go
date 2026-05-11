// Package integration provides cross-package integration tests that exercise
// the Eden Family launch surface end-to-end.
//
// This package is the canonical wiring example for products composing the
// platform stack: household + consent + auth + feature-flags + billing-rail
// + livekit. Each scenario reads top-to-bottom as a user journey.
//
// The integration test suite is the verification artifact for objective 33
// (M9 — Eden Family launch-ready) in the portfolio standardization plan. It
// proves the platform packages compose correctly without requiring Postgres,
// real Apple/Google/Stripe SDKs, or a LiveKit server.
//
// See docs/eden-family-integration.md for the architecture diagram and
// docs/eden-family-launch-checklist.md for the launch-readiness gating.
package integration
