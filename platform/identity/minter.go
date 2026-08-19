package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/kmssigner"
	"github.com/golang-jwt/jwt/v5"
)

// DefaultTTL is the lifetime a Minter stamps onto a context unless the caller
// configures another one.
//
// A context is an assertion that a principal was authenticated a moment ago,
// not a session. Its lifetime is the window in which a stolen copy is still
// worth replaying, so it is measured in minutes: long enough to survive a slow
// request and some clock skew, short enough that revocation infrastructure is
// not what stands between a leaked context and its expiry.
const DefaultTTL = 15 * time.Minute

// Misconfiguration reported by NewMinter. These are distinct and separately
// matchable with errors.Is: they surface at process start, to an operator who
// has to know which of several settings is wrong, and there is no
// authorization oracle to protect by collapsing them.
var (
	// ErrNilSigner reports a Minter constructed without a signer. There is no
	// in-process fallback key — a context this package cannot sign through the
	// signing seam is one it does not mint.
	ErrNilSigner = errors.New("identity: minter has no signer")

	// ErrMissingKeyID reports an empty key id. A verifier selects the
	// verification key by the kid header, so a context minted without one is
	// unverifiable as soon as the issuer holds more than one key — which it
	// does from the first rotation onward.
	ErrMissingKeyID = errors.New("identity: minter has no key id")

	// ErrMissingIssuer reports an empty issuer. A verifier resolves a key
	// source by issuer, and an empty one matches no configured trust entry.
	ErrMissingIssuer = errors.New("identity: minter has no issuer")

	// ErrUnsupportedAlgorithm reports a signer whose algorithm this minter
	// cannot pair with a signing method. It fires at construction rather than
	// at the first mint, so a wiring mistake is a start-up failure instead of
	// a run-time one.
	ErrUnsupportedAlgorithm = errors.New("identity: unsupported signing algorithm")

	// ErrInvalidTTL reports a non-positive context lifetime, which would mint
	// a context that has already expired.
	ErrInvalidTTL = errors.New("identity: context lifetime must be positive")

	// ErrInvalidClock reports a nil time source passed to WithClock.
	ErrInvalidClock = errors.New("identity: clock must not be nil")
)

// Rejected mint input. The claims a context is built from are the caller's to
// supply, and a context that cannot carry an honest answer is not minted.
var (
	// ErrInvalidAssurance reports an assurance level outside the AAL1/AAL2/
	// AAL3 ladder. A free-text level cannot be compared against a policy
	// threshold, so a consumer would have to drop it or guess at it, and
	// neither is acceptable for a value an audit record is built from.
	ErrInvalidAssurance = errors.New("identity: unrecognised assurance level")

	// ErrMalformedTokenRef reports a TokenRef that is not a handle produced by
	// TokRef. The claim is a correlation handle; accepting an arbitrary string
	// invites the upstream credential itself being passed here, which would
	// sign a live credential into a token that crosses service boundaries and
	// is logged at every hop.
	ErrMalformedTokenRef = errors.New("identity: token reference is not a derived handle")
)

// tokenRefWidth is the number of characters TokRef emits. It is derived from
// the function rather than restated as a literal so the two cannot drift.
var tokenRefWidth = len(TokRef("derive the width from the function itself"))

// MintInput is the authenticated subject data a caller supplies for one
// context. The minter does not discover any of it: every claim below is an
// assertion the caller is making, and the minter's job is to state it
// faithfully and sign it.
//
// Whatever produced this input is what actually authenticated the principal.
// This package deliberately knows nothing about how that happened, so an
// issuer built on top of it can change its authentication story without
// reshaping the context it emits.
type MintInput struct {
	// Subject names the authenticated principal. Required: a context that does
	// not name a principal cannot authorize one.
	Subject string

	// Tenant is the tenant slug the principal is acting within. Required, and
	// never inferred — an absent tenant read as "all tenants" is a cross-tenant
	// escalation.
	Tenant string

	// Entitlements are the entitlement strings resolved at issuance. A nil or
	// empty list is a legitimate outcome and is emitted as an empty JSON
	// array, keeping "this principal has none" distinguishable from "this
	// issuer does not speak the claim".
	Entitlements []string

	// Assurance is the level actually achieved during authentication: AAL1,
	// AAL2 or AAL3. Required, and never defaulted — see the honesty note on
	// Mint.
	Assurance string

	// TokenID is an optional jti. It is caller-supplied rather than generated
	// here because its value is correlating this context with a record the
	// caller already holds; a value invented by this package would correlate
	// with nothing. Omitted from the token when empty.
	TokenID string

	// TokenRef is an optional correlation handle for the upstream credential
	// this context derives from, as produced by TokRef.
	//
	// Pass the DERIVED handle, not the credential: this field exists so that
	// an upstream credential never has to be handed to the minter at all.
	// Mint rejects anything that is not a TokRef-shaped handle. For logging
	// and forensics only — it is not a credential and nothing may gate on it.
	TokenRef string
}

// MinterOption adjusts a Minter at construction. Options are applied before
// validation, so an option that supplies an unusable value is reported by
// NewMinter rather than at the first mint.
type MinterOption func(*Minter)

// WithTTL sets the context lifetime, overriding DefaultTTL. It must be
// positive.
//
// Lengthening it widens the window in which a captured context is still worth
// replaying, and nothing downstream can narrow it again, so treat a longer
// lifetime as a decision about blast radius rather than about convenience.
func WithTTL(ttl time.Duration) MinterOption {
	return func(m *Minter) { m.ttl = ttl }
}

// WithClock replaces the time source used to stamp iat and exp. It must not be
// nil.
//
// This is a seam for tests and for callers that already hold a controlled time
// source. It does not weaken any input requirement: the subject data below is
// still required in full, so a controlled clock cannot be used to mint a
// context that would not otherwise be minted.
func WithClock(now func() time.Time) MinterOption {
	return func(m *Minter) { m.now = now }
}

// Minter mints signed identity contexts for one issuer, with one signing key.
//
// It is deliberately narrow. It does not authenticate anyone, does not decide
// what a principal is entitled to, and does not choose an assurance level — a
// caller that has already done those things hands it the result. That is what
// lets an issuer be built on top without this having to change shape.
//
// A Minter is immutable after construction and safe for concurrent use, so a
// process shares one across every request path. It is only as concurrency-safe
// as the Signer it was given; the signing seam requires signers to be safe for
// concurrent use.
type Minter struct {
	signer kmssigner.Signer
	method jwt.SigningMethod
	keyID  string
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewMinter returns a Minter that signs contexts for issuer with signer,
// publishing keyID in the kid header of everything it mints.
//
// keyID must resolve, for a verifier, against whatever key set it trusts for
// this issuer; that correspondence is a deployment concern this package cannot
// check. Every argument is required, and each omission is reported distinctly.
//
// Signing goes through the platform signing seam, which keeps the private half
// wherever it lives — an HSM or a hosted key service — rather than requiring it
// in this process's memory. Constructing a Minter has no process-global effect:
// see signingMethodFor for why the seam's methods are used directly rather than
// registered.
func NewMinter(signer kmssigner.Signer, keyID, issuer string, opts ...MinterOption) (*Minter, error) {
	m := &Minter{
		signer: signer,
		keyID:  keyID,
		issuer: issuer,
		ttl:    DefaultTTL,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}

	if m.signer == nil {
		return nil, ErrNilSigner
	}
	if m.keyID == "" {
		return nil, ErrMissingKeyID
	}
	if m.issuer == "" {
		return nil, ErrMissingIssuer
	}
	if m.ttl <= 0 {
		return nil, fmt.Errorf("%w: got %v", ErrInvalidTTL, m.ttl)
	}
	if m.now == nil {
		return nil, ErrInvalidClock
	}

	method, err := signingMethodFor(m.signer.SigningAlgorithm())
	if err != nil {
		return nil, err
	}
	m.method = method

	return m, nil
}

// Mint builds a context from in, stamps the required headers and signs it,
// returning the compact JWS.
//
// The assurance level is reported exactly as supplied. Mint never substitutes
// one, never fills in an absent one, and in particular never rounds upward:
// the claim propagates into downstream authorization decisions and audit
// records, where a level nobody actually achieved is indistinguishable from
// one that was. A caller that authenticated with a single factor says so, and
// the record says so too.
//
// There is no mint path that does without in. A context asserts that some
// principal was authenticated, and this package has no way to establish that
// on its own, so input that could not support such an assertion is refused
// rather than completed with defaults.
//
// On any error the returned token is empty; a caller never has to decide
// whether a token accompanying an error is safe to use.
func (m *Minter) Mint(in MintInput) (string, error) {
	claims, err := m.contextClaims(in)
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(m.method, claims)
	// Both headers are set explicitly. The library defaults typ to "JWT" and
	// sets no kid at all, and neither default is usable here: a verifier
	// asserts the context type so that an access token presented in this slot
	// is rejected on its type rather than on its signature, and selects the
	// verification key by kid.
	token.Header["typ"] = JWTType
	token.Header["kid"] = m.keyID

	signed, err := token.SignedString(m.signer)
	if err != nil {
		return "", fmt.Errorf("identity: sign context: %w", err)
	}
	return signed, nil
}

// contextClaims validates the caller's input and assembles the claim set.
func (m *Minter) contextClaims(in MintInput) (*Claims, error) {
	// The three required claims report the same sentinels a verifier reports
	// for a context that arrives without them: the rule is one rule, stated
	// once, and a caller matching on it does not need to know which side of
	// the wire it fired on.
	if in.Subject == "" {
		return nil, fmt.Errorf("identity: mint: %w", ErrMissingSubject)
	}
	if in.Tenant == "" {
		return nil, fmt.Errorf("identity: mint: %w", ErrMissingTenant)
	}
	if in.Assurance == "" {
		return nil, fmt.Errorf("identity: mint: %w", ErrMissingAssurance)
	}
	if !knownAssurance(in.Assurance) {
		return nil, fmt.Errorf("%w: %q, want one of %q, %q, %q",
			ErrInvalidAssurance, in.Assurance, AAL1, AAL2, AAL3)
	}
	if in.TokenRef != "" && !isDerivedTokenRef(in.TokenRef) {
		// The rejected value is deliberately absent from this message. The
		// failure this check exists to catch is an upstream credential being
		// passed here, and quoting it back would copy that credential into
		// every log that records the error.
		return nil, fmt.Errorf("%w: want %d lowercase hex characters as produced by TokRef, got %d characters",
			ErrMalformedTokenRef, tokenRefWidth, len(in.TokenRef))
	}

	// Copied rather than referenced: the token is a snapshot of what the
	// caller asserted at this instant, and a later mutation of the caller's
	// slice must not appear to have been part of it. The copy is non-nil even
	// when empty, so the claim marshals as [] rather than null.
	entitlements := make([]string, len(in.Entitlements))
	copy(entitlements, in.Entitlements)

	issued := m.now().UTC()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   in.Subject,
			IssuedAt:  jwt.NewNumericDate(issued),
			ExpiresAt: jwt.NewNumericDate(issued.Add(m.ttl)),
			ID:        in.TokenID,
		},
		Tenant:       in.Tenant,
		Entitlements: entitlements,
		AAL:          in.Assurance,
		CtxVer:       Version,
		TokRef:       in.TokenRef,
	}

	// The minter holds itself to the rules it expects a consumer to apply, so
	// it cannot emit a context that its own verifier would reject. This is
	// unreachable given the checks above; it is here so that it stays
	// unreachable as both sides change.
	if err := claims.Validate(DefaultVersionSet()); err != nil {
		return nil, fmt.Errorf("identity: mint: refusing to mint an unvalidatable context: %w", err)
	}
	return claims, nil
}

// signingMethodFor returns the signing seam's method for alg, or an error
// naming what was offered and what is supported.
//
// The method value is constructed here rather than resolved through the JWT
// library's global registry, and the seam's registration helper is
// deliberately never called. That helper OVERRIDES the library's own
// ES256/RS256 methods for the whole process, irreversibly — there is no
// deregistration. A library that did so during construction would silently
// change how unrelated code in the same binary signs: anything resolving its
// method from the registry and passing an in-memory private key would start
// failing on a key-type mismatch, far from the import that caused it.
//
// Passing the method value straight to the token constructor needs no registry
// entry, so the host process is left exactly as it was found. Verification is
// unaffected either way — what gets signed is a standard JWS, which the
// library's own methods verify against a public key.
//
// The algorithm names are read off the seam's own methods rather than written
// out here, so they cannot drift from what the seam actually implements.
func signingMethodFor(alg string) (jwt.SigningMethod, error) {
	switch alg {
	case es256Alg():
		return &kmssigner.ES256SigningMethod{}, nil
	case rs256Alg():
		return &kmssigner.RS256SigningMethod{}, nil
	default:
		return nil, fmt.Errorf("%w: signer reports %q, want %q or %q",
			ErrUnsupportedAlgorithm, alg, es256Alg(), rs256Alg())
	}
}

func es256Alg() string { return (&kmssigner.ES256SigningMethod{}).Alg() }

func rs256Alg() string { return (&kmssigner.RS256SigningMethod{}).Alg() }

// knownAssurance reports whether level is a rung of the assurance ladder.
func knownAssurance(level string) bool {
	switch level {
	case AAL1, AAL2, AAL3:
		return true
	default:
		return false
	}
}

// isDerivedTokenRef reports whether ref has the shape TokRef produces: exactly
// tokenRefWidth lowercase hexadecimal characters.
//
// This is a shape check, not a security control — it cannot confirm the handle
// was derived from any particular credential. What it does catch is the one
// mistake worth catching here: a raw credential passed where its digest
// belongs.
func isDerivedTokenRef(ref string) bool {
	if len(ref) != tokenRefWidth {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
