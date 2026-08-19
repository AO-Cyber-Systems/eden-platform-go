package identity

import (
	"errors"
	"reflect"
	"testing"
)

// The rungs of the ladder, and every way of stopping short of the next one.
// The rows that matter most are the ones a careless derivation gets wrong:
// counting prompts instead of categories, and reading the two properties as
// though they could stand in for a second category.
func TestDeriveAssurance(t *testing.T) {
	tests := []struct {
		name string
		auth Authentication
		want string
	}{
		{
			name: "a single factor is AAL1",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
			},
			want: AAL1,
		},
		{
			name: "two knowledge factors are one category, so AAL1",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorKnowledge, Method: "security_question"},
				},
			},
			want: AAL1,
		},
		{
			name: "three methods within one category are still AAL1",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorPossession, Method: "totp"},
					{Kind: FactorPossession, Method: "lookup_secret"},
					{Kind: FactorPossession, Method: "push_approval"},
				},
			},
			want: AAL1,
		},
		{
			name: "one category with both properties asserted is AAL1, not AAL3",
			auth: Authentication{
				Subject:           "principal-0001",
				Factors:           []Factor{{Kind: FactorPossession, Method: "webauthn"}},
				PhishingResistant: true,
				HardwareBacked:    true,
			},
			want: AAL1,
		},
		{
			name: "knowledge and possession are two categories, so AAL2",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "totp"},
				},
			},
			want: AAL2,
		},
		{
			name: "knowledge and inherence are two categories, so AAL2",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorInherence, Method: "fingerprint"},
				},
			},
			want: AAL2,
		},
		{
			name: "two categories with only hardware backing asserted is AAL2",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
				},
				HardwareBacked: true,
			},
			want: AAL2,
		},
		{
			name: "two categories with only phishing resistance asserted is AAL2",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
				},
				PhishingResistant: true,
			},
			want: AAL2,
		},
		{
			name: "two categories with neither property asserted is AAL2 whatever the methods are called",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
				},
			},
			want: AAL2,
		},
		{
			name: "two categories with both properties asserted is AAL3",
			auth: Authentication{
				Subject: "principal-0001",
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
			name: "three categories with both properties asserted is AAL3",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorPossession, Method: "webauthn"},
					{Kind: FactorInherence, Method: "fingerprint"},
				},
				PhishingResistant: true,
				HardwareBacked:    true,
			},
			want: AAL3,
		},
		{
			name: "duplicated categories do not promote an AAL2 to AAL3",
			auth: Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorKnowledge, Method: "security_question"},
					{Kind: FactorPossession, Method: "totp"},
				},
			},
			want: AAL2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveAssurance(&tc.auth)
			if err != nil {
				t.Fatalf("DeriveAssurance() returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("DeriveAssurance() returned %q, want %q", got, tc.want)
			}
			// Whatever the derivation decides, it must be a rung of the
			// existing ladder rather than a level of its own invention: the
			// value is minted into a claim and compared against a policy
			// threshold downstream.
			if !knownAssurance(got) {
				t.Fatalf("DeriveAssurance() returned %q, which is not a rung of the ladder", got)
			}
		})
	}
}

// An authentication that established nothing has no level. Returning the
// lowest rung here would read as "authenticated weakly" when the truth is "not
// authenticated", which is exactly the overstatement this function exists to
// prevent.
func TestDeriveAssuranceRejectsAnAuthenticationWithNoFactors(t *testing.T) {
	auth := Authentication{Subject: "principal-0001"}

	level, err := DeriveAssurance(&auth)
	if !errors.Is(err, ErrNoFactors) {
		t.Fatalf("DeriveAssurance() returned error %v, want error matching %v", err, ErrNoFactors)
	}
	if level != "" {
		t.Fatalf("DeriveAssurance() returned level %q alongside an error, want no level", level)
	}
}

// Every rejection returns no level at all, so a caller that ignores the error
// cannot pick up a rung that was never reached.
func TestDeriveAssuranceReturnsNoLevelWithAnError(t *testing.T) {
	tests := []struct {
		name string
		auth *Authentication
		want error
	}{
		{
			name: "nil authentication",
			auth: nil,
			want: ErrNilAuthentication,
		},
		{
			name: "no subject",
			auth: &Authentication{
				Factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
			},
			want: ErrMissingAuthenticatedSubject,
		},
		{
			name: "empty factor slice",
			auth: &Authentication{
				Subject: "principal-0001",
				Factors: []Factor{},
			},
			want: ErrNoFactors,
		},
		{
			name: "unrecognised category",
			auth: &Authentication{
				Subject: "principal-0001",
				Factors: []Factor{
					{Kind: FactorKnowledge, Method: "password"},
					{Kind: FactorKind("something-else"), Method: "unclear"},
				},
			},
			want: ErrUnknownFactorKind,
		},
		{
			name: "factor with no method",
			auth: &Authentication{
				Subject: "principal-0001",
				Factors: []Factor{{Kind: FactorKnowledge}},
			},
			want: ErrMissingFactorMethod,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			level, err := DeriveAssurance(tc.auth)
			if !errors.Is(err, tc.want) {
				t.Fatalf("DeriveAssurance() returned error %v, want error matching %v", err, tc.want)
			}
			if level != "" {
				t.Fatalf("DeriveAssurance() returned level %q alongside an error, want no level", level)
			}
		})
	}
}

// An unrecognised category must not be quietly counted as an independent one.
// A derivation that counted it would promote a single-factor authentication to
// AAL2 on the strength of a typo.
func TestDeriveAssuranceDoesNotCountAnUnrecognisedCategory(t *testing.T) {
	auth := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorKind("possesion"), Method: "totp"},
		},
	}

	if _, err := DeriveAssurance(&auth); !errors.Is(err, ErrUnknownFactorKind) {
		t.Fatalf("DeriveAssurance() returned error %v, want error matching %v", err, ErrUnknownFactorKind)
	}
}

// The derivation is a pure function of the authentication: no clock, no
// configuration, no package state. The same authentication asked twice, and
// two equal authentications asked once each, must answer identically.
func TestDeriveAssuranceIsPure(t *testing.T) {
	auth := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorPossession, Method: "webauthn"},
		},
		PhishingResistant: true,
		HardwareBacked:    true,
	}
	twin := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorPossession, Method: "webauthn"},
		},
		PhishingResistant: true,
		HardwareBacked:    true,
	}

	first, err := DeriveAssurance(&auth)
	if err != nil {
		t.Fatalf("DeriveAssurance() returned error: %v", err)
	}
	second, err := DeriveAssurance(&auth)
	if err != nil {
		t.Fatalf("second DeriveAssurance() returned error: %v", err)
	}
	if first != second {
		t.Fatalf("DeriveAssurance() returned %q then %q for one authentication", first, second)
	}

	fromTwin, err := DeriveAssurance(&twin)
	if err != nil {
		t.Fatalf("DeriveAssurance() on an equal authentication returned error: %v", err)
	}
	if fromTwin != first {
		t.Fatalf("DeriveAssurance() returned %q for an equal authentication, want %q", fromTwin, first)
	}
}

// Deriving a level must not edit the record the level was derived from.
func TestDeriveAssuranceDoesNotMutateTheAuthentication(t *testing.T) {
	auth := wellFormedAuthentication()
	before := Authentication{
		Subject:           auth.Subject,
		Factors:           append([]Factor(nil), auth.Factors...),
		PhishingResistant: auth.PhishingResistant,
		HardwareBacked:    auth.HardwareBacked,
	}

	if _, err := DeriveAssurance(&auth); err != nil {
		t.Fatalf("DeriveAssurance() returned error: %v", err)
	}

	if !reflect.DeepEqual(auth, before) {
		t.Fatalf("DeriveAssurance() changed the authentication to %+v, want %+v", auth, before)
	}
}

// The properties qualify a multi-factor authentication; they do not substitute
// for one. Asserting both on a single-category authentication must not reach
// the top rung, however strong the one authenticator is.
func TestBothPropertiesDoNotSubstituteForASecondCategory(t *testing.T) {
	single := Authentication{
		Subject:           "principal-0001",
		Factors:           []Factor{{Kind: FactorPossession, Method: "webauthn"}},
		PhishingResistant: true,
		HardwareBacked:    true,
	}

	level, err := DeriveAssurance(&single)
	if err != nil {
		t.Fatalf("DeriveAssurance() returned error: %v", err)
	}
	if level == AAL3 {
		t.Fatalf("DeriveAssurance() returned AAL3 for a single-category authentication")
	}
	if level != AAL1 {
		t.Fatalf("DeriveAssurance() returned %q, want %q", level, AAL1)
	}
}

// AAL3 is reachable only with both properties asserted. Neither alone lifts a
// multi-factor authentication off AAL2, because neither is inferable from the
// other and neither is inferable from a method name.
func TestAAL3RequiresBothPropertiesAsserted(t *testing.T) {
	base := Authentication{
		Subject: "principal-0001",
		Factors: []Factor{
			{Kind: FactorKnowledge, Method: "password"},
			{Kind: FactorPossession, Method: "webauthn"},
		},
	}

	tests := []struct {
		name              string
		phishingResistant bool
		hardwareBacked    bool
		want              string
	}{
		{name: "neither asserted", want: AAL2},
		{name: "only phishing resistance asserted", phishingResistant: true, want: AAL2},
		{name: "only hardware backing asserted", hardwareBacked: true, want: AAL2},
		{name: "both asserted", phishingResistant: true, hardwareBacked: true, want: AAL3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := base
			auth.PhishingResistant = tc.phishingResistant
			auth.HardwareBacked = tc.hardwareBacked

			got, err := DeriveAssurance(&auth)
			if err != nil {
				t.Fatalf("DeriveAssurance() returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("DeriveAssurance() returned %q, want %q", got, tc.want)
			}
		})
	}
}

// The derived level is what a caller passes to the minter, so it has to be a
// value the minter accepts. If the two ever disagree, every issuance fails at
// run time on a level this package produced itself.
func TestDerivedLevelsAreAcceptedByTheMintContract(t *testing.T) {
	authentications := []Authentication{
		{
			Subject: "principal-0001",
			Factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
		},
		{
			Subject: "principal-0001",
			Factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "totp"},
			},
		},
		{
			Subject: "principal-0001",
			Factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "webauthn"},
			},
			PhishingResistant: true,
			HardwareBacked:    true,
		},
	}

	for _, auth := range authentications {
		level, err := DeriveAssurance(&auth)
		if err != nil {
			t.Fatalf("DeriveAssurance() returned error: %v", err)
		}
		if !knownAssurance(level) {
			t.Fatalf("DeriveAssurance() returned %q, which the mint contract rejects", level)
		}
	}
}
