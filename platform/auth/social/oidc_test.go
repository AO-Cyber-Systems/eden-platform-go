package social

import (
	"net/url"
	"strings"
	"testing"
)

// Test list (09-02):
//  1. InitiateOIDC(google, redirectURI) returns an auth URL on accounts.google.com
//     carrying client_id, redirect_uri=<server callback>, scope incl. openid email,
//     and a non-empty state.
//  2. claimsToIdentity(google, {sub, email, email_verified:true, name}) → verified.
//  3. claimsToIdentity(microsoft, {sub, email, name} no email_verified) → unverified.
//  4. HandleCallback with a non-allowlisted redirect_uri in state → error BEFORE
//     issuing any tokens.

// newOIDCTestService builds a SocialAuthService with both OIDC providers
// registered and a populated redirect allowlist, using the same hand-built fakes
// as service_test.go.
func newOIDCTestService(t *testing.T) (*SocialAuthService, *fakeSocialStore, *fakeAuthStore) {
	t.Helper()
	social := newFakeSocialStore()
	users := newFakeAuthStore()
	svc := NewSocialAuthService(social, users, newTestJWT(t), "https://example.test",
		[]string{"com.justindonnaruma.app://auth/social/callback", "http://localhost"})
	// Register the two OIDC providers with hand-set client credentials (no env
	// dependency in the test).
	svc.RegisterOIDCProvider("google", "google-client-id", "google-secret")
	svc.RegisterOIDCProvider("microsoft", "ms-client-id", "ms-secret")
	return svc, social, users
}

// Case 1: Google initiate produces a correct authorization URL + non-empty state.
func TestInitiateOIDC_Google_BuildsAuthURL(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	redirectURI := "com.justindonnaruma.app://auth/social/callback"
	authURL, state, err := svc.InitiateOIDC(t.Context(), "google", redirectURI)
	if err != nil {
		t.Fatalf("InitiateOIDC(google): %v", err)
	}
	if state == "" {
		t.Error("expected non-empty state JWT")
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("auth URL host = %q, want accounts.google.com", u.Host)
	}
	q := u.Query()
	if q.Get("client_id") != "google-client-id" {
		t.Errorf("client_id = %q, want google-client-id", q.Get("client_id"))
	}
	// The redirect_uri registered with the provider is the SERVER callback, not
	// the app deep-link (which travels in the state JWT).
	if got := q.Get("redirect_uri"); got != "https://example.test/auth/social/callback" {
		t.Errorf("redirect_uri = %q, want https://example.test/auth/social/callback", got)
	}
	scope := q.Get("scope")
	if !strings.Contains(scope, "openid") || !strings.Contains(scope, "email") {
		t.Errorf("scope = %q, want to contain openid and email", scope)
	}
	if q.Get("state") == "" {
		t.Error("auth URL must carry a non-empty state param")
	}
}

// Case 1b: a non-allowlisted redirect_uri is rejected at initiate time
// (defense-in-depth, Pitfall 4).
func TestInitiateOIDC_RejectsUnallowlistedRedirect(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	_, _, err := svc.InitiateOIDC(t.Context(), "google", "https://evil.example.com/steal")
	if err == nil {
		t.Fatal("expected error for non-allowlisted redirect_uri, got nil")
	}
}

// Case 1c: an unknown provider is rejected.
func TestInitiateOIDC_RejectsUnknownProvider(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	_, _, err := svc.InitiateOIDC(t.Context(), "myspace", "http://localhost/cb")
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

// Case 2: Google claims with email_verified=true map to a verified Identity.
func TestClaimsToIdentity_Google_Verified(t *testing.T) {
	id := claimsToIdentity("google", map[string]any{
		"sub":            "google-sub-123",
		"email":          "user@gmail.com",
		"email_verified": true,
		"name":           "Real Name",
	})
	if id.Provider != "google" {
		t.Errorf("Provider = %q, want google", id.Provider)
	}
	if id.ProviderSub != "google-sub-123" {
		t.Errorf("ProviderSub = %q, want google-sub-123", id.ProviderSub)
	}
	if id.Email != "user@gmail.com" {
		t.Errorf("Email = %q, want user@gmail.com", id.Email)
	}
	if !id.EmailVerified {
		t.Error("EmailVerified = false, want true (Google email_verified=true)")
	}
	if id.DisplayName != "Real Name" {
		t.Errorf("DisplayName = %q, want Real Name", id.DisplayName)
	}
}

// Case 2b: Google claims with email_verified as the string "true" (some IdPs
// stringify booleans) must still map to verified.
func TestClaimsToIdentity_Google_StringifiedVerified(t *testing.T) {
	id := claimsToIdentity("google", map[string]any{
		"sub":            "google-sub-456",
		"email":          "str@gmail.com",
		"email_verified": "true",
	})
	if !id.EmailVerified {
		t.Error("EmailVerified = false for email_verified=\"true\", want true")
	}
}

// Case 3: Microsoft /common has NO email_verified claim → EmailVerified must be
// false so Provision never auto-links (Pitfall 7).
func TestClaimsToIdentity_Microsoft_Unverified(t *testing.T) {
	id := claimsToIdentity("microsoft", map[string]any{
		"sub":   "ms-sub-789",
		"email": "user@outlook.com",
		"name":  "MS User",
	})
	if id.Provider != "microsoft" {
		t.Errorf("Provider = %q, want microsoft", id.Provider)
	}
	if id.ProviderSub != "ms-sub-789" {
		t.Errorf("ProviderSub = %q, want ms-sub-789", id.ProviderSub)
	}
	if id.Email != "user@outlook.com" {
		t.Errorf("Email = %q, want user@outlook.com", id.Email)
	}
	if id.EmailVerified {
		t.Error("SECURITY: Microsoft identity must be EmailVerified=false (no email_verified claim)")
	}
}

// Case 4: HandleCallback with a state whose redirect_uri is NOT allowlisted must
// return an error BEFORE any token exchange / issuance.
func TestHandleCallback_RejectsUnallowlistedRedirect_NoTokens(t *testing.T) {
	svc, _, users := newOIDCTestService(t)

	// Forge a state JWT carrying a non-allowlisted redirect_uri. Signed by the
	// same JWTManager, so it parses cleanly — the guard must still reject it.
	state, err := svc.createStateJWT("google", "https://evil.example.com/steal", "", "nonce")
	if err != nil {
		t.Fatalf("createStateJWT: %v", err)
	}

	_, _, err = svc.HandleCallback(t.Context(), "any-code", state)
	if err == nil {
		t.Fatal("expected error for non-allowlisted redirect_uri in state, got nil")
	}
	// No code exchange should have happened, hence no refresh token recorded.
	if len(users.refreshTokens) != 0 {
		t.Errorf("SECURITY: no tokens must be issued for a rejected redirect, got %d", len(users.refreshTokens))
	}
	if users.createdUsers != 0 {
		t.Errorf("no user should be created for a rejected redirect, got %d", users.createdUsers)
	}
}
