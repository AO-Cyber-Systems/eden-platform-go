package auth

// Test-only seams for the external auth_test package (TRD 27-01 PKCE tests).
//
// export_*_test.go files are compiled ONLY under `go test` and are never part
// of the shipped package — so exposing these internals here keeps the
// production surface minimal (the public SSOService API is unchanged) while
// letting sso_pkce_test.go assert on the state JWT internals it must verify:
// that the PKCE verifier is threaded through createStateJWT/parseStateJWT.

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// StateVerifierForTest parses the given state JWT and returns the PKCE verifier
// carried in it (empty string when the state predates PKCE).
func (s *SSOService) StateVerifierForTest(t *testing.T, stateJWT string) string {
	t.Helper()
	_, _, _, verifier := s.ParseStateForTest(t, stateJWT)
	return verifier
}

// ParseStateForTest exposes parseStateJWT to the external test package.
func (s *SSOService) ParseStateForTest(t *testing.T, stateJWT string) (uuid.UUID, string, string, string) {
	t.Helper()
	companyID, provider, redirectURI, verifier, err := s.parseStateJWT(stateJWT)
	if err != nil {
		t.Fatalf("parseStateJWT: %v", err)
	}
	return companyID, provider, redirectURI, verifier
}

// LegacyStateForTest builds a state JWT in the OLD 3-field
// companyID|provider|redirectURI format (no PKCE verifier), so the back-compat
// path (an in-flight redirect issued just before deploy) can be exercised.
func (s *SSOService) LegacyStateForTest(t *testing.T, companyID uuid.UUID, provider, redirectURI string) string {
	t.Helper()
	subject := companyID.String() + "|" + provider + "|" + redirectURI
	state, err := s.jwtManager.CreateShortLivedToken(subject, 10*time.Minute)
	if err != nil {
		t.Fatalf("create legacy state: %v", err)
	}
	return state
}
