// Package saml is the Eden Platform's KMS-aware SAML 2.0 primitives
// package — Service Provider, Identity Provider, metadata cache, replay
// store, and XSW guard. It is the foundation for AOID Obj 6
// (federation-inbound-outbound-jit) and is intended for re-use by any
// AOCyber service that needs to consume or issue SAML 2.0 assertions
// with HSM/KMS-resident signing keys.
//
// # Design intent
//
// crewjam/saml is the canonical Go SAML library. This package wraps it
// with three AOCyber-specific concerns:
//
//  1. KMS-backed signing. crewjam exposes a `Signer crypto.Signer`
//     field on saml.IdentityProvider and saml.ServiceProvider, and the
//     underlying goxmldsig SigningContext now accepts crypto.Signer as
//     of v1.4.0. This package's KMSSigner adapts the
//     platform/kms.KMSSigner interface to crypto.Signer with explicit
//     hash-function whitelisting (SHA-256/384/512 only — no SHA-1).
//
//  2. XML Signature Wrapping (XSW) defense. The 2020 Mattermost
//     coordinated disclosure showed that multiple Go SAML libraries
//     had patched-then-re-broken XSW defenses. XSWGuard wraps
//     mattermost/xml-roundtrip-validator AND enforces at-most-one
//     <saml:Assertion> element BEFORE any signature validation runs.
//     Callers MUST invoke XSWGuard first — it is a precondition of
//     signature verification.
//
//  3. Replay defense. SAML's id-based replay protection requires a
//     shared store keyed by Assertion.ID with a TTL. ReplayStore is
//     the interface; this package provides InMemoryReplayStore for
//     tests and single-process deployments. The Postgres-backed
//     implementation lives in AOID TRD 06-05 (federation_assertion_log
//     table + INSERT ON CONFLICT DO NOTHING + rows-affected check).
//
// # Non-goal: metadata HTTP fetch
//
// Federation metadata is a tenant trust anchor. AOID stores metadata
// bytes in the federation_idp_inbound table and re-fetches only on
// operator-controlled rotation. crewjam's deprecated
// samlsp.Options.IDPMetadataURL field is intentionally NOT exposed here
// — callers MUST pass the bytes via SPOptions.IDPMetadata.
// ParseAndCacheMetadata caches by sha256(rawXML) so callers can pass
// the same bytes repeatedly without re-parsing.
//
// # Forked goxmldsig
//
// internal/forked/goxmldsig provides a single keystore type
// (MemoryX509KeyStoreSigner) whose GetKeyPair returns a crypto.Signer
// rather than upstream's *rsa.PrivateKey, plus a defensive
// NewSigningContextSigner constructor. See
// internal/forked/goxmldsig/UPSTREAM_PR.md for removal instructions
// when upstream ships an equivalent keystore type.
//
// # Thread safety
//
// All exported types and functions in this package are safe for
// concurrent use. KMSSigner is immutable; InMemoryReplayStore guards
// its map with a mutex. The metadata cache uses a sync.Mutex.
//
// # Audit and logging
//
// This package emits NO audit events or log lines. AOID composes audit
// emission via platform/audit at the federation handler layer (TRD
// 06-07 and 06-09).
//
// # References
//
//   - AOID Obj 6 RESEARCH.md §1 (federation architecture), §3 (XSW
//     defense), §9 (KMS Signer rationale), §10 (replay protection).
//   - SAML 2.0 Core: https://docs.oasis-open.org/security/saml/v2.0/saml-core-2.0-os.pdf
//   - Mattermost XSW disclosure:
//     https://mattermost.com/blog/coordinated-disclosure-go-xml-vulnerabilities/
//   - crewjam/saml: https://pkg.go.dev/github.com/crewjam/saml
//   - goxmldsig: https://pkg.go.dev/github.com/russellhaering/goxmldsig
package saml
