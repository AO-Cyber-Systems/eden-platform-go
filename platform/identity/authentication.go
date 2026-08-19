package identity

import (
	"context"
	"errors"
	"fmt"
)

// FactorKind is the category an authentication factor belongs to.
//
// The three categories are the classical ones, and they are what assurance is
// derived from: two factors drawn from the same category tend to fail to the
// same attack, so they are one line of defence however many prompts the
// principal answered. See Authentication.Kinds.
type FactorKind string

const (
	// FactorKnowledge is something the principal knows — a password, a
	// passphrase, the answer to a security question.
	FactorKnowledge FactorKind = "knowledge"

	// FactorPossession is something the principal has. This is the category
	// most often mis-assigned: a one-time code from an authenticator app and a
	// printed recovery code are both possession factors, because what is being
	// demonstrated is custody of the seed or of the code sheet, not memory of
	// a secret.
	FactorPossession FactorKind = "possession"

	// FactorInherence is something the principal is — a biometric trait
	// measured by an authenticator.
	FactorInherence FactorKind = "inherence"
)

// factorKinds is the canonical order Kinds reports categories in. It is an
// array rather than a slice so that no caller can reach in and reorder the
// package's own notion of that order.
var factorKinds = [3]FactorKind{FactorKnowledge, FactorPossession, FactorInherence}

// recognised reports whether k is one of the three categories this package
// derives assurance from.
func (k FactorKind) recognised() bool {
	switch k {
	case FactorKnowledge, FactorPossession, FactorInherence:
		return true
	default:
		return false
	}
}

// Factor is one authentication factor that was actually exercised.
type Factor struct {
	// Kind is the category the factor belongs to. Assurance is derived from
	// the set of distinct kinds, never from the number of factors.
	Kind FactorKind

	// Method names the concrete mechanism — "password", "totp",
	// "lookup_secret", "webauthn". It is free-form on purpose: it exists for
	// audit records and for an operator reading a log line, and no decision in
	// this package is made by matching on it. Inferring a property from a
	// method name is exactly the guess this type is shaped to prevent.
	Method string
}

// Authentication is what a credential verification established: which
// principal was authenticated, by which factors, and what the verifier is
// willing to assert about how.
//
// It is a record of what happened, not a request for what should happen. A
// caller fills it in from the exchange it observed and nothing else.
type Authentication struct {
	// Subject names the principal that was authenticated. It becomes the
	// context's subject claim, so it must be the identifier a consumer will
	// authorize against.
	Subject string

	// Factors are the factors actually exercised, in whatever order the
	// verifier recorded them. Duplicated categories are permitted here — a
	// password and a security question are two genuine factors — and are
	// collapsed by Kinds when assurance is derived.
	Factors []Factor

	// PhishingResistant asserts that the exchange resisted verifier
	// impersonation: the authenticator was bound to the relying party, so a
	// principal could not have been induced to complete it against an
	// attacker-controlled site.
	//
	// It is an assertion by the verifier that saw the exchange, never an
	// inference from a Method name. The same named mechanism can be deployed
	// in a configuration that has this property and one that does not, so a
	// verifier that cannot vouch for it leaves it false.
	PhishingResistant bool

	// HardwareBacked asserts that the authenticator's key material is resident
	// in hardware and cannot be exported from it.
	//
	// The same caution applies: it is asserted from what the verifier knows
	// about the authenticator, never guessed from what the factor is called.
	HardwareBacked bool
}

// Rejected authentications. Each is returned wrapped where an index or a
// category makes the failure easier to place, and each is separately matchable
// with errors.Is: these surface to the code assembling the authentication, not
// across a trust boundary, so there is no authorization oracle to protect by
// collapsing them into one.
var (
	// ErrNilAuthentication reports Validate called on a nil authentication,
	// which is distinct from an empty one: nothing was recorded at all.
	ErrNilAuthentication = errors.New("identity: authentication is nil")

	// ErrMissingAuthenticatedSubject reports an authentication that names no
	// principal. There is nobody to issue a context for.
	ErrMissingAuthenticatedSubject = errors.New("identity: authentication has no subject")

	// ErrNoFactors reports an authentication that exercised no factor. An
	// authentication with no factor is not a weak authentication, it is not an
	// authentication, and it must not be allowed to reach the assurance ladder
	// and come back with a level attached.
	ErrNoFactors = errors.New("identity: authentication exercised no factor")

	// ErrUnknownFactorKind reports a factor whose category is empty or outside
	// the three recognised ones. It is rejected rather than ignored: a
	// category this package does not know is one the assurance derivation was
	// never written with in mind, and silently dropping it would understate
	// the authentication while silently counting it would overstate it.
	ErrUnknownFactorKind = errors.New("identity: unrecognised authentication factor category")

	// ErrMissingFactorMethod reports a factor that names no mechanism. The
	// category alone is not enough for the audit record the factor exists to
	// produce.
	ErrMissingFactorMethod = errors.New("identity: authentication factor has no method")
)

// Validate reports whether the authentication is well formed enough to issue a
// context from.
//
// It checks that a principal is named, that at least one factor was exercised,
// and that every factor names a recognised category and a mechanism. It says
// nothing about whether the authentication was strong — that is the assurance
// derivation's job, and it is deliberately a separate question.
func (a *Authentication) Validate() error {
	if a == nil {
		return ErrNilAuthentication
	}
	if a.Subject == "" {
		return ErrMissingAuthenticatedSubject
	}
	if len(a.Factors) == 0 {
		return ErrNoFactors
	}
	for i, factor := range a.Factors {
		if !factor.Kind.recognised() {
			return fmt.Errorf("%w: factor %d has category %q", ErrUnknownFactorKind, i, factor.Kind)
		}
		if factor.Method == "" {
			return fmt.Errorf("%w: factor %d is a %s factor", ErrMissingFactorMethod, i, factor.Kind)
		}
	}
	return nil
}

// Kinds returns the distinct categories present, in a fixed canonical order.
//
// This is the function assurance is derived from, and the deduplication
// happens here so that it happens once. A password and a security question
// yield one category, not two, because the second is another prompt rather
// than another line of defence.
//
// The order does not depend on the order the verifier happened to record
// factors in, so the same authentication described two ways yields one answer
// — which matters as soon as a caller compares or logs the result.
//
// Only recognised categories are reported. An empty or unrecognised category
// contributes nothing, so it can never inflate a level even if a caller
// derives assurance without validating first; Validate is what rejects it
// outright. The returned slice is freshly allocated and shares nothing with
// the authentication.
func (a *Authentication) Kinds() []FactorKind {
	if a == nil || len(a.Factors) == 0 {
		return nil
	}

	present := make(map[FactorKind]struct{}, len(a.Factors))
	for _, factor := range a.Factors {
		if factor.Kind.recognised() {
			present[factor.Kind] = struct{}{}
		}
	}
	if len(present) == 0 {
		return nil
	}

	kinds := make([]FactorKind, 0, len(present))
	for _, kind := range factorKinds {
		if _, ok := present[kind]; ok {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// CredentialVerifier checks a credential and reports what that established.
//
// It is generic over C, the credential type, because what a principal presents
// is the consumer's own business: an identifier and a password, a bearer
// assertion, a signed challenge. Keeping it a type parameter means the shape
// is checked where the call is made, rather than every implementation opening
// with a type assertion that fails at run time when the wiring is wrong.
//
// The seam says nothing about where credentials are stored, what schema they
// have, or what the principal is a member of. An implementation answers from
// whatever it already has.
//
// An implementation returns a non-nil error for any credential it does not
// accept, and an Authentication describing only what it actually verified.
type CredentialVerifier[C any] interface {
	VerifyCredential(ctx context.Context, credential C) (*Authentication, error)
}

// ClaimsResolver supplies the claims that are true of a subject regardless of
// how it authenticated.
//
// The split from CredentialVerifier is deliberate: authentication establishes
// who the principal is, and this establishes what they are entitled to. The
// two are usually answered by different systems, and an implementation of one
// should not be forced to know about the other.
type ClaimsResolver interface {
	ResolveClaims(ctx context.Context, subject string) (Grant, error)
}

// Grant is what a resolver returns: exactly the claims a context needs beyond
// the subject and the assurance level.
//
// It is this narrow on purpose. Every field added here becomes a concept every
// consumer has to have an answer for, and this package has no business
// deciding that a consumer models companies, roles or sessions at all.
type Grant struct {
	// Tenant is the tenant slug the principal is acting within. It is
	// human-readable and stable; an empty value is not "every tenant", it is a
	// context that cannot be scoped.
	Tenant string

	// Entitlements are the entitlement strings resolved for the principal. An
	// empty list is a legitimate answer — a principal may hold none — and is
	// distinct from a resolver that could not answer, which returns an error.
	Entitlements []string
}
