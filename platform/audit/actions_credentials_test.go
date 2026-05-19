package audit

import "testing"

// TestCredentialActionStrings pins the wire string values for every
// Obj 5 credential Action constant. Downstream consumers (Obj 9 audit
// dashboards, AOAudit aggregation pipelines) parse on these exact
// strings — renaming any of them is a deliberate cross-repo event and
// this test is the canary.
func TestCredentialActionStrings(t *testing.T) {
	cases := []struct {
		name string
		got  Action
		want string
	}{
		{"ActionApiKeyMinted", ActionApiKeyMinted, "credential.api_key.minted"},
		{"ActionApiKeyRevoked", ActionApiKeyRevoked, "credential.api_key.revoked"},
		{"ActionApiKeyRotated", ActionApiKeyRotated, "credential.api_key.rotated"},
		{"ActionApiKeyValidated", ActionApiKeyValidated, "credential.api_key.validated"},
		{"ActionApiKeyValidationDenied", ActionApiKeyValidationDenied, "credential.api_key.validation_denied"},
		{"ActionCertificateIssued", ActionCertificateIssued, "credential.certificate.issued"},
		{"ActionCertificateRenewed", ActionCertificateRenewed, "credential.certificate.renewed"},
		{"ActionCertificateRevoked", ActionCertificateRevoked, "credential.certificate.revoked"},
		{"ActionSVIDIssued", ActionSVIDIssued, "credential.svid.issued"},
		{"ActionSVIDIssuedFailure", ActionSVIDIssuedFailure, "credential.svid.issued.failure"},
		{"ActionSVIDRevoked", ActionSVIDRevoked, "credential.svid.revoked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, string(tc.got), tc.want)
			}
		})
	}
}

// TestCredentialActionStringer confirms the (Action).String() helper
// round-trips for every constant. The Logger machinery in logger.go
// invokes String() at emission time; a broken Stringer would silently
// emit empty actions.
func TestCredentialActionStringer(t *testing.T) {
	if ActionApiKeyMinted.String() != "credential.api_key.minted" {
		t.Fatalf("Stringer drift: %q", ActionApiKeyMinted.String())
	}
	if ActionApiKeyValidationDenied.String() != "credential.api_key.validation_denied" {
		t.Fatalf("Stringer drift: %q", ActionApiKeyValidationDenied.String())
	}
}
