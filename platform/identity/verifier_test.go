package identity

import (
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth/kmssigner"
	"github.com/golang-jwt/jwt/v5"
)

// Two issuers, because the verifier is plural from day one and a suite built
// around a single issuer cannot tell "trusts this issuer" apart from "trusts
// anything". Neither name is a default: every issuer a verifier trusts is
// supplied by the test that builds it, exactly as an operator supplies it.
const (
	issuerAName = "https://issuer-a.example/"
	issuerBName = "https://issuer-b.example/"
	issuerCName = "https://issuer-c.example/"
	issuerAKID  = "context-key-a"
	issuerBKID  = "context-key-b"

	// sharedKID is the same key id published by two different issuers, which
	// is what makes the cross-issuer confusion case a test of the ISSUER
	// binding rather than an accidental test of key-id lookup.
	sharedKID = "context-key-shared"
)

// verifierCtxKey marks the caller's context so a key source can prove it was
// handed that context and not a fresh one.
type verifierCtxKey struct{}

// issuerFixture is one issuer: its name, its signing key, the key id it
// publishes, and the key source a verifier resolves that key id through.
type issuerFixture struct {
	issuer string
	kid    string
	signer kmssigner.Signer
	keys   *StaticKeys
}

func newIssuerFixture(t *testing.T, issuer, kid string) *issuerFixture {
	t.Helper()
	return newIssuerFixtureWithSigner(t, issuer, kid, newES256Signer(t))
}

func newIssuerFixtureWithSigner(t *testing.T, issuer, kid string, signer kmssigner.Signer) *issuerFixture {
	t.Helper()

	return &issuerFixture{
		issuer: issuer,
		kid:    kid,
		signer: signer,
		keys:   NewStaticKeys(map[string]crypto.PublicKey{kid: signer.Public()}),
	}
}

func (f *issuerFixture) trusted() TrustedIssuer {
	return TrustedIssuer{Issuer: f.issuer, Keys: f.keys}
}

// mint produces a context the ordinary way, through the minter, so the happy
// path is exercised against a token built by the shipped issuing path rather
// than one assembled by the test.
func (f *issuerFixture) mint(t *testing.T, opts ...MinterOption) string {
	t.Helper()

	minter, err := NewMinter(f.signer, f.kid, f.issuer, opts...)
	if err != nil {
		t.Fatalf("NewMinter returned error: %v", err)
	}
	return mustMint(t, minter, validInput())
}

// mintWithLifetime mints a context issued at issuedAt that lives for ttl, which
// is how the expiry cases place a token's exp precisely either side of the
// verifier's leeway without sleeping.
func (f *issuerFixture) mintWithLifetime(t *testing.T, issuedAt time.Time, ttl time.Duration) string {
	t.Helper()

	return f.mint(t, WithTTL(ttl), WithClock(func() time.Time { return issuedAt }))
}

// sign signs an arbitrary claim set with this issuer's key, stamping the
// headers a context carries. adjust runs last, so a case can corrupt or remove
// a header the minter would always set correctly.
func (f *issuerFixture) sign(t *testing.T, claims *Claims, adjust ...func(map[string]any)) string {
	t.Helper()

	method, err := signingMethodFor(f.signer.SigningAlgorithm())
	if err != nil {
		t.Fatalf("signingMethodFor(%q) returned error: %v", f.signer.SigningAlgorithm(), err)
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["typ"] = JWTType
	token.Header["kid"] = f.kid
	for _, a := range adjust {
		a(token.Header)
	}

	signed, err := token.SignedString(f.signer)
	if err != nil {
		t.Fatalf("SignedString returned error: %v", err)
	}
	return signed
}

// contextFor is a complete, currently valid claim set at the given schema
// version: the shape individual cases then degrade one field at a time.
func contextFor(issuer string, version int) *Claims {
	now := time.Now()
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "principal-0001",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
		Tenant:       "acme",
		Entitlements: []string{"reports.read"},
		AAL:          AAL2,
		CtxVer:       version,
	}
}

func trustedSet(fixtures ...*issuerFixture) []TrustedIssuer {
	trusted := make([]TrustedIssuer, 0, len(fixtures))
	for _, f := range fixtures {
		trusted = append(trusted, f.trusted())
	}
	return trusted
}

func newTestVerifier(t *testing.T, trusted []TrustedIssuer, opts ...VerifierOption) *Verifier {
	t.Helper()

	v, err := NewVerifier(trusted, opts...)
	if err != nil {
		t.Fatalf("NewVerifier returned error: %v", err)
	}
	return v
}

// wantRejected asserts a token was refused, that the refusal carries no
// information beyond "not valid", and that no claims came back alongside it.
//
// The identity comparison is the point: an error that merely satisfies
// errors.Is(err, ErrInvalidContext) may still have been produced by wrapping
// the real cause, and a wrapped cause is readable again with errors.Is. The
// returned error must BE the sentinel.
func wantRejected(t *testing.T, v *Verifier, token, when string) error {
	t.Helper()

	claims, err := v.Verify(context.Background(), token)
	if err == nil {
		t.Fatalf("Verify(%s) returned no error, want a rejection", when)
	}
	if claims != nil {
		t.Errorf("Verify(%s) returned claims %+v alongside an error, want nil claims", when, claims)
	}
	if !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Verify(%s) error = %v, want errors.Is(err, ErrInvalidContext)", when, err)
	}
	if err != ErrInvalidContext { //nolint:errorlint // identity is precisely the assertion
		t.Fatalf("Verify(%s) error = %v (%T), want exactly ErrInvalidContext, unwrapped: a wrapped cause stays readable with errors.Is and is an oracle", when, err, err)
	}
	return err
}

func wantAccepted(t *testing.T, v *Verifier, token, when string) *Claims {
	t.Helper()

	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify(%s) returned error: %v, want acceptance", when, err)
	}
	if claims == nil {
		t.Fatalf("Verify(%s) returned nil claims and a nil error", when)
	}
	return claims
}

// --- construction -----------------------------------------------------------

// Nothing about which issuer is trusted is compiled in, so a verifier built
// with no trusted issuers is a misconfiguration rather than a verifier that
// falls back to some built-in default.
func TestNewVerifierTrustsNothingByDefault(t *testing.T) {
	for _, trusted := range [][]TrustedIssuer{nil, {}} {
		v, err := NewVerifier(trusted)
		if !errors.Is(err, ErrNoTrustedIssuers) {
			t.Errorf("NewVerifier(%v) error = %v, want ErrNoTrustedIssuers", trusted, err)
		}
		if v != nil {
			t.Errorf("NewVerifier(%v) returned a verifier alongside an error, want nil", trusted)
		}
	}
}

func TestNewVerifierRejectsAnEmptyIssuerName(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	_, err := NewVerifier([]TrustedIssuer{{Issuer: "", Keys: a.keys}})
	if !errors.Is(err, ErrMissingTrustedIssuer) {
		t.Errorf("NewVerifier(empty issuer) error = %v, want ErrMissingTrustedIssuer", err)
	}
}

func TestNewVerifierRejectsANilKeySource(t *testing.T) {
	_, err := NewVerifier([]TrustedIssuer{{Issuer: issuerAName, Keys: nil}})
	if !errors.Is(err, ErrNilKeySource) {
		t.Errorf("NewVerifier(nil key source) error = %v, want ErrNilKeySource", err)
	}
}

// A repeated issuer means two key sources disagree about who signs for it.
// Silently keeping one of them makes the trust set depend on slice order.
func TestNewVerifierRejectsADuplicateIssuer(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	other := newIssuerFixture(t, issuerAName, issuerBKID)

	_, err := NewVerifier(trustedSet(a, other))
	if !errors.Is(err, ErrDuplicateTrustedIssuer) {
		t.Errorf("NewVerifier(duplicate issuer) error = %v, want ErrDuplicateTrustedIssuer", err)
	}
}

// A verifier that accepts no schema version rejects every context it is ever
// shown. That is a configuration mistake worth reporting at start-up rather
// than as a total outage discovered in traffic.
func TestNewVerifierRejectsAnEmptyAcceptedVersionSet(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	_, err := NewVerifier(trustedSet(a), WithAcceptedVersions(NewVersionSet()))
	if !errors.Is(err, ErrNoAcceptedVersions) {
		t.Errorf("NewVerifier(empty version set) error = %v, want ErrNoAcceptedVersions", err)
	}
}

// Construction failures are the operator's, not a caller's: they surface at
// process start to someone who has to know which setting is wrong. They are
// deliberately distinguishable, unlike every failure Verify reports.
func TestNewVerifierRejectionsAreDistinguishable(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	distinct := []error{
		ErrNoTrustedIssuers,
		ErrMissingTrustedIssuer,
		ErrNilKeySource,
		ErrDuplicateTrustedIssuer,
		ErrNoAcceptedVersions,
	}
	for i, outer := range distinct {
		for j, inner := range distinct {
			if i != j && errors.Is(outer, inner) {
				t.Errorf("construction error %v matches %v; each must be separately identifiable", outer, inner)
			}
		}
		if errors.Is(outer, ErrInvalidContext) {
			t.Errorf("construction error %v matches ErrInvalidContext; misconfiguration is not a rejected token", outer)
		}
	}

	if _, err := NewVerifier(trustedSet(a)); err != nil {
		t.Errorf("NewVerifier(valid) returned error: %v", err)
	}
}

// The accepted set defaults to the package default rather than to "anything",
// so a caller that never thought about version negotiation gets the narrow set.
func TestNewVerifierDefaultsToTheDefaultVersionSet(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	claims := wantAccepted(t, v, a.sign(t, contextFor(a.issuer, Version)), "the emitted version")
	if claims.CtxVer != Version {
		t.Errorf("accepted context CtxVer = %d, want %d", claims.CtxVer, Version)
	}

	for present := range DefaultVersionSet() {
		if present != Version {
			t.Fatalf("DefaultVersionSet contains %d; this test assumes exactly {%d}", present, Version)
		}
	}
	wantRejected(t, v, a.sign(t, contextFor(a.issuer, Version+1)), "a version outside the default set")
}

// A trust set the caller can still write to after construction is a trust set
// some unrelated code path can widen at run time.
func TestNewVerifierDoesNotAliasTheCallerSlice(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	b := newIssuerFixture(t, issuerBName, issuerBKID)

	trusted := trustedSet(a)
	v := newTestVerifier(t, trusted)

	trusted[0] = b.trusted()

	wantAccepted(t, v, a.mint(t), "the issuer trusted at construction")
	wantRejected(t, v, b.mint(t), "an issuer written into the caller slice afterwards")
}

// --- the happy path ---------------------------------------------------------

func TestVerifyAcceptsAContextFromATrustedIssuer(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	claims := wantAccepted(t, v, a.mint(t), "a freshly minted context")

	in := validInput()
	if claims.Issuer != a.issuer {
		t.Errorf("claims.Issuer = %q, want %q", claims.Issuer, a.issuer)
	}
	if claims.Subject != in.Subject {
		t.Errorf("claims.Subject = %q, want %q", claims.Subject, in.Subject)
	}
	if claims.Tenant != in.Tenant {
		t.Errorf("claims.Tenant = %q, want %q", claims.Tenant, in.Tenant)
	}
	if claims.AAL != in.Assurance {
		t.Errorf("claims.AAL = %q, want %q", claims.AAL, in.Assurance)
	}
	if claims.CtxVer != Version {
		t.Errorf("claims.CtxVer = %d, want %d", claims.CtxVer, Version)
	}
	if len(claims.Entitlements) != len(in.Entitlements) {
		t.Fatalf("claims.Entitlements = %v, want %v", claims.Entitlements, in.Entitlements)
	}
	for i, ent := range in.Entitlements {
		if claims.Entitlements[i] != ent {
			t.Errorf("claims.Entitlements[%d] = %q, want %q", i, claims.Entitlements[i], ent)
		}
	}
}

// Both algorithms the verifier admits must actually verify, or the valid-method
// list is narrower in practice than it claims to be.
func TestVerifyAcceptsBothAdmittedSigningAlgorithms(t *testing.T) {
	es256 := newIssuerFixture(t, issuerAName, issuerAKID)
	rs256 := newIssuerFixtureWithSigner(t, issuerBName, issuerBKID, newRS256Signer(t))
	v := newTestVerifier(t, trustedSet(es256, rs256))

	for _, f := range []*issuerFixture{es256, rs256} {
		alg := f.signer.SigningAlgorithm()
		if claims := wantAccepted(t, v, f.mint(t), alg); claims.Issuer != f.issuer {
			t.Errorf("%s: claims.Issuer = %q, want %q", alg, claims.Issuer, f.issuer)
		}
	}
}

// The plural trust set is the reason this package exists: one verifier serves
// an in-process issuer and an external one at the same time.
func TestVerifyAcceptsAContextFromEachOfTwoTrustedIssuers(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	b := newIssuerFixture(t, issuerBName, issuerBKID)
	v := newTestVerifier(t, trustedSet(a, b))

	for _, f := range []*issuerFixture{a, b} {
		if claims := wantAccepted(t, v, f.mint(t), f.issuer); claims.Issuer != f.issuer {
			t.Errorf("claims.Issuer = %q, want %q", claims.Issuer, f.issuer)
		}
	}
}

// --- the issuer binding -----------------------------------------------------

func TestVerifyRejectsAnUntrustedIssuer(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	stranger := newIssuerFixture(t, issuerBName, issuerBKID)
	v := newTestVerifier(t, trustedSet(a))

	wantRejected(t, v, stranger.mint(t), "a correctly signed context from an issuer nobody trusts")
}

func TestVerifyRejectsAContextWithNoIssuer(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	wantRejected(t, v, a.sign(t, contextFor("", Version)), "a context claiming no issuer")
}

// An untrusted issuer is refused before any key is resolved and any signature
// is checked. Reading iss before the signature is verified is only safe because
// the value selects a key set and does nothing else; doing key work for an
// issuer outside the trust set would make that read do something else.
func TestVerifyRejectsAnUntrustedIssuerBeforeAnyKeyWork(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	stranger := newIssuerFixture(t, issuerBName, issuerBKID)

	recorder := &recordingKeySource{inner: a.keys}
	v := newTestVerifier(t, []TrustedIssuer{{Issuer: a.issuer, Keys: recorder}})

	wantRejected(t, v, stranger.mint(t), "an untrusted issuer")

	if calls := recorder.calls(); len(calls) != 0 {
		t.Errorf("key source consulted %v for an untrusted issuer, want no key work at all", calls)
	}
}

// The cross-issuer confusion case. Two trusted issuers publish the SAME key id
// with DIFFERENT keys. A token signed by A's key but claiming iss B is checked
// against B's keys, because iss selects the key set — so it fails to verify.
//
// This is what stops a forged iss from being useful: it can only ever select a
// key set that will refuse the signature.
func TestVerifyRejectsATokenSignedByAnotherIssuersKey(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, sharedKID)
	b := newIssuerFixture(t, issuerBName, sharedKID)
	v := newTestVerifier(t, trustedSet(a, b))

	// Precondition: both issuers are genuinely trusted, so the rejection below
	// is about the signature binding and not about an issuer being unknown.
	wantAccepted(t, v, a.mint(t), "issuer A signing for itself")
	wantAccepted(t, v, b.mint(t), "issuer B signing for itself")

	forged := a.sign(t, contextFor(b.issuer, Version))
	wantRejected(t, v, forged, "signed by A's key, claiming to be from B")
}

// The same confusion where the issuers do not share a key id: the key id is not
// in the claimed issuer's key set at all.
func TestVerifyRejectsATokenWhoseKeyIDBelongsToAnotherIssuer(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	b := newIssuerFixture(t, issuerBName, issuerBKID)
	v := newTestVerifier(t, trustedSet(a, b))

	forged := a.sign(t, contextFor(b.issuer, Version))
	wantRejected(t, v, forged, "A's key id presented as a context from B")
}

func TestVerifyRejectsAnUnknownKeyID(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	cases := map[string]func(map[string]any){
		"a key id the issuer does not publish": func(h map[string]any) { h["kid"] = "context-key-retired" },
		"an empty key id":                      func(h map[string]any) { h["kid"] = "" },
		"no key id at all":                     func(h map[string]any) { delete(h, "kid") },
		"a non-string key id":                  func(h map[string]any) { h["kid"] = 1 },
	}
	for name, adjust := range cases {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, v, a.sign(t, contextFor(a.issuer, Version), adjust), name)
		})
	}
}

// The key is resolved by the key id the token names, never by "the only key"
// or "the first key": a verifier that falls back is a verifier an attacker
// chooses the key for.
func TestVerifyResolvesTheKeyTheTokenNames(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	recorder := &recordingKeySource{inner: a.keys}
	v := newTestVerifier(t, []TrustedIssuer{{Issuer: a.issuer, Keys: recorder}})

	wantAccepted(t, v, a.mint(t), "a context naming a published key id")

	calls := recorder.calls()
	if len(calls) != 1 || calls[0] != a.kid {
		t.Errorf("key source consulted %v, want exactly [%q]", calls, a.kid)
	}
}

// --- algorithm and type -----------------------------------------------------

// An unsigned token is a token whose signature check the presenter chose to
// remove. The valid-method list is what refuses it.
func TestVerifyRejectsTheNoneAlgorithm(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	token := jwt.NewWithClaims(jwt.SigningMethodNone, contextFor(a.issuer, Version))
	token.Header["typ"] = JWTType
	token.Header["kid"] = a.kid
	unsigned, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString returned error: %v", err)
	}

	wantRejected(t, v, unsigned, "a context with alg none")
}

// The algorithm-confusion attack: the issuer's PUBLIC key is published, so an
// attacker can use its encoding as an HMAC secret and present a token the
// verifier would happily check with the very key it just resolved. Pinning the
// admitted algorithms is what refuses it.
func TestVerifyRejectsAnHMACSignedContext(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	forged := hmacForgery(t, a, contextFor(a.issuer, Version))
	wantRejected(t, v, forged, "a context HMAC-signed with the issuer's published public key")
}

// The type header is what stops any other token the issuer signs — an access
// token above all — from being replayed into the identity-context slot. It is
// not checked by the parser, so a verifier that does not assert it inherits the
// issuer's entire token surface.
func TestVerifyRejectsAContextWithoutTheContextTokenType(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	cases := map[string]func(map[string]any){
		"the library default type":         func(h map[string]any) { h["typ"] = "JWT" },
		"an access token type":             func(h map[string]any) { h["typ"] = "at+jwt" },
		"no type header at all":            func(h map[string]any) { delete(h, "typ") },
		"a non-string type":                func(h map[string]any) { h["typ"] = 1 },
		"the right type in the wrong case": func(h map[string]any) { h["typ"] = "Identity-Context+JWT" },
	}
	for name, adjust := range cases {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, v, a.sign(t, contextFor(a.issuer, Version), adjust), name)
		})
	}
}

// --- time -------------------------------------------------------------------

func TestVerifyRejectsAContextExpiredBeyondTheLeeway(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	issued := time.Now().Add(-time.Hour)
	wantRejected(t, v, a.mintWithLifetime(t, issued, time.Minute), "a context that expired an hour ago")
}

// Leeway exists because clocks between two hosts disagree by small amounts, and
// a context rejected for a two-second skew is an outage nobody can reproduce.
func TestVerifyAcceptsAContextExpiredWithinTheLeeway(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	ttl := time.Minute
	issued := time.Now().Add(-ttl - 2*time.Second)
	wantAccepted(t, v, a.mintWithLifetime(t, issued, ttl), "a context two seconds past expiry")
}

func TestVerifyRejectsAContextNotYetValidBeyondTheLeeway(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	claims := contextFor(a.issuer, Version)
	claims.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour))
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(2 * time.Hour))

	wantRejected(t, v, a.sign(t, claims), "a context that becomes valid in an hour")
}

func TestVerifyAcceptsAContextNotYetValidWithinTheLeeway(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	claims := contextFor(a.issuer, Version)
	claims.NotBefore = jwt.NewNumericDate(time.Now().Add(2 * time.Second))

	wantAccepted(t, v, a.sign(t, claims), "a context valid two seconds from now")
}

// --- semantic validation ----------------------------------------------------

// Verify delegates the semantic rules to Claims.Validate rather than
// re-implementing them, which is what keeps the two from drifting apart.
func TestVerifyRejectsAContextMissingTheClaimsAuthorizationNeeds(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	cases := map[string]func(*Claims){
		"no subject":   func(c *Claims) { c.Subject = "" },
		"no tenant":    func(c *Claims) { c.Tenant = "" },
		"no assurance": func(c *Claims) { c.AAL = "" },
	}
	for name, degrade := range cases {
		t.Run(name, func(t *testing.T) {
			claims := contextFor(a.issuer, Version)
			degrade(claims)
			wantRejected(t, v, a.sign(t, claims), name)
		})
	}
}

func TestVerifyRejectsAVersionOutsideTheAcceptedSet(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a), WithAcceptedVersions(NewVersionSet(Version)))

	for _, version := range []int{0, Version + 1, 99} {
		wantRejected(t, v, a.sign(t, contextFor(a.issuer, version)), "a context at an unaccepted version")
	}
}

// The version-negotiation proof: ONE token, two verifiers, opposite outcomes.
// The accepted set is per-verifier configuration, so widening one deployment
// does not widen another and does not depend on any package-level default.
func TestVerifyVersionNegotiationIsPerVerifier(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	next := Version + 1
	token := a.sign(t, contextFor(a.issuer, next))

	widened := newTestVerifier(t, trustedSet(a), WithAcceptedVersions(NewVersionSet(Version, next)))
	narrow := newTestVerifier(t, trustedSet(a), WithAcceptedVersions(NewVersionSet(Version)))

	if claims := wantAccepted(t, widened, token, "a widened verifier"); claims.CtxVer != next {
		t.Errorf("claims.CtxVer = %d, want %d", claims.CtxVer, next)
	}
	wantRejected(t, narrow, token, "the same token at a narrow verifier")

	// The narrow verifier still takes the emitted version, so the rejection
	// above is about the version and not about the verifier being broken.
	wantAccepted(t, narrow, a.sign(t, contextFor(a.issuer, Version)), "the emitted version at the narrow verifier")
}

// --- one uniform rejection --------------------------------------------------

// Claims.Validate reports NAMED, WRAPPED reasons on purpose, for a caller that
// wants them. A verifier is not that caller: propagating one of those errors,
// or wrapping it with %w, republishes the reason through errors.Is and turns
// the verifier into a per-reason oracle.
func TestVerifyDoesNotLeakTheSemanticRejectionReason(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	cases := []struct {
		name    string
		leaked  error
		degrade func(*Claims)
	}{
		{"unsupported version", ErrUnsupportedVersion, func(c *Claims) { c.CtxVer = Version + 1 }},
		{"missing subject", ErrMissingSubject, func(c *Claims) { c.Subject = "" }},
		{"missing tenant", ErrMissingTenant, func(c *Claims) { c.Tenant = "" }},
		{"missing assurance", ErrMissingAssurance, func(c *Claims) { c.AAL = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := contextFor(a.issuer, Version)
			tc.degrade(claims)

			// Without this the case would pass vacuously if Claims.Validate
			// ever stopped reporting the named reason.
			if err := claims.Validate(DefaultVersionSet()); !errors.Is(err, tc.leaked) {
				t.Fatalf("precondition: Claims.Validate = %v, want errors.Is(err, %v)", err, tc.leaked)
			}

			err := wantRejected(t, v, a.sign(t, claims), tc.name)
			if errors.Is(err, tc.leaked) {
				t.Errorf("Verify error satisfies errors.Is(err, %v): the rejection reason reached the caller", tc.leaked)
			}
		})
	}
}

// The same discipline for the key-resolution seam: whether a key id is unknown,
// and whether an issuer's key set could be reached at all, are operator facts.
func TestVerifyDoesNotLeakTheKeyResolutionReason(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	cases := []struct {
		name   string
		source KeySource
		leaked error
	}{
		{"unknown key id", a.keys, ErrUnknownKeyID},
		{"key set unreachable", &recordingKeySource{inner: a.keys, err: ErrJWKSUnavailable}, ErrJWKSUnavailable},
		{"key set malformed", &recordingKeySource{inner: a.keys, err: ErrJWKSMalformed}, ErrJWKSMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newTestVerifier(t, []TrustedIssuer{{Issuer: a.issuer, Keys: tc.source}})

			token := a.sign(t, contextFor(a.issuer, Version), func(h map[string]any) {
				h["kid"] = "context-key-retired"
			})

			err := wantRejected(t, v, token, tc.name)
			if errors.Is(err, tc.leaked) {
				t.Errorf("Verify error satisfies errors.Is(err, %v): the key resolution reason reached the caller", tc.leaked)
			}
		})
	}
}

// The library's own error taxonomy is just as readable as ours, so it must not
// travel either: jwt.ErrTokenExpired versus jwt.ErrTokenSignatureInvalid tells
// a prober it holds a structurally valid token.
func TestVerifyDoesNotLeakTheLibraryRejectionReason(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	stranger := newIssuerFixture(t, issuerBName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	leaky := []error{
		jwt.ErrTokenExpired,
		jwt.ErrTokenNotValidYet,
		jwt.ErrTokenSignatureInvalid,
		jwt.ErrTokenMalformed,
		jwt.ErrTokenUnverifiable,
		jwt.ErrTokenInvalidClaims,
	}

	tokens := map[string]string{
		"expired":          a.mintWithLifetime(t, time.Now().Add(-time.Hour), time.Minute),
		"wrong signature":  stranger.sign(t, contextFor(a.issuer, Version)),
		"malformed":        "not-a-token",
		"empty":            "",
		"two segments":     "aGVhZGVy.Y2xhaW1z",
		"unknown key id":   a.sign(t, contextFor(a.issuer, Version), func(h map[string]any) { h["kid"] = "absent" }),
		"untrusted issuer": stranger.mint(t),
	}
	for name, token := range tokens {
		t.Run(name, func(t *testing.T) {
			err := wantRejected(t, v, token, name)
			for _, leak := range leaky {
				if errors.Is(err, leak) {
					t.Errorf("Verify error satisfies errors.Is(err, %v): the library's reason reached the caller", leak)
				}
			}
		})
	}
}

// Every rejection is the same value with the same text. A caller cannot tell
// "bad signature" from "expired" from "unknown issuer" from "wrong version",
// because a per-reason error becomes a per-reason response, and a per-reason
// response is an oracle.
func TestVerifyRejectionsAreIndistinguishable(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, sharedKID)
	b := newIssuerFixture(t, issuerBName, sharedKID)
	stranger := newIssuerFixture(t, issuerCName, sharedKID)
	v := newTestVerifier(t, trustedSet(a, b))

	noSubject := contextFor(a.issuer, Version)
	noSubject.Subject = ""

	rejected := map[string]string{
		"untrusted issuer":     stranger.mint(t),
		"cross-issuer forgery": a.sign(t, contextFor(b.issuer, Version)),
		"unknown key id":       a.sign(t, contextFor(a.issuer, Version), func(h map[string]any) { h["kid"] = "absent" }),
		"wrong type":           a.sign(t, contextFor(a.issuer, Version), func(h map[string]any) { h["typ"] = "JWT" }),
		"unaccepted version":   a.sign(t, contextFor(a.issuer, Version+1)),
		"no subject":           a.sign(t, noSubject),
		"expired":              a.mintWithLifetime(t, time.Now().Add(-time.Hour), time.Minute),
		"hmac signed":          hmacForgery(t, a, contextFor(a.issuer, Version)),
		"malformed":            "not-a-token",
	}

	seen := map[string][]string{}
	for name, token := range rejected {
		err := wantRejected(t, v, token, name)
		seen[err.Error()] = append(seen[err.Error()], name)
	}
	if len(seen) != 1 {
		t.Errorf("rejections produced %d distinct messages %v, want exactly one uniform message", len(seen), seen)
	}
}

// The specific cause is still available — to the operator, through a seam that
// is not the returned error. Detail belongs in a log an operator reads, not in
// a response a prober reads.
func TestVerifyReportsTheSpecificCauseToTheOperatorOnly(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	var mu sync.Mutex
	var logged []error
	v := newTestVerifier(t, trustedSet(a), WithRejectionLogger(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, err)
	}))

	err := wantRejected(t, v, a.sign(t, contextFor(a.issuer, Version+1)), "an unaccepted version")

	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 1 {
		t.Fatalf("rejection logger called %d times, want 1", len(logged))
	}
	if !errors.Is(logged[0], ErrUnsupportedVersion) {
		t.Errorf("logged error = %v, want the specific cause for the operator", logged[0])
	}
	if errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("returned error = %v, want the cause withheld from the caller", err)
	}
}

func TestVerifyDoesNotLogAnAcceptedContext(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)

	calls := 0
	v := newTestVerifier(t, trustedSet(a), WithRejectionLogger(func(error) { calls++ }))

	wantAccepted(t, v, a.mint(t), "a valid context")
	if calls != 0 {
		t.Errorf("rejection logger called %d times for an accepted context, want 0", calls)
	}
}

// --- malformed input --------------------------------------------------------

func TestVerifyRejectsInputThatIsNotAContext(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	v := newTestVerifier(t, trustedSet(a))

	minted := a.mint(t)
	cases := map[string]string{
		"empty":            "",
		"not a token":      "not-a-token",
		"two segments":     "aGVhZGVy.Y2xhaW1z",
		"four segments":    "a.b.c.d",
		"only separators":  "...",
		"bearer prefixed":  "Bearer " + minted,
		"truncated":        minted[:len(minted)/2],
		"trailing garbage": minted + "tampered",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			wantRejected(t, v, token, name)
		})
	}
}

// --- the context seam -------------------------------------------------------

// The caller's context reaches the key source, so a remote key set fetch is
// cancelled with the request that triggered it rather than outliving it.
func TestVerifyPassesTheCallerContextToTheKeySource(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	recorder := &recordingKeySource{inner: a.keys}
	v := newTestVerifier(t, []TrustedIssuer{{Issuer: a.issuer, Keys: recorder}})

	ctx := context.WithValue(context.Background(), verifierCtxKey{}, "caller")
	if _, err := v.Verify(ctx, a.mint(t)); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}

	marks := recorder.marks()
	if len(marks) != 1 {
		t.Fatalf("key source consulted %d times, want 1", len(marks))
	}
	if marks[0] != "caller" {
		t.Errorf("key source saw context value %v, want the caller's context", marks[0])
	}
}

func TestVerifyRejectsWhenTheKeySourceFails(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	recorder := &recordingKeySource{inner: a.keys, err: ErrJWKSUnavailable}
	v := newTestVerifier(t, []TrustedIssuer{{Issuer: a.issuer, Keys: recorder}})

	wantRejected(t, v, a.mint(t), "a key source that cannot answer")
}

// --- concurrency ------------------------------------------------------------

// One verifier is shared across every request path in a process.
func TestVerifyIsSafeForConcurrentUse(t *testing.T) {
	a := newIssuerFixture(t, issuerAName, issuerAKID)
	b := newIssuerFixture(t, issuerBName, issuerBKID)
	v := newTestVerifier(t, trustedSet(a, b))

	valid := []string{a.mint(t), b.mint(t)}
	invalid := []string{
		a.sign(t, contextFor(issuerCName, Version)),
		a.sign(t, contextFor(a.issuer, Version+1)),
		"not-a-token",
	}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			if claims, err := v.Verify(context.Background(), valid[i%len(valid)]); err != nil {
				t.Errorf("Verify(valid) returned error: %v", err)
			} else if claims.Subject != validInput().Subject {
				t.Errorf("Verify(valid) subject = %q, want %q", claims.Subject, validInput().Subject)
			}

			if _, err := v.Verify(context.Background(), invalid[i%len(invalid)]); err != ErrInvalidContext { //nolint:errorlint // identity is precisely the assertion
				t.Errorf("Verify(invalid) error = %v, want exactly ErrInvalidContext", err)
			}
		}(i)
	}
	wg.Wait()
}

// --- test doubles -----------------------------------------------------------

// hmacForgery signs claims with HS256, using the DER encoding of the issuer's
// PUBLIC key as the shared secret. That encoding is published, so this is a
// token anyone can produce; only pinning the admitted algorithms refuses it.
func hmacForgery(t *testing.T, f *issuerFixture, claims *Claims) string {
	t.Helper()

	published, err := x509.MarshalPKIXPublicKey(f.signer.Public())
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey returned error: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = JWTType
	token.Header["kid"] = f.kid

	forged, err := token.SignedString(published)
	if err != nil {
		t.Fatalf("SignedString returned error: %v", err)
	}
	return forged
}

// recordingKeySource notes what it was asked for and what context it was asked
// in, and can be told to fail, so the tests can assert on key work that did or
// did not happen rather than only on the verdict.
type recordingKeySource struct {
	inner KeySource
	err   error

	mu       sync.Mutex
	kids     []string
	contexts []any
}

func (r *recordingKeySource) KeyForKID(ctx context.Context, kid string) (crypto.PublicKey, error) {
	r.mu.Lock()
	r.kids = append(r.kids, kid)
	r.contexts = append(r.contexts, ctx.Value(verifierCtxKey{}))
	r.mu.Unlock()

	if r.err != nil {
		return nil, r.err
	}
	return r.inner.KeyForKID(ctx, kid)
}

func (r *recordingKeySource) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.kids...)
}

func (r *recordingKeySource) marks() []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]any(nil), r.contexts...)
}

var _ KeySource = (*recordingKeySource)(nil)
