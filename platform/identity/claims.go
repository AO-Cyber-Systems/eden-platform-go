// Package identity defines the wire contract for a signed identity context.
//
// An identity context is a short-lived, signed assertion that some boundary —
// an edge gateway, an application front door, an identity provider — has
// already authenticated a principal. A service behind that boundary verifies
// the context and maps it onto its own authorization model rather than
// re-authenticating the principal itself.
//
// # The wire format is frozen
//
// A context crosses service boundaries, and consumers on the far side of those
// boundaries may mirror this schema by hand instead of importing it. That makes
// the JSON encoding a contract rather than an implementation detail: renaming a
// field or editing a JSON tag compiles cleanly on both sides and then fails to
// authorize, at run time, in production. Treat the tags on Claims as immutable.
// Every schema change is negotiated through CtxVer and VersionSet instead.
//
// # What lives here
//
// The contract, and the two ends that speak it. This file holds the claim
// shape, the token type, the version this package emits, the set of versions a
// consumer accepts, and the semantic checks a JWT parser does not perform.
// Minter signs a context; Verifier checks one against a configured set of
// trusted issuers; KeySource is the seam a verifier resolves signing keys
// through, with an in-memory and a remote implementation.
//
// Where the private key lives, and which issuers a deployment trusts, are
// deliberately not decided here — both are supplied by the caller.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// JWTType is the value carried in the JWT `typ` header of an identity context.
//
// Explicit typing keeps a context from being accepted where a general-purpose
// access token is expected, and keeps an access token from being accepted as a
// context — even when both were signed by the same key. A verifier that does
// not require this header inherits every other token the issuer signs.
const JWTType = "identity-context+jwt"

// Version is the schema version this package stamps into CtxVer.
//
// It is not the same thing as the set of versions a consumer will accept; see
// VersionSet. Raising it is a coordinated change across every deployment:
// consumers must already accept the new version before anything emits it,
// otherwise the first context minted at the new version is rejected by
// whichever consumer has not been widened yet.
const Version = 1

// Authenticator assurance levels reported in the AAL claim, following the
// NIST SP 800-63B ladder:
//
//   - AAL1 — a single authentication factor.
//   - AAL2 — two distinct factors.
//   - AAL3 — two factors, one of them a hardware-based authenticator proving
//     possession through a cryptographic protocol.
//
// An issuer reports the level it actually achieved and never rounds upward.
// The claim propagates into downstream authorization decisions and audit
// records, where an inflated value is indistinguishable from a real one.
const (
	AAL1 = "AAL1"
	AAL2 = "AAL2"
	AAL3 = "AAL3"
)

// Validation failures. Each is returned wrapped, so callers match with
// errors.Is and may collapse the set into a single opaque rejection at their
// own boundary.
var (
	// ErrUnsupportedVersion reports a CtxVer outside the accepted set. This is
	// the failure that version negotiation exists to make legible: the wrapped
	// message names both the version that arrived and the versions this
	// consumer was configured to take.
	ErrUnsupportedVersion = errors.New("identity: unsupported context schema version")

	// ErrMissingSubject reports an absent `sub`. A context that does not name
	// a principal cannot authorize one.
	ErrMissingSubject = errors.New("identity: context has no subject")

	// ErrMissingTenant reports an absent `tnt`. Without it a context cannot be
	// scoped, and treating that as "any tenant" is a cross-tenant escalation.
	ErrMissingTenant = errors.New("identity: context has no tenant")

	// ErrMissingAssurance reports an absent `aal`. An unstated assurance level
	// must not be read as an adequate one.
	ErrMissingAssurance = errors.New("identity: context has no assurance level")
)

// Claims is the identity-context claim set.
//
// The JSON tags below are FROZEN. Independent consumers decode this document
// by key name against their own copy of the schema, so a renamed tag is a
// silent desync: it does not fail to compile, it fails to authorize. Adding a
// claim is a schema change that goes through Version and VersionSet, not a
// field appended in passing.
type Claims struct {
	// RegisteredClaims supplies the standard `iss`, `sub`, `iat`, `exp` and
	// `jti` claims. `sub` names the authenticated principal; `iss` identifies
	// the boundary that authenticated it and selects the key a verifier
	// resolves the signature against.
	jwt.RegisteredClaims

	// Tenant is the tenant slug the principal is acting within. It is
	// human-readable and stable, NOT an opaque identifier — never parse it as
	// a UUID, and never treat an empty value as "all tenants".
	Tenant string `json:"tnt"`

	// Entitlements are the entitlement strings resolved at issuance. The list
	// may legitimately be empty: a principal with no entitlements is a normal
	// outcome, distinct from an issuer that does not speak the claim. The
	// field therefore carries no omitempty and the key is always emitted.
	Entitlements []string `json:"ent"`

	// AAL is the authenticator assurance level actually achieved during
	// authentication — one of AAL1, AAL2 or AAL3.
	AAL string `json:"aal"`

	// CtxVer is the schema version of this claim set. A verifier checks it
	// against its configured VersionSet before reading any other claim.
	CtxVer int `json:"ctx_ver"`

	// TokRef is a correlation handle for the upstream credential this context
	// was derived from: the first eight bytes of its SHA-256 digest, hex
	// encoded. See the TokRef function.
	//
	// LOGGING AND FORENSICS ONLY. A verifier never recomputes it and never
	// gates on it — it generally cannot, because it never sees the upstream
	// credential. An absent value is not an error.
	TokRef string `json:"tok_ref"`
}

// Claims is usable directly with jwt.NewWithClaims and jwt.ParseWithClaims.
var _ jwt.Claims = (*Claims)(nil)

// VersionSet is the set of schema versions a consumer accepts.
//
// The accepted set is configuration, not a constant, and that is the point:
// when each consumer hard-codes its own notion of which versions are valid,
// the copies drift, and the drift surfaces only as a run-time authorization
// failure at the moment someone emits a version another party never learned
// about. Making the set explicit turns that into a deployment-time decision.
//
// The zero value accepts nothing, so a set that was never configured fails
// closed rather than admitting everything.
type VersionSet map[int]struct{}

// NewVersionSet returns the set containing exactly the given versions.
// Duplicates collapse; no arguments yields a set that accepts nothing.
func NewVersionSet(versions ...int) VersionSet {
	set := make(VersionSet, len(versions))
	for _, v := range versions {
		set[v] = struct{}{}
	}
	return set
}

// DefaultVersionSet returns the conservative default: exactly {Version}.
//
// It is deliberately as narrow as the strictest consumer in the field.
// Accepting an additional version is a decision a caller makes explicitly at
// construction, through NewVersionSet — never something inherited by default.
// Each call returns an independent set, so widening one copy cannot reach
// another caller's.
func DefaultVersionSet() VersionSet {
	return NewVersionSet(Version)
}

// Accepts reports whether v is a member of the set.
func (s VersionSet) Accepts(v int) bool {
	_, ok := s[v]
	return ok
}

// Versions returns the members in ascending order, for diagnostics and log
// lines that need to state what this consumer was configured to accept.
func (s VersionSet) Versions() []int {
	versions := make([]int, 0, len(s))
	for v := range s {
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions
}

// Validate applies the semantic rules a JWT parser does not: the schema
// version must be one this consumer accepts, and the claims an authorization
// decision is built from must actually be present.
//
// It assumes the signature, issuer and expiry have already been checked by the
// parser; it is the second half of verification, not a substitute for the
// first. This is the single home for these rules — callers invoke it rather
// than re-implementing the checks and drifting apart again.
//
// The signature takes the accepted set explicitly, which is why this
// deliberately does not satisfy jwt.ClaimsValidator: the set is per-consumer
// configuration and must not be smuggled in as package state.
//
// TokRef is never validated. It is a correlation handle, not a credential.
func (c *Claims) Validate(accepted VersionSet) error {
	if !accepted.Accepts(c.CtxVer) {
		return fmt.Errorf("%w: got %d, accepted %v", ErrUnsupportedVersion, c.CtxVer, accepted.Versions())
	}
	if c.Subject == "" {
		return ErrMissingSubject
	}
	if c.Tenant == "" {
		return ErrMissingTenant
	}
	if c.AAL == "" {
		return ErrMissingAssurance
	}
	return nil
}

// TokRef derives the value for the TokRef claim from an upstream credential:
// the first eight bytes of its SHA-256 digest, hex encoded as sixteen
// lowercase characters.
//
// The truncation is intentional. The result is long enough to correlate a
// context with the credential it came from across log lines and audit records,
// and short enough that it is useless for reconstructing the credential. It is
// not a security control: never compare it against a secret, and never gate a
// decision on it.
//
// An empty token yields an empty string rather than the digest of the empty
// string, which would otherwise stamp one identical constant onto every
// context that has no upstream credential and read like a genuine handle.
func TokRef(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}
