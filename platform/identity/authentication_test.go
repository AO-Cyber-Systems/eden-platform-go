package identity

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// testCredential is the credential type an in-test consumer verifies. Its
// shape is deliberately unlike anything this package knows about: the point of
// the generic seam is that the credential is the consumer's own business, and
// a test that used a type this package could have named would not demonstrate
// that.
type testCredential struct {
	Handle    string
	Assertion []byte
}

// stubVerifier is an in-test implementation of the credential seam. It answers
// from a fixed result rather than any store, because the seam deliberately
// says nothing about where credentials live.
type stubVerifier struct {
	result *Authentication
	err    error
}

func (s *stubVerifier) VerifyCredential(_ context.Context, credential testCredential) (*Authentication, error) {
	if s.err != nil {
		return nil, s.err
	}
	auth := *s.result
	auth.Subject = credential.Handle
	return &auth, nil
}

// stubResolver is an in-test implementation of the claims seam.
type stubResolver struct {
	grant Grant
	err   error
}

func (s *stubResolver) ResolveClaims(_ context.Context, subject string) (Grant, error) {
	if s.err != nil {
		return Grant{}, s.err
	}
	grant := s.grant
	grant.Tenant = subject + "-tenant"
	return grant, nil
}

// Compile-time assertions that the in-test types satisfy the seams. A change
// to either signature is a build failure here rather than a surprise at a
// consumer that only finds out when its own build breaks.
var (
	_ CredentialVerifier[testCredential] = (*stubVerifier)(nil)
	_ ClaimsResolver                     = (*stubResolver)(nil)
)

// wellFormedAuthentication returns an authentication that passes validation,
// for tests that mutate one field of it at a time.
func wellFormedAuthentication() Authentication {
	return Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorPossession, Method: "totp"},
		},
		PhishingResistant: true,
		HardwareBacked:    true,
	}
}

func TestAuthenticationValidateAcceptsWellFormed(t *testing.T) {
	auth := wellFormedAuthentication()

	if err := auth.Validate(); err != nil {
		t.Fatalf("Validate() on a well-formed authentication returned error: %v", err)
	}
}

func TestAuthenticationValidateRejects(t *testing.T) {
	tests := []struct {
		name string
		auth Authentication
		want error
	}{
		{
			name: "empty subject",
			auth: Authentication{
				Factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
			},
			want: ErrMissingAuthenticatedSubject,
		},
		{
			name: "nil factor slice",
			auth: Authentication{Subject: "principal-0001"},
			want: ErrNoFactors,
		},
		{
			name: "empty factor slice",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{},
			},
			want: ErrNoFactors,
		},
		{
			name: "empty factor kind",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{{Method: "password"}},
			},
			want: ErrUnknownFactorKind,
		},
		{
			name: "unknown factor kind",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{{Kind: FactorKind("something-else"), Method: "password"}},
			},
			want: ErrUnknownFactorKind,
		},
		{
			name: "empty factor method",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{{Kind: FactorKnowledge}},
			},
			want: ErrMissingFactorMethod,
		},
		{
			name: "second factor is the malformed one",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession},
				},
			},
			want: ErrMissingFactorMethod,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.auth.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() returned %v, want error matching %v", err, tc.want)
			}
		})
	}
}

func TestAuthenticationValidateRejectsNil(t *testing.T) {
	var auth *Authentication

	if err := auth.Validate(); !errors.Is(err, ErrNilAuthentication) {
		t.Fatalf("Validate() on nil returned %v, want error matching %v", err, ErrNilAuthentication)
	}
}

func TestAuthenticationKindsReturnsDistinctCategories(t *testing.T) {
	tests := []struct {
		name    string
		factors []Factor
		want    []FactorKind
	}{
		{
			name:    "single factor is a single category",
			factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
			want:    []FactorKind{FactorKnowledge},
		},
		{
			name: "two knowledge factors collapse to one category",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorKnowledge, Method: "security_question"},
			},
			want: []FactorKind{FactorKnowledge},
		},
		{
			name: "three methods within one category collapse to one category",
			factors: []Factor{
				{Kind: FactorPossession, Method: "totp"},
				{Kind: FactorPossession, Method: "lookup_secret"},
				{Kind: FactorPossession, Method: "push_approval"},
			},
			want: []FactorKind{FactorPossession},
		},
		{
			name: "distinct categories are both reported",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "totp"},
			},
			want: []FactorKind{FactorKnowledge, FactorPossession},
		},
		{
			name: "all three categories",
			factors: []Factor{
				{Kind: FactorInherence, Method: "fingerprint"},
				{Kind: FactorPossession, Method: "webauthn"},
				{Kind: FactorKnowledge, Method: "password"},
			},
			want: []FactorKind{FactorKnowledge, FactorPossession, FactorInherence},
		},
		{
			name: "duplicates across a longer list still collapse",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "totp"},
				{Kind: FactorKnowledge, Method: "security_question"},
				{Kind: FactorPossession, Method: "lookup_secret"},
			},
			want: []FactorKind{FactorKnowledge, FactorPossession},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := Authentication{Subject: "principal-0001", Factors: tc.factors}

			got := auth.Kinds()
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Kinds() returned %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAuthenticationKindsIsEmptyWithoutFactors(t *testing.T) {
	auth := Authentication{Subject: "principal-0001"}

	if got := auth.Kinds(); len(got) != 0 {
		t.Fatalf("Kinds() with no factors returned %v, want empty", got)
	}
}

// A factor whose category was never set names no category, so it contributes
// none. Validate is what rejects it; Kinds must not manufacture a category out
// of the empty string, which would otherwise count toward assurance.
func TestAuthenticationKindsIgnoresEmptyKind(t *testing.T) {
	auth := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{{Method: "password"}},
	}

	if got := auth.Kinds(); len(got) != 0 {
		t.Fatalf("Kinds() with an unnamed category returned %v, want empty", got)
	}
}

// The order Kinds reports must not depend on the order a verifier happened to
// record factors in: the same authentication described two ways is the same
// authentication, and a caller comparing or logging the result should see one
// answer rather than two.
func TestAuthenticationKindsOrderIsIndependentOfFactorOrder(t *testing.T) {
	forward := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorPossession, Method: "totp"},
			{Kind: FactorInherence, Method: "fingerprint"},
		},
	}
	reversed := Authentication{
		Subject: "principal-0001",
		Factors: slices.Clone(forward.Factors),
	}
	slices.Reverse(reversed.Factors)

	if got, want := reversed.Kinds(), forward.Kinds(); !slices.Equal(got, want) {
		t.Fatalf("Kinds() on reordered factors returned %v, want %v", got, want)
	}
}

// Kinds must not hand a caller a window onto the authentication's own state.
func TestAuthenticationKindsDoesNotAliasFactors(t *testing.T) {
	auth := wellFormedAuthentication()

	kinds := auth.Kinds()
	if len(kinds) == 0 {
		t.Fatalf("Kinds() returned empty for a well-formed authentication")
	}
	kinds[0] = FactorKind("mutated")

	if got := auth.Kinds(); got[0] == FactorKind("mutated") {
		t.Fatalf("mutating the returned slice changed a later Kinds() result: %v", got)
	}
}

func TestCredentialVerifierSeamIsUsableThroughTheInterface(t *testing.T) {
	result := wellFormedAuthentication()
	var verifier CredentialVerifier[testCredential] = &stubVerifier{result: &result}

	auth, err := verifier.VerifyCredential(context.Background(), testCredential{
		Handle:    "principal-0002",
		Assertion: []byte("signed-challenge"),
	})
	if err != nil {
		t.Fatalf("VerifyCredential() returned error: %v", err)
	}
	if auth == nil {
		t.Fatalf("VerifyCredential() returned a nil authentication and a nil error")
	}
	if auth.Subject != "principal-0002" {
		t.Fatalf("VerifyCredential() returned subject %q, want %q", auth.Subject, "principal-0002")
	}
	if err := auth.Validate(); err != nil {
		t.Fatalf("Validate() on the verified authentication returned error: %v", err)
	}
}

func TestCredentialVerifierSeamPropagatesFailure(t *testing.T) {
	wantErr := errors.New("credential rejected")
	var verifier CredentialVerifier[testCredential] = &stubVerifier{err: wantErr}

	auth, err := verifier.VerifyCredential(context.Background(), testCredential{Handle: "principal-0002"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("VerifyCredential() returned error %v, want %v", err, wantErr)
	}
	if auth != nil {
		t.Fatalf("VerifyCredential() returned authentication %+v alongside an error, want nil", auth)
	}
}

func TestClaimsResolverSeamIsUsableThroughTheInterface(t *testing.T) {
	var resolver ClaimsResolver = &stubResolver{
		grant: Grant{Entitlements: []string{"reports.read"}},
	}

	grant, err := resolver.ResolveClaims(context.Background(), "principal-0003")
	if err != nil {
		t.Fatalf("ResolveClaims() returned error: %v", err)
	}
	if grant.Tenant != "principal-0003-tenant" {
		t.Fatalf("ResolveClaims() returned tenant %q, want %q", grant.Tenant, "principal-0003-tenant")
	}
	if !slices.Equal(grant.Entitlements, []string{"reports.read"}) {
		t.Fatalf("ResolveClaims() returned entitlements %v, want %v", grant.Entitlements, []string{"reports.read"})
	}
}

// Grant supplies exactly the two claims the minter needs beyond the subject.
// Anything else appearing here is a model this package has no business
// imposing on a consumer, so the field set is asserted rather than assumed.
func TestGrantCarriesTenantAndEntitlementsOnly(t *testing.T) {
	want := []string{"Tenant", "Entitlements"}

	typ := reflect.TypeOf(Grant{})
	got := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		got = append(got, typ.Field(i).Name)
	}

	if !slices.Equal(got, want) {
		t.Fatalf("Grant has fields %v, want exactly %v", got, want)
	}
}

// The seams stay narrow by construction: one method each, over a context and
// either a consumer-supplied credential or a subject string. A parameter added
// here to carry a session, a role or a refresh token would widen the contract
// every consumer has to satisfy, so the signatures are pinned.
func TestSeamsExposeExactlyOneMethod(t *testing.T) {
	verifier := reflect.TypeOf((*CredentialVerifier[testCredential])(nil)).Elem()
	if got := verifier.NumMethod(); got != 1 {
		t.Fatalf("CredentialVerifier has %d methods, want 1", got)
	}
	if got := verifier.Method(0).Name; got != "VerifyCredential" {
		t.Fatalf("CredentialVerifier method is named %q, want %q", got, "VerifyCredential")
	}
	if got, want := verifier.Method(0).Type.NumIn(), 2; got != want {
		t.Fatalf("VerifyCredential takes %d parameters, want %d", got, want)
	}
	if got, want := verifier.Method(0).Type.In(1), reflect.TypeOf(testCredential{}); got != want {
		t.Fatalf("VerifyCredential takes credential type %v, want %v", got, want)
	}

	resolver := reflect.TypeOf((*ClaimsResolver)(nil)).Elem()
	if got := resolver.NumMethod(); got != 1 {
		t.Fatalf("ClaimsResolver has %d methods, want 1", got)
	}
	if got := resolver.Method(0).Name; got != "ResolveClaims" {
		t.Fatalf("ClaimsResolver method is named %q, want %q", got, "ResolveClaims")
	}
	if got, want := resolver.Method(0).Type.NumIn(), 2; got != want {
		t.Fatalf("ResolveClaims takes %d parameters, want %d", got, want)
	}
	if got, want := resolver.Method(0).Type.In(1).Kind(), reflect.String; got != want {
		t.Fatalf("ResolveClaims takes subject of kind %v, want %v", got, want)
	}
	if got, want := resolver.Method(0).Type.Out(0), reflect.TypeOf(Grant{}); got != want {
		t.Fatalf("ResolveClaims returns %v, want %v", got, want)
	}
}

// The three categories are the ones assurance is derived from. A fourth
// appearing without the derivation being revisited would be counted as an
// independent factor by a rule that was never written with it in mind.
func TestFactorKindsAreTheThreeCategories(t *testing.T) {
	want := []FactorKind{"knowledge", "possession", "inherence"}
	got := []FactorKind{FactorKnowledge, FactorPossession, FactorInherence}

	if !slices.Equal(got, want) {
		t.Fatalf("factor categories are %v, want %v", got, want)
	}
}
