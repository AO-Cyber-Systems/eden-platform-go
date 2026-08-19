package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// verificationLeeway is the clock skew tolerated on the time-based claims.
//
// Two hosts that agree to the second are a laboratory condition, and a context
// refused for a two-second disagreement is an authorization failure nobody can
// reproduce. It is small enough that it does not meaningfully extend the window
// in which a captured context is worth replaying.
const verificationLeeway = 5 * time.Second

// verificationMethods are the only signature algorithms a context may be
// signed with.
//
// Pinning them is a security control rather than a compatibility list. Without
// it the token's own alg header chooses how the resolved key is used, which is
// two separate forgeries: "none", which removes the signature check altogether,
// and an HMAC algorithm, where an attacker uses the issuer's PUBLIC key — a
// value the issuer publishes — as the shared secret.
var verificationMethods = []string{"ES256", "RS256"}

// ErrInvalidContext is the ONLY error Verify reports, for every reason it
// might refuse a context: a bad signature, an unknown issuer, an unresolvable
// key id, an expired or not-yet-valid token, an unaccepted algorithm, the
// wrong token type, a schema version outside the accepted set, or a claim an
// authorization decision needs and did not get.
//
// The uniformity is deliberate and load-bearing. A caller that can tell those
// apart publishes the difference: a per-reason error becomes a per-reason
// response, and a per-reason response is an oracle. "Unknown issuer" versus
// "bad signature" tells a prober whether it has guessed a trusted issuer;
// "expired" versus "bad signature" confirms it holds a structurally valid
// token and needs only a fresher one.
//
// It is returned unwrapped. Wrapping the underlying cause would republish it
// through errors.Is and errors.As and defeat the collapse entirely — including
// the named reasons Claims.Validate reports, which exist for a caller that
// validates claims itself, not for one verifying a context from elsewhere.
//
// The specific cause is not discarded, only withheld: see WithRejectionLogger,
// which hands it to the operator instead of to the caller.
var ErrInvalidContext = errors.New("identity: identity context is not valid")

// Misconfiguration reported by NewVerifier. These are distinct and separately
// matchable with errors.Is, unlike everything Verify reports: they surface at
// process start, to an operator who has to know which setting is wrong, and
// none of them can be provoked by a token.
var (
	// ErrNoTrustedIssuers reports a verifier built with an empty trust set.
	// Nothing about which issuer is trusted is compiled in, so a verifier
	// given no issuers trusts nobody rather than falling back to a default.
	ErrNoTrustedIssuers = errors.New("identity: verifier must trust at least one issuer")

	// ErrMissingTrustedIssuer reports a trusted issuer with no name. It could
	// never be matched against a token's iss, and an empty name would silently
	// become the entry every context without an issuer resolves to.
	ErrMissingTrustedIssuer = errors.New("identity: trusted issuer name is empty")

	// ErrNilKeySource reports a trusted issuer with no key source. There would
	// be nothing to verify its contexts against.
	ErrNilKeySource = errors.New("identity: trusted issuer has no key source")

	// ErrDuplicateTrustedIssuer reports the same issuer listed twice. The two
	// entries disagree about which keys sign for it, and silently keeping one
	// of them would make the trust set depend on the order it was written in.
	ErrDuplicateTrustedIssuer = errors.New("identity: trusted issuer is listed more than once")

	// ErrNoAcceptedVersions reports a verifier that accepts no schema version
	// and would therefore refuse every context it is ever shown. That is worth
	// reporting at start-up rather than discovering as a total outage.
	ErrNoAcceptedVersions = errors.New("identity: verifier must accept at least one schema version")
)

// TrustedIssuer pairs an issuer with the keys that sign for it.
//
// The pairing is the security property. A verifier that held one flat pool of
// keys for every issuer would accept a context from issuer A signed with
// issuer B's key, which is exactly the confusion a multi-issuer deployment
// invites.
type TrustedIssuer struct {
	// Issuer is the exact iss value contexts from this issuer carry. It is
	// compared literally: no normalisation, no trailing-slash equivalence, no
	// prefix matching. A verifier that guesses at equivalence is a verifier an
	// attacker gets to guess with.
	Issuer string

	// Keys resolves the kid a context from this issuer names. It is consulted
	// for contexts claiming this issuer and no other.
	Keys KeySource
}

// VerifierOption adjusts a Verifier at construction. Options are applied
// before validation, so an option carrying an unusable value is reported by
// NewVerifier rather than at the first verification.
type VerifierOption func(*Verifier)

// WithAcceptedVersions sets the schema versions this verifier accepts,
// overriding DefaultVersionSet.
//
// This is the negotiation seam. The accepted set is per-verifier configuration
// and never a package-level default, so one deployment can be widened ahead of
// an emitter being raised without that widening reaching any other consumer.
// The set is copied, so a caller that later edits its own copy cannot widen a
// verifier already in service.
func WithAcceptedVersions(accepted VersionSet) VerifierOption {
	return func(v *Verifier) { v.accepted = accepted }
}

// WithRejectionLogger supplies a sink for the specific reason a context was
// refused.
//
// This is how a rejection stays diagnosable without becoming an oracle: the
// operator gets the cause, the caller gets ErrInvalidContext. The error handed
// to the sink is detailed and may wrap a named sentinel; it is never the value
// Verify returns.
//
// It is called only for a rejection, synchronously, on the verifying
// goroutine, so an implementation must be cheap and safe for concurrent use.
// Whatever it writes to is attacker-influenced, so it belongs somewhere an
// operator reads and not in a response.
func WithRejectionLogger(log func(error)) VerifierOption {
	return func(v *Verifier) { v.logRejection = log }
}

// Verifier checks a signed identity context against a configured set of
// trusted issuers.
//
// The trust set is plural from day one. The same service verifies an
// in-process issuer now and an external provider later, and a verifier built
// around a single pinned issuer has to be reshaped to do that. The plural set
// is also why the parser's own issuer check is unusable here: it pins exactly
// one issuer, so the match is made against the configured set instead.
//
// A Verifier is immutable after construction and safe for concurrent use, so a
// process shares one across every request path. It is only as concurrency-safe
// as the key sources it was given; the KeySource seam requires them to be safe
// for concurrent use.
type Verifier struct {
	trusted      map[string]KeySource
	accepted     VersionSet
	parser       *jwt.Parser
	logRejection func(error)
}

// NewVerifier returns a Verifier trusting exactly the given issuers.
//
// Every issuer a verifier trusts arrives here, from configuration. There is no
// default issuer, no built-in list, and no way to widen the set afterwards:
// the trust set is fixed at construction and the caller's slice is not
// retained, so a slice edited later cannot add an issuer to a verifier already
// serving traffic.
//
// The accepted schema versions default to DefaultVersionSet; see
// WithAcceptedVersions. Each misconfiguration is reported distinctly, because
// an operator reading this error has to know which setting to correct.
func NewVerifier(trusted []TrustedIssuer, opts ...VerifierOption) (*Verifier, error) {
	v := &Verifier{
		accepted: DefaultVersionSet(),
		parser: jwt.NewParser(
			jwt.WithValidMethods(verificationMethods),
			jwt.WithLeeway(verificationLeeway),
		),
	}
	for _, opt := range opts {
		opt(v)
	}

	if len(trusted) == 0 {
		return nil, ErrNoTrustedIssuers
	}
	v.trusted = make(map[string]KeySource, len(trusted))
	for _, issuer := range trusted {
		if issuer.Issuer == "" {
			return nil, ErrMissingTrustedIssuer
		}
		if issuer.Keys == nil {
			return nil, fmt.Errorf("%w: %q", ErrNilKeySource, issuer.Issuer)
		}
		if _, duplicate := v.trusted[issuer.Issuer]; duplicate {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTrustedIssuer, issuer.Issuer)
		}
		v.trusted[issuer.Issuer] = issuer.Keys
	}

	if len(v.accepted) == 0 {
		return nil, ErrNoAcceptedVersions
	}
	// Copied for the same reason the trust set is: a version set the caller
	// can still write to is an accepted set some unrelated code path can widen
	// at run time, and a widening nobody decided on is not a decision.
	v.accepted = NewVersionSet(v.accepted.Versions()...)

	return v, nil
}

// Verify checks token and returns the claims it carries.
//
// On success the claims have been through every check this package makes: the
// issuer is one this verifier was configured to trust, the signature verifies
// under a key that issuer publishes, the algorithm is one of the two admitted,
// the token is within its validity window, it is typed as an identity context
// rather than as some other token the issuer signs, and Claims.Validate has
// passed against this verifier's accepted version set.
//
// On any failure it returns a nil claim set and ErrInvalidContext — always
// that value, never a wrapped one, and never a different one. A caller cannot
// learn why, by design; see ErrInvalidContext. The single exit below is what
// makes that structural rather than a rule each failure path has to remember.
//
// ctx is passed to the issuer's key source, so a remote key set fetch is bound
// to the lifetime of whatever asked for this verification.
func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	claims, err := v.verify(ctx, token)
	if err != nil {
		v.report(err)
		return nil, ErrInvalidContext
	}
	return claims, nil
}

// verify does the work and reports the real reason. Its errors are for the
// operator: they reach Verify's caller only as ErrInvalidContext.
func (v *Verifier) verify(ctx context.Context, token string) (*Claims, error) {
	issuer, err := v.claimedIssuer(token)
	if err != nil {
		return nil, err
	}

	// An issuer outside the trust set is refused HERE — before a key is
	// resolved and before a signature is checked.
	//
	// This is what makes reading iss ahead of verification safe. The value is
	// unverified at this point, so it is used for exactly one thing: choosing
	// which trusted key set the signature is checked against. It never becomes
	// an identity, a tenant, or an authorization input, and it is never
	// consulted again after the claims below arrive verified. A forged iss can
	// therefore only ever select a key set that will refuse the signature —
	// a context signed by one issuer's key but claiming another's name is
	// checked against the claimed issuer's keys, and fails.
	keys, trusted := v.trusted[issuer]
	if !trusted {
		return nil, fmt.Errorf("identity: issuer %q is not in this verifier's trust set", issuer)
	}

	claims := &Claims{}
	parsed, err := v.parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		// The key is resolved by the kid the token names and by nothing else:
		// no fallback to "the only key" or "the first key". A missing or
		// non-string kid resolves to the empty string, which every key source
		// refuses.
		kid, _ := t.Header["kid"].(string)
		return keys.KeyForKID(ctx, kid)
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("identity: parser accepted the context without marking it valid")
	}

	// The parser does not check typ, and it is what keeps this slot from
	// accepting every other token the issuer signs. Without it an access token
	// from the same issuer, signed by the same key, verifies here perfectly.
	if typ, _ := parsed.Header["typ"].(string); typ != JWTType {
		return nil, fmt.Errorf("identity: token type header is %v, want %q", parsed.Header["typ"], JWTType)
	}

	// The semantic rules live in one place. Re-implementing them here is how
	// consumers drifted apart in the first place, and the accepted set is this
	// verifier's configuration rather than any package-level default.
	if err := claims.Validate(v.accepted); err != nil {
		return nil, err
	}

	return claims, nil
}

// claimedIssuer reads iss without verifying anything.
//
// The claim set decoded here is deliberately local and discarded: it is
// untrusted input, and the only thing taken from it is the issuer name used to
// select a key set. The claims Verify returns come from the verified parse in
// verify, never from this one.
func (v *Verifier) claimedIssuer(token string) (string, error) {
	var unverified Claims
	if _, _, err := v.parser.ParseUnverified(token, &unverified); err != nil {
		return "", err
	}
	return unverified.Issuer, nil
}

// report hands the specific cause to the operator's sink, if one was
// configured. Nothing it is given ever reaches Verify's caller.
func (v *Verifier) report(err error) {
	if v.logRejection != nil {
		v.logRejection(err)
	}
}
