package logingov

import (
	"errors"
	"testing"
)

// TestMapACR_Exhaustive verifies the ACR -> AOID assurance enum mapping
// covers all 7 known Login.gov ACR values plus the unknown-value fallback.
// This table is the single source of truth for ACR mapping in Eden.
func TestMapACR_Exhaustive(t *testing.T) {
	cases := []struct {
		name string
		acr  string
		want string
	}{
		{"auth-only", "urn:acr.login.gov:auth-only", "ial_1"},
		{"verified-no-match", "urn:acr.login.gov:verified", "verified_no_match"},
		{"verified-facial-match-required", "urn:acr.login.gov:verified-facial-match-required", "ial_2"},
		{"verified-facial-match-preferred", "urn:acr.login.gov:verified-facial-match-preferred", "ial_2_preferred"},
		{"aal2", "http://idmanagement.gov/ns/assurance/aal/2", "aal_2"},
		{"aal2-phishing-resistant", "http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true", "aal_3"},
		{"aal2-hspd12", "http://idmanagement.gov/ns/assurance/aal/2?hspd12=true", "aal_3_piv"},
		{"unknown", "urn:acr.login.gov:bogus-future-value", "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapACR(tc.acr)
			if got != tc.want {
				t.Fatalf("mapACR(%q) = %q, want %q", tc.acr, got, tc.want)
			}
		})
	}
}

// TestSentinelErrors_AreExported is a compile-time + runtime guard that
// the package-level sentinels exist and are real errors that callers can
// branch on via errors.Is.
func TestSentinelErrors_AreExported(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"ErrSigningKeyMissing", ErrSigningKeyMissing},
		{"ErrSigningKeyTooShort", ErrSigningKeyTooShort},
		{"ErrNonceMismatch", ErrNonceMismatch},
		{"ErrACRMismatch", ErrACRMismatch},
		{"ErrTokenEndpointStatus", ErrTokenEndpointStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("sentinel %s is nil", tc.name)
			}
			// errors.Is must work with itself.
			if !errors.Is(tc.err, tc.err) {
				t.Fatalf("errors.Is(%s, %s) = false", tc.name, tc.name)
			}
			if tc.err.Error() == "" {
				t.Fatalf("sentinel %s has empty message", tc.name)
			}
		})
	}
}
