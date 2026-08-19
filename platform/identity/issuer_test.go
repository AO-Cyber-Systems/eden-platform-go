package identity

import (
	"context"
	"crypto"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// These tests are about ORDER as much as outcome.
//
// Every rule the issuer follows is a security property: which seam is consulted
// first, which one is not consulted at all once an earlier one has failed, and
// which of two subjects reaches the minted context. A test that only asserted
// "an error came back" would pass against an implementation that resolved a
// stranger's entitlements before deciding the stranger had not authenticated,
// so the fakes below record every call and the assertions read the sequence.

const (
	// issuerUnderTest and issuerKeyID name the issuer these tests build. They
	// are separate from the minter tests' constants so that a change to either
	// suite's fixture cannot silently retune the other.
	issuerUnderTest = "https://issuing-service.example/"
	issuerKeyID     = "issuing-key-2024-06"

	// rotatedIssuerKeyID stands for the identifier the key surface reports
	// after the signing key has been replaced. It exists to show the kid
	// follows the key rather than a value the caller chose.
	rotatedIssuerKeyID = "issuing-key-2024-12"

	// credentialHandle is what the caller CLAIMS to be, and authenticatedSubject
	// is what the verifier says they PROVED to be. They differ on purpose:
	// only the second may reach a context.
	credentialHandle     = "claimed-handle-0001"
	authenticatedSubject = "principal-0001"
)

// --- recording seams ---------------------------------------------------------

// seam names as they appear in a recorded call sequence.
const (
	callHealthCheck      = "HealthCheck"
	callVerifyCredential = "VerifyCredential"
	callResolveClaims    = "ResolveClaims"
)

// issuerCtxKey marks a caller's context so a seam can prove it was handed that
// context rather than a fresh one. A context that does not reach the seams is a
// cancellation and a deadline that do not reach whatever they talk to.
type issuerCtxKey struct{}

// recorder is the shared call log every fake writes to. Reading the sequence
// off one object is what makes "the resolver was never reached" expressible as
// an assertion rather than an inference.
type recorder struct {
	mu sync.Mutex

	calls []string

	// credentials and subjects capture what each seam was actually handed, so
	// a test can show the subject the resolver was asked about came from the
	// authentication and not from the credential.
	credentials []testCredential
	subjects    []string

	// ctxValues records the marker found on the context each seam received,
	// keyed by seam name.
	ctxValues map[string]any
}

func newRecorder() *recorder {
	return &recorder{ctxValues: map[string]any{}}
}

func (r *recorder) record(ctx context.Context, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	if _, seen := r.ctxValues[name]; !seen {
		r.ctxValues[name] = ctx.Value(issuerCtxKey{})
	}
}

// sequence returns the calls in order, excluding the construction-time health
// check so that a test about Issue reads only what Issue did.
func (r *recorder) sequence() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	seq := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		if call == callHealthCheck {
			continue
		}
		seq = append(seq, call)
	}
	return seq
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := 0
	for _, call := range r.calls {
		if call == name {
			total++
		}
	}
	return total
}

func (r *recorder) ctxValue(name string) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctxValues[name]
}

func (r *recorder) resolvedSubjects() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.subjects)
}

func (r *recorder) verifiedCredentials() []testCredential {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.credentials)
}

// recordingVerifier answers from a fixed result and logs that it was consulted.
type recordingVerifier struct {
	rec *recorder

	// result is returned as-is. A test sets it to nil, with a nil error, to
	// stand in for a verifier that reports success without establishing
	// anything.
	result *Authentication
	err    error
}

func (v *recordingVerifier) VerifyCredential(ctx context.Context, credential testCredential) (*Authentication, error) {
	v.rec.record(ctx, callVerifyCredential)
	v.rec.mu.Lock()
	v.rec.credentials = append(v.rec.credentials, credential)
	v.rec.mu.Unlock()

	if v.err != nil {
		return nil, v.err
	}
	return v.result, nil
}

// recordingResolver answers from a fixed grant and logs the subject it was
// asked about.
type recordingResolver struct {
	rec *recorder

	grant Grant
	err   error
}

func (r *recordingResolver) ResolveClaims(ctx context.Context, subject string) (Grant, error) {
	r.rec.record(ctx, callResolveClaims)
	r.rec.mu.Lock()
	r.rec.subjects = append(r.rec.subjects, subject)
	r.rec.mu.Unlock()

	if r.err != nil {
		return Grant{}, r.err
	}
	return r.grant, nil
}

// recordingSigner is the key surface the issuer is built over: a signer that
// reports its own identifier and can be made to fail its health check.
//
// It embeds the package's ES256 test signer for the crypto half, so the token
// it produces is a real signature that the shipped verifier can check.
type recordingSigner struct {
	*ecdsaTestSigner

	rec *recorder

	keyID     string
	healthErr error
}

func (s *recordingSigner) KeyID() string { return s.keyID }

func (s *recordingSigner) HealthCheck(ctx context.Context) error {
	s.rec.record(ctx, callHealthCheck)
	return s.healthErr
}

// Compile-time assertions. The first is the one that matters: the issuer takes
// the platform key surface, not the narrower signing seam, because that is
// where KeyID and HealthCheck live.
var (
	_ kms.KMSSigner                      = (*recordingSigner)(nil)
	_ CredentialVerifier[testCredential] = (*recordingVerifier)(nil)
	_ ClaimsResolver                     = (*recordingResolver)(nil)
)

// --- fixture -----------------------------------------------------------------

// issuerHarness is one wired issuer plus the fakes behind it, so a test can
// drive a seam and then read what happened.
type issuerHarness struct {
	issuer   *Issuer[testCredential]
	rec      *recorder
	signer   *recordingSigner
	verifier *recordingVerifier
	resolver *recordingResolver
}

// harnessConfig is the set of knobs a test turns before construction.
type harnessConfig struct {
	auth       *Authentication
	verifyErr  error
	grant      Grant
	resolveErr error
	keyID      string
	healthErr  error
	issuerName string
}

func defaultHarnessConfig() harnessConfig {
	auth := wellFormedAuthentication()
	auth.Subject = authenticatedSubject

	return harnessConfig{
		auth:       &auth,
		grant:      Grant{Tenant: "acme", Entitlements: []string{"reports.read", "reports.write"}},
		keyID:      issuerKeyID,
		issuerName: issuerUnderTest,
	}
}

// newHarnessParts builds the fakes without constructing the issuer, for the
// constructor tests that need to observe NewIssuer's own error.
func newHarnessParts(t *testing.T, cfg harnessConfig) (*recorder, *recordingSigner, *recordingVerifier, *recordingResolver) {
	t.Helper()

	rec := newRecorder()
	signer := &recordingSigner{
		ecdsaTestSigner: newES256Signer(t),
		rec:             rec,
		keyID:           cfg.keyID,
		healthErr:       cfg.healthErr,
	}
	verifier := &recordingVerifier{rec: rec, result: cfg.auth, err: cfg.verifyErr}
	resolver := &recordingResolver{rec: rec, grant: cfg.grant, err: cfg.resolveErr}
	return rec, signer, verifier, resolver
}

func newHarness(t *testing.T, cfg harnessConfig) *issuerHarness {
	t.Helper()

	rec, signer, verifier, resolver := newHarnessParts(t, cfg)
	issuer, err := NewIssuer[testCredential](context.Background(), signer, cfg.issuerName, verifier, resolver)
	if err != nil {
		t.Fatalf("NewIssuer returned error: %v", err)
	}
	return &issuerHarness{issuer: issuer, rec: rec, signer: signer, verifier: verifier, resolver: resolver}
}

// issueCtx returns a marked context, so a seam can prove it received the
// caller's context.
func issueCtx() context.Context {
	return context.WithValue(context.Background(), issuerCtxKey{}, "caller-context")
}

func aCredential() testCredential {
	return testCredential{Handle: credentialHandle, Assertion: []byte("signed-challenge")}
}

// --- the key id is not a parameter -------------------------------------------

func TestNewIssuerAcceptsNoKeyIDParameter(t *testing.T) {
	// A caller that can pass a key id can pin a constant one, and a constant
	// key id survives the key material being replaced underneath it: a
	// consumer's cache only re-fetches on an id it does not know, so an id
	// that never changes never misses, and every request is refused with no
	// miss to trigger recovery. The defence is the signature — there is
	// nowhere to pass one — so this asserts on the signature itself.
	fn := reflect.TypeOf(NewIssuer[testCredential])

	var stringParams int
	for i := range fn.NumIn() {
		if fn.In(i).Kind() == reflect.String {
			stringParams++
		}
	}
	if stringParams != 1 {
		t.Errorf("NewIssuer takes %d string parameters, want exactly 1 (the issuer name); "+
			"a second string parameter would let a caller pin a constant key id", stringParams)
	}
}

func TestIssueStampsTheKeyIDReportedBySigner(t *testing.T) {
	h := newHarness(t, defaultHarnessConfig())

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if got := header(t, token)["kid"]; got != issuerKeyID {
		t.Errorf("kid header = %v, want %q (the signer's own KeyID)", got, issuerKeyID)
	}
}

func TestIssueFollowsTheKeyIDWhenTheKeyIsReplaced(t *testing.T) {
	// Replacing the key changes the identifier the key surface reports, and
	// the kid must follow it. That change is what produces the cache miss a
	// consumer recovers through.
	first := newHarness(t, defaultHarnessConfig())

	rotatedCfg := defaultHarnessConfig()
	rotatedCfg.keyID = rotatedIssuerKeyID
	second := newHarness(t, rotatedCfg)

	firstToken, err := first.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	secondToken, err := second.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	firstKID := header(t, firstToken)["kid"]
	secondKID := header(t, secondToken)["kid"]
	if firstKID != issuerKeyID {
		t.Errorf("kid header = %v, want %q", firstKID, issuerKeyID)
	}
	if secondKID != rotatedIssuerKeyID {
		t.Errorf("kid header after replacement = %v, want %q", secondKID, rotatedIssuerKeyID)
	}
	if firstKID == secondKID {
		t.Error("the kid did not change when the key did; a consumer's cache would never miss and never recover")
	}
}

// --- construction ------------------------------------------------------------

func TestNewIssuerRejects(t *testing.T) {
	// Each omission is reported distinctly. These surface at process start, to
	// an operator who has to know which of several settings is wrong, and
	// there is no authorization oracle to protect by collapsing them.
	cases := []struct {
		name string
		// mutate blanks one dependency out of an otherwise complete set.
		build func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver)
		want  error
	}{
		{
			name: "nil credential verifier",
			build: func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver) {
				_, signer, _, resolver := newHarnessParts(t, defaultHarnessConfig())
				return signer, issuerUnderTest, nil, resolver
			},
			want: ErrNilCredentialVerifier,
		},
		{
			name: "nil claims resolver",
			build: func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver) {
				_, signer, verifier, _ := newHarnessParts(t, defaultHarnessConfig())
				return signer, issuerUnderTest, verifier, nil
			},
			want: ErrNilClaimsResolver,
		},
		{
			name: "nil signer",
			build: func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver) {
				_, _, verifier, resolver := newHarnessParts(t, defaultHarnessConfig())
				return nil, issuerUnderTest, verifier, resolver
			},
			want: ErrNilSigner,
		},
		{
			name: "empty issuer",
			build: func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver) {
				_, signer, verifier, resolver := newHarnessParts(t, defaultHarnessConfig())
				return signer, "", verifier, resolver
			},
			want: ErrMissingIssuer,
		},
		{
			name: "empty key id",
			build: func(t *testing.T) (kms.KMSSigner, string, CredentialVerifier[testCredential], ClaimsResolver) {
				cfg := defaultHarnessConfig()
				cfg.keyID = ""
				_, signer, verifier, resolver := newHarnessParts(t, cfg)
				return signer, issuerUnderTest, verifier, resolver
			},
			want: ErrMissingKeyID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer, name, verifier, resolver := tc.build(t)

			issuer, err := NewIssuer[testCredential](context.Background(), signer, name, verifier, resolver)
			if !errors.Is(err, tc.want) {
				t.Errorf("NewIssuer error = %v, want %v", err, tc.want)
			}
			if issuer != nil {
				t.Error("NewIssuer returned an issuer alongside an error")
			}
		})
	}
}

func TestNewIssuerProvesSigningAtConstruction(t *testing.T) {
	// Reading a public key and signing with it are separately authorized on a
	// hosted key service, so a configuration permitting the first and denying
	// the second looks healthy until the first login. The health check is a
	// real sign-and-verify round trip, and running it here turns that into a
	// start-up failure an operator sees rather than an outage a user finds.
	h := newHarness(t, defaultHarnessConfig())

	if got := h.rec.count(callHealthCheck); got != 1 {
		t.Errorf("HealthCheck called %d times during construction, want 1", got)
	}
}

func TestNewIssuerRefusesAnUnhealthySigner(t *testing.T) {
	denied := errors.New("sign denied by policy")

	cfg := defaultHarnessConfig()
	cfg.healthErr = denied
	_, signer, verifier, resolver := newHarnessParts(t, cfg)

	issuer, err := NewIssuer[testCredential](context.Background(), signer, issuerUnderTest, verifier, resolver)
	if !errors.Is(err, ErrSignerUnhealthy) {
		t.Errorf("NewIssuer error = %v, want %v", err, ErrSignerUnhealthy)
	}
	if !errors.Is(err, denied) {
		t.Errorf("NewIssuer error = %v, want it to wrap the cause %v", err, denied)
	}
	if issuer != nil {
		t.Error("NewIssuer returned an issuer despite a failing health check")
	}
}

func TestNewIssuerPassesTheCallersContextToTheHealthCheck(t *testing.T) {
	cfg := defaultHarnessConfig()
	rec, signer, verifier, resolver := newHarnessParts(t, cfg)

	if _, err := NewIssuer[testCredential](issueCtx(), signer, issuerUnderTest, verifier, resolver); err != nil {
		t.Fatalf("NewIssuer returned error: %v", err)
	}
	if got := rec.ctxValue(callHealthCheck); got != "caller-context" {
		t.Errorf("HealthCheck received context value %v, want the caller's context", got)
	}
}

// --- the order is the security property --------------------------------------

func TestIssueConsultsTheSeamsInOrder(t *testing.T) {
	h := newHarness(t, defaultHarnessConfig())

	if _, err := h.issuer.Issue(issueCtx(), aCredential()); err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	want := []string{callVerifyCredential, callResolveClaims}
	if got := h.rec.sequence(); !slices.Equal(got, want) {
		t.Errorf("call sequence = %v, want %v", got, want)
	}
}

func TestIssueDoesNotResolveClaimsWhenVerificationFails(t *testing.T) {
	// Resolving entitlements for an identifier that has not authenticated is a
	// disclosure even when nothing is minted afterwards: it answers "what is
	// this principal entitled to" for a caller who has proved nothing.
	rejected := errors.New("credential rejected")

	cfg := defaultHarnessConfig()
	cfg.verifyErr = rejected
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if !errors.Is(err, rejected) {
		t.Errorf("Issue error = %v, want it to wrap %v", err, rejected)
	}
	if token != "" {
		t.Error("Issue returned a token after verification failed")
	}
	if got := h.rec.count(callResolveClaims); got != 0 {
		t.Errorf("ResolveClaims called %d times after a failed verification, want 0", got)
	}
	if got := h.rec.sequence(); !slices.Equal(got, []string{callVerifyCredential}) {
		t.Errorf("call sequence = %v, want only %v", got, callVerifyCredential)
	}
}

func TestIssueTreatsANilAuthenticationAsFailure(t *testing.T) {
	// A verifier returning (nil, nil) is a broken verifier. Reading that as
	// success would mint a context for nobody.
	cfg := defaultHarnessConfig()
	cfg.auth = nil
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if !errors.Is(err, ErrVerifierReportedNoAuthentication) {
		t.Errorf("Issue error = %v, want %v", err, ErrVerifierReportedNoAuthentication)
	}
	if token != "" {
		t.Error("Issue minted a context for a nil authentication")
	}
	if got := h.rec.count(callResolveClaims); got != 0 {
		t.Errorf("ResolveClaims called %d times for a nil authentication, want 0", got)
	}
}

func TestIssueRefusesAnAuthenticationThatFailsValidation(t *testing.T) {
	cases := []struct {
		name string
		auth Authentication
		want error
	}{
		{
			name: "no subject",
			auth: Authentication{Factors: []Factor{{Kind: FactorKnowledge, Method: "password"}}},
			want: ErrMissingAuthenticatedSubject,
		},
		{
			name: "no factors",
			auth: Authentication{Subject: authenticatedSubject},
			want: ErrNoFactors,
		},
		{
			name: "unrecognised factor category",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{{Kind: FactorKind("telepathy"), Method: "guess"}},
			},
			want: ErrUnknownFactorKind,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultHarnessConfig()
			auth := tc.auth
			cfg.auth = &auth
			h := newHarness(t, cfg)

			token, err := h.issuer.Issue(issueCtx(), aCredential())
			if !errors.Is(err, tc.want) {
				t.Errorf("Issue error = %v, want %v", err, tc.want)
			}
			if token != "" {
				t.Error("Issue minted a context from an invalid authentication")
			}
			if got := h.rec.count(callResolveClaims); got != 0 {
				t.Errorf("ResolveClaims called %d times for an invalid authentication, want 0", got)
			}
		})
	}
}

func TestIssueMintsNothingWhenTheResolverFails(t *testing.T) {
	unavailable := errors.New("entitlement store unavailable")

	cfg := defaultHarnessConfig()
	cfg.resolveErr = unavailable
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if !errors.Is(err, unavailable) {
		t.Errorf("Issue error = %v, want it to wrap %v", err, unavailable)
	}
	if token != "" {
		t.Error("Issue returned a token after the resolver failed")
	}
}

func TestIssuePassesTheCallersContextToBothSeams(t *testing.T) {
	h := newHarness(t, defaultHarnessConfig())

	if _, err := h.issuer.Issue(issueCtx(), aCredential()); err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	for _, seam := range []string{callVerifyCredential, callResolveClaims} {
		if got := h.rec.ctxValue(seam); got != "caller-context" {
			t.Errorf("%s received context value %v, want the caller's context", seam, got)
		}
	}
}

// --- the subject comes from the authentication --------------------------------

func TestIssueTakesTheSubjectFromTheAuthentication(t *testing.T) {
	// The credential says who the caller CLAIMS to be; the authentication says
	// who they PROVED to be. Only the second belongs in a context, and only
	// the second may be used to look up entitlements.
	h := newHarness(t, defaultHarnessConfig())

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if got := payload(t, token)["sub"]; got != authenticatedSubject {
		t.Errorf("sub claim = %v, want %q (from the authentication, not the credential)", got, authenticatedSubject)
	}
	if got := h.rec.resolvedSubjects(); !slices.Equal(got, []string{authenticatedSubject}) {
		t.Errorf("ResolveClaims asked about %v, want %v", got, []string{authenticatedSubject})
	}
	// The credential still reached the verifier untouched; the issuer does not
	// interpret it.
	if got := h.rec.verifiedCredentials(); len(got) != 1 || got[0].Handle != credentialHandle {
		t.Errorf("VerifyCredential received %v, want the caller's credential unchanged", got)
	}
}

// --- assurance is derived, never supplied -------------------------------------

func TestIssueDerivesAssuranceFromTheFactorsExercised(t *testing.T) {
	// Each rung of the ladder must reach the minted claim intact. The level is
	// not a parameter, not a field on the issuer, and not a default.
	cases := []struct {
		name string
		auth Authentication
		want string
	}{
		{
			name: "one category",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorKnowledge, Method: "security_question"},
				},
			},
			want: AAL1,
		},
		{
			name: "two categories",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "totp"},
				},
			},
			want: AAL2,
		},
		{
			name: "two categories with only one asserted property",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
				},
				PhishingResistant: true,
			},
			want: AAL2,
		},
		{
			name: "two categories with both asserted properties",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
				},
				PhishingResistant: true,
				HardwareBacked:    true,
			},
			want: AAL3,
		},
		{
			name: "both properties without two categories does not reach the top rung",
			auth: Authentication{
				Subject: authenticatedSubject,
				Factors: []Factor{
					{Kind: FactorPossession, Method: "webauthn"},
				},
				PhishingResistant: true,
				HardwareBacked:    true,
			},
			want: AAL1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultHarnessConfig()
			auth := tc.auth
			cfg.auth = &auth
			h := newHarness(t, cfg)

			token, err := h.issuer.Issue(issueCtx(), aCredential())
			if err != nil {
				t.Fatalf("Issue returned error: %v", err)
			}
			if got := payload(t, token)["aal"]; got != tc.want {
				t.Errorf("aal claim = %v, want %q", got, tc.want)
			}
		})
	}
}

// --- the resolved grant reaches the context -----------------------------------

func TestIssueCarriesTheResolvedTenantAndEntitlements(t *testing.T) {
	cfg := defaultHarnessConfig()
	cfg.grant = Grant{Tenant: "northwind", Entitlements: []string{"invoices.read", "invoices.approve"}}
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	claims := payload(t, token)
	if got := claims["tenant"]; got != "northwind" {
		t.Errorf("tenant claim = %v, want %q", got, "northwind")
	}

	raw, ok := claims["ent"].([]any)
	if !ok {
		t.Fatalf("ent claim = %v (%T), want a JSON array", claims["ent"], claims["ent"])
	}
	got := make([]string, 0, len(raw))
	for _, v := range raw {
		got = append(got, v.(string))
	}
	want := []string{"invoices.read", "invoices.approve"}
	if !slices.Equal(got, want) {
		t.Errorf("ent claim = %v, want %v", got, want)
	}
}

func TestIssueRefusesAnEmptyTenantRatherThanMintingAnUnscopedContext(t *testing.T) {
	// An absent tenant read as "every tenant" is a cross-tenant escalation, so
	// a resolver that answers with one surfaces as a mint failure rather than
	// as a context nothing can scope.
	cfg := defaultHarnessConfig()
	cfg.grant = Grant{Tenant: "", Entitlements: []string{"reports.read"}}
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if !errors.Is(err, ErrMissingTenant) {
		t.Errorf("Issue error = %v, want %v", err, ErrMissingTenant)
	}
	if token != "" {
		t.Error("Issue minted a context with no tenant")
	}
}

func TestIssueEmitsAnEmptyEntitlementListRatherThanNull(t *testing.T) {
	// Holding no entitlements is a legitimate answer, and it must stay
	// distinguishable from an issuer that does not speak the claim.
	cfg := defaultHarnessConfig()
	cfg.grant = Grant{Tenant: "acme"}
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	raw, ok := payload(t, token)["ent"].([]any)
	if !ok {
		t.Fatalf("ent claim = %v (%T), want an empty JSON array", payload(t, token)["ent"], payload(t, token)["ent"])
	}
	if len(raw) != 0 {
		t.Errorf("ent claim = %v, want an empty array", raw)
	}
}

// --- the minted context verifies ----------------------------------------------

func TestIssueMintsAContextTheVerifierAccepts(t *testing.T) {
	// Minting through the issuing path and verifying through the shipped
	// verifier is what shows the two halves actually interoperate — including
	// that the kid the issuer stamps is the one a key set is looked up by.
	cfg := defaultHarnessConfig()
	auth := wellFormedAuthentication()
	auth.Subject = authenticatedSubject
	cfg.auth = &auth
	h := newHarness(t, cfg)

	token, err := h.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	keys := NewStaticKeys(map[string]crypto.PublicKey{h.signer.KeyID(): h.signer.Public()})
	verifier, err := NewVerifier([]TrustedIssuer{{Issuer: issuerUnderTest, Keys: keys}})
	if err != nil {
		t.Fatalf("NewVerifier returned error: %v", err)
	}

	claims, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if claims.Subject != authenticatedSubject {
		t.Errorf("verified subject = %q, want %q", claims.Subject, authenticatedSubject)
	}
	if claims.Issuer != issuerUnderTest {
		t.Errorf("verified issuer = %q, want %q", claims.Issuer, issuerUnderTest)
	}
	if claims.Tenant != "acme" {
		t.Errorf("verified tenant = %q, want %q", claims.Tenant, "acme")
	}
	// wellFormedAuthentication exercises two categories and asserts both
	// properties, so the derived level is the top rung.
	if claims.AAL != AAL3 {
		t.Errorf("verified assurance = %q, want %q", claims.AAL, AAL3)
	}
}

func TestIssueAcceptsMinterOptions(t *testing.T) {
	// The issuer forwards construction options to the minter, so a deployment
	// can shorten a context's lifetime without this package growing a second
	// way to express it.
	_, signer, verifier, resolver := newHarnessParts(t, defaultHarnessConfig())

	issuer, err := NewIssuer[testCredential](
		context.Background(), signer, issuerUnderTest, verifier, resolver,
		WithTTL(-time.Second),
	)
	if !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("NewIssuer error = %v, want %v", err, ErrInvalidTTL)
	}
	if issuer != nil {
		t.Error("NewIssuer returned an issuer despite an unusable option")
	}
}

// --- no unauthenticated mint path ---------------------------------------------

func TestIssuerExposesNoSecondMintPath(t *testing.T) {
	// Issue is the only exported method, so there is no exported way to reach
	// the minter without a successful credential verification first.
	methods := reflect.TypeOf(&Issuer[testCredential]{}).NumMethod()
	names := make([]string, 0, methods)
	for i := range methods {
		names = append(names, reflect.TypeOf(&Issuer[testCredential]{}).Method(i).Name)
	}

	if !slices.Equal(names, []string{"Issue"}) {
		t.Errorf("*Issuer exports %v, want only [Issue]; any other exported method is a mint path "+
			"that does not begin with a credential verification", names)
	}
}

func TestIssuingPathDeclaresNoBuildTaggedFiles(t *testing.T) {
	// A mint path that fabricates a subject must not be reachable behind a
	// build tag. Build-constrained files are invisible to the default test
	// run, so this suite could not observe such a path even in principle. This
	// check is what notices the file appearing at all.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("os.ReadDir(.) returned error: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("os.ReadFile(%s) returned error: %v", entry.Name(), err)
		}
		// A constraint is only meaningful above the package clause.
		for _, line := range strings.Split(string(source), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				break
			}
			if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
				t.Errorf("%s carries the build constraint %q; an issuing path must not be build-tagged", entry.Name(), line)
			}
		}
	}
}

// --- concurrency --------------------------------------------------------------

func TestIssueIsSafeForConcurrentUse(t *testing.T) {
	// A process shares one issuer across every request path, so this is the
	// ordinary case rather than an edge one.
	h := newHarness(t, defaultHarnessConfig())

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	tokens := make([]string, goroutines)

	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = h.issuer.Issue(issueCtx(), aCredential())
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Issue returned error: %v", i, err)
			continue
		}
		if got := payload(t, tokens[i])["sub"]; got != authenticatedSubject {
			t.Errorf("goroutine %d: sub claim = %v, want %q", i, got, authenticatedSubject)
		}
	}
}
