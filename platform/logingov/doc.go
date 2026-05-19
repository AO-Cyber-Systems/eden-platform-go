// Package logingov implements a thin OIDC Relying Party wrapper specialized
// for Login.gov — the U.S. federal government's shared-services consumer
// identity provider used by federal civilian agencies (GSA, DHS, USCIS,
// etc.).
//
// # Why a wrapper
//
// Login.gov is *almost* a standard OpenID Connect provider but differs in
// three production-critical ways that disqualify direct use of
// platform/oidcrp's generic helpers:
//
//  1. Client authentication is restricted to private_key_jwt (RFC 7523
//     §2.2). Login.gov does NOT accept client_secret_basic or
//     client_secret_post. Stock golang.org/x/oauth2's Config.Exchange
//     only supports the latter two methods, so we implement the
//     token-endpoint POST manually and inject a signed RFC 7523 client
//     assertion JWT.
//
//  2. ACR (Authentication Context Class Reference) values use a Login.gov-
//     specific URN taxonomy (urn:acr.login.gov:auth-only,
//     urn:acr.login.gov:verified-facial-match-required, etc.) that does not
//     overlap with generic OIDC ACR conventions. mapACR translates Login.gov
//     ACRs into AOID's canonical assurance enum (ial_1, ial_2, aal_2, aal_3,
//     aal_3_piv) — the single source of truth shared with the AOID sessions
//     schema's `aal` column.
//
//  3. The `sub` claim is per-RP — different RPs registered with the same
//     Login.gov account see distinct sub values. This is intentional
//     privacy-by-design from Login.gov; consumers (AOID) must scope the
//     federation subject by (tenant_id, idp_id, sub) and never expect sub
//     to be portable across RPs.
//
// # Surface
//
//   - Config — caller-supplied configuration. The RP signing key (RSA-2048
//     minimum) is provided externally; this package does NOT generate or
//     persist keys. Key lifecycle (KMS decryption, rotation) is the
//     caller's responsibility.
//   - Client — composed wrapper holding the cached *oidc.Provider, OAuth2
//     config, and verifier cache reference.
//   - NewClient — constructs a Client; runs cfg validation + discovery.
//   - Client.BuildAuthURL — composes the authorization-code URL with
//     PKCE+nonce (via oidcrp.BuildAuthURL) and Login.gov-specific
//     acr_values defaulting.
//   - Client.Exchange — completes the authorization-code flow: signs the
//     client_assertion, POSTs to the token endpoint manually, verifies
//     the returned ID token, validates nonce, and maps ACR.
//   - SignClientAssertion — exported helper that wraps
//     github.com/hashicorp/cap/oidc/clientassertion.NewJWTWithRSAKey for
//     RFC 7523 §2.2 client_assertion JWTs.
//   - mapACR — exported pure function: Login.gov ACR URN → AOID canonical
//     assurance level. The mapping table here is authoritative across
//     Eden + AOID + future AOSentry.
//   - ID — the post-exchange identity record returned to callers,
//     containing the verified sub/email + raw + mapped ACR + claim
//     passthrough for downstream attribute mapping.
//
// # Non-goals
//
// This package does NOT ship HTTP routes — AOID owns the
// /federate/logingov/{tenant}/{start,callback} handlers and wires session
// bridging + audit emission itself.
//
// This package does NOT persist state — no DB, no in-memory store beyond
// the cached *oidc.Provider/*oidc.IDTokenVerifier handed in via
// platform/oidcrp's caches.
//
// This package does NOT manage the RP signing key. The caller decrypts the
// KMS-wrapped key material and passes the *rsa.PrivateKey into Config.
// Key rotation (re-registering with the Login.gov Partner Portal or
// hosting jwks_uri) is also caller-side.
//
// # Multi-consumer
//
// AOID Obj 7 (federal identity integrations) is the first consumer. The
// future AOSentry FedRAMP-Moderate console will reuse this package
// unchanged; do not add AOID-specific shapes here.
//
// # References
//
//   - https://developers.login.gov/oidc/ — Login.gov OIDC documentation
//   - RFC 7523 §2.2 — JWT bearer client authentication (private_key_jwt)
//   - RFC 9700 — OAuth 2.0 Security Best Current Practice (mandates PKCE)
//   - 07-RESEARCH.md §A.1, §A.4, §H.8 — AOID-side planning notes
package logingov
