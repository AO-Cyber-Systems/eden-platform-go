package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// Misconfiguration reported by NewIssuer, in addition to the sentinels the
// minter already defines for a missing signer, key id or issuer. These surface
// at process start, to an operator who has to know which of several settings is
// wrong, so each is separately matchable with errors.Is.
var (
	// ErrNilCredentialVerifier reports an issuer constructed without a
	// credential verifier. There is no built-in one: this package cannot
	// authenticate anybody, and an issuer that could mint without consulting a
	// verifier would be an issuer that mints for anybody.
	ErrNilCredentialVerifier = errors.New("identity: issuer has no credential verifier")

	// ErrNilClaimsResolver reports an issuer constructed without a claims
	// resolver. A context carries a tenant it cannot be minted without, and
	// this package has no way to invent one.
	ErrNilClaimsResolver = errors.New("identity: issuer has no claims resolver")

	// ErrSignerUnhealthy reports a signer that failed its health check at
	// construction. It wraps the underlying cause, which is what names the
	// actual denial.
	ErrSignerUnhealthy = errors.New("identity: signer failed its health check")
)

// ErrVerifierReportedNoAuthentication reports a credential verifier that
// returned neither an authentication nor an error.
//
// It is distinct from ErrNilAuthentication, which reports a nil authentication
// handed to Validate: this one says the verifier itself is broken. The
// distinction is worth keeping because the remedies differ — one is a
// malformed record, the other is an implementation that has to be fixed — and
// because the failure mode is severe. A verifier answering (nil, nil) has
// established nothing about anybody, and reading that as success would mint a
// context for nobody.
var ErrVerifierReportedNoAuthentication = errors.New("identity: credential verifier reported success without an authentication")

// Issuer turns a credential into a signed identity context.
//
// It composes three things it does not implement: a CredentialVerifier that
// establishes who the principal is, a ClaimsResolver that establishes what they
// are entitled to, and a Minter that states the result and signs it. The
// assurance level is derived here, from the factors the verifier reported.
//
// It is generic over C, the credential type, for the same reason the credential
// seam is: what a principal presents is the consumer's own business, and the
// shape is checked where the call is made rather than at run time inside an
// implementation.
//
// An Issuer is immutable after construction and safe for concurrent use, so a
// process shares one across every request path. It is only as concurrency-safe
// as the seams it was given.
type Issuer[C any] struct {
	verifier CredentialVerifier[C]
	resolver ClaimsResolver
	minter   *Minter
}

// NewIssuer returns an Issuer that mints contexts for issuer, signing with
// signer and authenticating through verifier and resolver.
//
// # The key id is not a parameter
//
// This is deliberate, and it is enforced by the signature rather than by this
// comment. The key id stamped in every kid header is signer.KeyID(), which the
// key surface derives from the key itself — an ARN, a key URL, a label, a
// generated identifier.
//
// The failure that prevents: a deployment pins a constant key id and later
// replaces the key material behind it. A consumer's key cache only re-fetches
// on an id it does not recognise, so a constant id that now names different
// material never misses, and every request is refused indefinitely with no
// cache miss to trigger recovery. An id derived from the key changes when the
// key does, which produces exactly the miss that heals. A caller cannot pin a
// constant here because there is nowhere to pass one.
//
// An empty KeyID() is refused: it would mint contexts no verifier can resolve a
// key for.
//
// # Signing is proved before the issuer exists
//
// NewIssuer runs signer.HealthCheck and refuses to construct if it fails. That
// check performs a real sign-and-verify round trip, and it is run here because
// reading a public key and signing with it are separately authorized operations
// on a hosted key service: a configuration that permits the first while denying
// the second looks perfectly healthy until the first login attempt. Failing at
// construction turns that into a start-up failure an operator sees immediately
// rather than an authentication outage a user discovers.
//
// ctx bounds the health check only; it is not retained. Every dependency is
// required, and each omission is reported distinctly. Options are forwarded to
// the minter, so an unusable one is reported here rather than at the first
// issue.
func NewIssuer[C any](
	ctx context.Context,
	signer kms.KMSSigner,
	issuer string,
	verifier CredentialVerifier[C],
	resolver ClaimsResolver,
	opts ...MinterOption,
) (*Issuer[C], error) {
	if verifier == nil {
		return nil, ErrNilCredentialVerifier
	}
	if resolver == nil {
		return nil, ErrNilClaimsResolver
	}
	if signer == nil {
		return nil, ErrNilSigner
	}
	if issuer == "" {
		return nil, ErrMissingIssuer
	}

	// Read once and use the same value for the check and for the minter, so
	// there is no window in which a signer could report one id here and
	// another when the header is stamped.
	keyID := signer.KeyID()
	if keyID == "" {
		return nil, ErrMissingKeyID
	}

	// Before anything else is built: prove this process can actually sign.
	if err := signer.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignerUnhealthy, err)
	}

	// kms.KMSSigner is a superset of the minter's signing seam — it adds
	// KeyID and HealthCheck — so it satisfies that seam directly and needs no
	// adapter.
	minter, err := NewMinter(signer, keyID, issuer, opts...)
	if err != nil {
		return nil, err
	}

	return &Issuer[C]{verifier: verifier, resolver: resolver, minter: minter}, nil
}

// Issue authenticates credential and returns a signed context for the principal
// it established, or an error and an empty token.
//
// # The order is the security property
//
// The steps below run in exactly this order and stop at the first failure. The
// order is not a style choice, and the tests assert it rather than merely
// asserting the outcomes:
//
//  1. Verify the credential. On an error — or on a nil authentication, which is
//     a broken verifier — return without going any further.
//
//  2. Validate the authentication. A verifier reporting success with no subject
//     or no factor has not established anything mintable. DeriveAssurance
//     validates too, but the check is made explicitly here so that this gate
//     does not depend on another function's internals to hold.
//
//  3. Derive the assurance level from the factors that were actually exercised.
//     It is not a parameter, not a field on this type, and not a default: a
//     level nobody achieved is indistinguishable, downstream, from one that was.
//
//  4. Resolve the claims — and only now. Resolving entitlements for an
//     identifier that has not authenticated is a disclosure even when nothing
//     is minted afterwards, because it answers "what is this principal entitled
//     to" for a caller who has proved nothing.
//
//  5. Mint, taking the subject from the AUTHENTICATION. The credential says who
//     the caller claims to be; the authentication says who they proved to be,
//     and only the latter belongs in a context.
//
// ctx is passed to both seams, so a caller's cancellation and deadline reach
// whatever they talk to.
//
// On any error the returned token is empty, so a caller never has to decide
// whether a token accompanying an error is safe to use.
func (i *Issuer[C]) Issue(ctx context.Context, credential C) (string, error) {
	// 1. Authenticate. Nothing else happens until this succeeds.
	auth, err := i.verifier.VerifyCredential(ctx, credential)
	if err != nil {
		return "", fmt.Errorf("identity: issue: verify credential: %w", err)
	}
	if auth == nil {
		return "", ErrVerifierReportedNoAuthentication
	}

	// 2. The record has to be well formed before anything is read off it.
	if err := auth.Validate(); err != nil {
		return "", fmt.Errorf("identity: issue: %w", err)
	}

	// 3. The level comes from what was exercised, never from a caller.
	assurance, err := DeriveAssurance(auth)
	if err != nil {
		return "", fmt.Errorf("identity: issue: derive assurance: %w", err)
	}

	// 4. Only a subject that actually authenticated gets looked up.
	grant, err := i.resolver.ResolveClaims(ctx, auth.Subject)
	if err != nil {
		return "", fmt.Errorf("identity: issue: resolve claims: %w", err)
	}

	// 5. The subject is the one that was proved, not the one that was claimed.
	token, err := i.minter.Mint(MintInput{
		Subject:      auth.Subject,
		Tenant:       grant.Tenant,
		Entitlements: grant.Entitlements,
		Assurance:    assurance,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}
