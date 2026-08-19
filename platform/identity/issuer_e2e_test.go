package identity

import (
	"errors"
	"testing"

	"slices"
)

// These are the acceptance tests for an application acting as its own issuer.
// The unit tests alongside them each pin one layer against its own
// construction; these assemble the layers the way a deployment does — a login
// on one side, a published key set in between, a verifier on the other — and
// assert on what comes out the far end.
//
// Only the two consumer seams are faked, because a consumer supplies those and
// this package genuinely has no implementation of them. Everything else on the
// path is the real thing: the real issuer, the real minter, the real key-set
// handler over a real server, the real remote key source, the real verifier. A
// round trip that substituted any of those would prove only that the
// substitutes agree with each other.

// verifierOverThePublishedKeySet wires the far end of the round trip: it serves
// the issuer's key set over HTTP, points a remote key source at it, and returns
// a verifier that trusts the issuer.
//
// The verifier is built on the DEFAULT accepted-version set, deliberately. That
// is the assertion that an issuer emits a version already-deployed consumers
// take. Widening it here would hide exactly the mismatch this package exists to
// surface: minting outside the set the strictest deployed consumer accepts
// strands that consumer in the field, and turns a library change into a
// coordinated multi-service deploy.
func verifierOverThePublishedKeySet(t *testing.T, signer KeySetSigner) *Verifier {
	t.Helper()

	keys, err := NewRemoteJWKS(newKeySetServer(t, signer))
	if err != nil {
		t.Fatalf("NewRemoteJWKS returned error: %v", err)
	}
	return newTestVerifier(t, []TrustedIssuer{{Issuer: issuerUnderTest, Keys: keys}})
}

// TestAnIssuedContextVerifiesThroughThePublishedKeySet is the acceptance proof
// for the objective: a credential goes in one end and a verified claim set
// comes out the other, across a real HTTP boundary.
func TestAnIssuedContextVerifiesThroughThePublishedKeySet(t *testing.T) {
	harness := newHarness(t, defaultHarnessConfig())

	token, err := harness.issuer.Issue(issueCtx(), aCredential())
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	claims, err := verifierOverThePublishedKeySet(t, harness.signer).Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify of an issued context returned error: %v", err)
	}

	// The subject is the one that was PROVED, not the handle that was claimed.
	if claims.Subject != authenticatedSubject {
		t.Errorf("verified sub = %q, want %q", claims.Subject, authenticatedSubject)
	}
	if claims.Subject == credentialHandle {
		t.Error("the verified subject is the handle the caller claimed, not the one " +
			"the verifier established")
	}
	if claims.Tenant != "acme" {
		t.Errorf("verified tnt = %q, want %q", claims.Tenant, "acme")
	}
	if want := []string{"reports.read", "reports.write"}; !slices.Equal(claims.Entitlements, want) {
		t.Errorf("verified ent = %v, want %v", claims.Entitlements, want)
	}
	// The default harness authenticates with two categories and asserts both
	// properties, which is the top rung.
	if claims.AAL != AAL3 {
		t.Errorf("verified aal = %q, want %q", claims.AAL, AAL3)
	}
	if claims.Issuer != issuerUnderTest {
		t.Errorf("verified iss = %q, want %q", claims.Issuer, issuerUnderTest)
	}
	if claims.CtxVer != Version {
		t.Errorf("verified ctx_ver = %d, want %d — an issuer must emit a version "+
			"a verifier on the default accepted set already takes", claims.CtxVer, Version)
	}
}

// TestARejectedCredentialProducesNothingToVerify checks the negative path all
// the way through: a failed login yields no token, so there is nothing for the
// far end to accept.
func TestARejectedCredentialProducesNothingToVerify(t *testing.T) {
	cfg := defaultHarnessConfig()
	cfg.verifyErr = errors.New("credential rejected")
	harness := newHarness(t, cfg)

	token, err := harness.issuer.Issue(issueCtx(), aCredential())
	if err == nil {
		t.Fatal("Issue returned no error for a rejected credential")
	}
	if token != "" {
		t.Fatalf("Issue returned a token for a rejected credential: %q", token)
	}

	// Nothing was minted, so nothing reached the resolver either — a failed
	// login must not disclose what an identifier would have been entitled to.
	if subjects := harness.rec.resolvedSubjects(); len(subjects) != 0 {
		t.Errorf("the claims resolver was asked about %v after a rejected credential", subjects)
	}

	// And the far end refuses the empty string rather than treating it as
	// anything else.
	if _, err := verifierOverThePublishedKeySet(t, harness.signer).Verify(t.Context(), token); err != ErrInvalidContext {
		t.Errorf("Verify of an empty token = %v, want %v", err, ErrInvalidContext)
	}
}

// TestEachAssuranceRungSurvivesTheRoundTrip pins that the level established at
// the login end is the level read at the verifying end.
//
// This is the claim most likely to be wrong in a way nobody notices: it feeds a
// downstream authorization decision and an audit record, where a level nobody
// achieved is indistinguishable from one that was. A single-factor login has to
// still look single-factor after crossing the wire.
func TestEachAssuranceRungSurvivesTheRoundTrip(t *testing.T) {
	for _, rung := range []struct {
		name              string
		factors           []Factor
		phishingResistant bool
		hardwareBacked    bool
		want              string
	}{
		{
			name:    "a password-only login stays single-factor",
			factors: []Factor{{Kind: FactorKnowledge, Method: "password"}},
			want:    AAL1,
		},
		{
			name: "two prompts in one category stay single-factor",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorKnowledge, Method: "security_question"},
			},
			want: AAL1,
		},
		{
			name: "a password and a one-time code are multi-factor",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "totp"},
			},
			want: AAL2,
		},
		{
			name: "multi-factor without both asserted properties stops at AAL2",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "webauthn"},
			},
			hardwareBacked: true,
			want:           AAL2,
		},
		{
			name: "multi-factor with both asserted properties reaches the top rung",
			factors: []Factor{
				{Kind: FactorKnowledge, Method: "password"},
				{Kind: FactorPossession, Method: "webauthn"},
			},
			phishingResistant: true,
			hardwareBacked:    true,
			want:              AAL3,
		},
	} {
		t.Run(rung.name, func(t *testing.T) {
			cfg := defaultHarnessConfig()
			cfg.auth = &Authentication{
				Subject:           authenticatedSubject,
				Factors:           rung.factors,
				PhishingResistant: rung.phishingResistant,
				HardwareBacked:    rung.hardwareBacked,
			}
			harness := newHarness(t, cfg)

			token, err := harness.issuer.Issue(issueCtx(), aCredential())
			if err != nil {
				t.Fatalf("Issue returned error: %v", err)
			}

			claims, err := verifierOverThePublishedKeySet(t, harness.signer).Verify(t.Context(), token)
			if err != nil {
				t.Fatalf("Verify returned error: %v", err)
			}
			if claims.AAL != rung.want {
				t.Errorf("verified aal = %q, want %q — the level established at the "+
					"login end is not the level read at the verifying end",
					claims.AAL, rung.want)
			}
		})
	}
}
