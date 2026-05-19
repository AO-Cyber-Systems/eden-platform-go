// Package oidcrp implements the multi-tenant OIDC Relying Party primitives
// that AOID's federation subsystem (Obj 6, TRDs 06-05+) consumes — and that
// any future Eden service speaking inbound OIDC can reuse.
//
// # Why this package exists
//
// github.com/coreos/go-oidc/v3 deliberately omits four caller
// responsibilities that every production RP needs to get right:
//
//  1. state generation and verification (CSRF + tenant/IdP binding)
//  2. nonce generation and verification (replay defence)
//  3. PKCE — mandatory per RFC 9700 / OAuth 2.1, optional in go-oidc
//  4. per-tenant *oidc.Provider and *oidc.IDTokenVerifier caching
//     (without which discovery + JWKS round-trips happen on every callback)
//
// Implementations vary in subtle, exploitable ways. This package bundles
// those four responsibilities into one cohesive surface so consumers don't
// reinvent (and almost certainly get wrong) the nonce-check, state-sign,
// and PKCE plumbing.
//
// # Surface
//
//   - ProviderCache.Get / Invalidate — caches *oidc.Provider by an opaque
//     caller-supplied key (typically tenant_id+idp_id); singleflight
//     collapses concurrent misses to one underlying discovery call.
//   - VerifierCache.Get / Invalidate — caches *oidc.IDTokenVerifier pinned
//     to a (provider, clientID, algs) triple; defaults to RS256+ES256.
//   - BuildAuthURL — composes the authorization-code URL with PKCE (S256)
//     and nonce mandatorily attached. Refuses to skip PKCE.
//   - ExchangeAndVerify — code-for-token exchange + ID-token verification
//     plus nonce binding. Returns ErrNonceMismatch on mismatch and
//     ErrMissingIDToken when the OP omits id_token (e.g., wrong scopes).
//   - ClaimMap + ApplyClaimMap — JSON-path-lite resolver that maps OIDC
//     claims to a normalized struct (Email + Sub required), supporting
//     per-IdP overrides without bespoke parsing code at each call site.
//   - SignedState + VerifyState — HMAC-SHA256 signed state with TTL
//     enforcement and required-field guards (tenant, idp, nonce).
//   - InFlightStore interface + InMemoryInFlightStore — single-use
//     {nonce -> PKCE verifier + redirect target} record store for
//     resuming a flow at the callback. AOID Obj 6 ships a Postgres
//     implementation (TRD 06-05) for multi-replica deployments.
//
// # Non-goals
//
// This package does NOT ship an HTTP handler. AOID writes
// /federate/oidc/{tenant}/{idp}/{start,callback} on its own to retain full
// control over template rendering, audit emission, session bridging, and
// step-up policy gates. The handler imports oidcrp and orchestrates the
// pieces above.
//
// This package does NOT ship the OIDC OP role. Obj 4 already provides the
// authorization server; Obj 6 TRD 06-10 extends it (claim mapping for
// outbound assertions, etc.).
//
// This package does NOT implement OIDC back-channel logout. Explicit
// deferral — see 06-RESEARCH §"Out of scope".
//
// # Concurrency
//
// All cache types are safe for concurrent Get/Invalidate from many
// goroutines. The InMemoryInFlightStore uses a single mutex; throughput
// targets one-row-per-active-flow which never exceeds single-digit-k/s
// peak in realistic AOID traffic.
//
// # References
//
//   - pkg.go.dev/github.com/coreos/go-oidc/v3/oidc — the underlying library
//   - RFC 6749 — OAuth 2.0 Authorization Framework
//   - RFC 7636 — Proof Key for Code Exchange (PKCE)
//   - RFC 9700 — OAuth 2.0 Security Best Current Practice (mandates PKCE)
//   - OpenID Connect Core 1.0 §3.1.2 — Authentication using the
//     Authorization Code Flow
//   - 06-RESEARCH.md §2, §3, Pitfalls §2/§3/§7 (in the AOID planning tree)
package oidcrp
