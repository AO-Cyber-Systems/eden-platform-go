// Package aoid hosts the AO ID identity service binaries and runtime.
//
// AO ID is the standalone identity system for the AO Cyber portfolio
// (decision D2). It wraps the platform identity packages —
// platform/auth (credential management, sessions, OIDC SP, SAML SP+IdP,
// WebAuthn, TOTP, email-OTP, apikeys, password, legacy_bcrypt),
// platform/household (family / parent-of-record / child-account model)
// and platform/consent (append-only COPPA / GDPR-K consent ledger) —
// behind a single deployable service that products federate into.
//
// This package tree contains the service runtime; reusable identity logic
// stays in platform/* and is consumed verbatim. Sub-packages:
//
//   - config: env-driven configuration loader
//   - server: HTTP server, route registration, graceful shutdown
//   - discovery: OIDC discovery document + issuer-not-active stubs
//   - jwks: JSON Web Key Set endpoint for JWT verification keys
//   - composition: builds the platform/auth, platform/household and
//     platform/consent services from a backend (in-memory devstore or
//     pgstore-backed Postgres)
//   - fixtures: deterministic seed data for dev / smoke tests
//
// Scaffolding milestone (objective 29) includes everything above EXCEPT
// active token issuance — the discovery document advertises the endpoints
// but they reply 503 until objective 30 turns the issuer on. JWKS is
// already live so federation tooling can begin probing the service in
// staging.
//
// Service location decision: AO ID lives inside eden-platform-go as a
// second cmd binary (sibling of cmd/eden-platform-dev) rather than its own
// repository. Rationale lives in
// .planning/objectives/29-aoid-service-scaffold/29-01-TRD.md.
package aoid
