package social

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 09-03 custom-OAuth2 provider tests (Apple / Facebook / X). These exercise the
// PURE mappers and the Apple ES256 client-secret claim shape only — NO live HTTP
// is performed (no real token endpoints, no JWKS fetch). Apple id_token JWKS
// verification and the provider code exchanges are integration-only and are not
// unit-tested here (they require Apple's network/keys).

// newCustomTestService builds a SocialAuthService with the three custom-OAuth2
// providers (apple/facebook/x) registered and a populated redirect allowlist,
// reusing the hand-built fakes from service_test.go.
func newCustomTestService(t *testing.T) (*SocialAuthService, *fakeSocialStore, *fakeAuthStore) {
	t.Helper()
	social := newFakeSocialStore()
	users := newFakeAuthStore()
	svc := NewSocialAuthService(social, users, newTestJWT(t), "https://example.test",
		[]string{"com.justindonnaruma.app://auth/social/callback", "http://localhost"})
	svc.RegisterCustomProvider("apple", "com.justindonnaruma.service", "")
	svc.RegisterCustomProvider("facebook", "fb-app-id", "fb-app-secret")
	svc.RegisterCustomProvider("x", "x-client-id", "")
	return svc, social, users
}

// testECKeyPEM generates a fresh P-256 EC private key in PKCS#8 PEM form,
// matching the format Apple issues its .p8 keys in. Used to drive
// generateAppleClientSecret without a real Apple key.
func testECKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// Test list case 1: Apple client secret is an ES256 JWT whose claims carry
// iss=TEAM_ID, sub=SERVICES_ID, aud=https://appleid.apple.com, exp ≤ now+180d,
// and is signed with alg=ES256.
func TestGenerateAppleClientSecret_ClaimShape(t *testing.T) {
	keyPEM := testECKeyPEM(t)

	secret, err := generateAppleClientSecret(keyPEM, "TEAMID1234", "com.justindonnaruma.service", "KEYID5678")
	if err != nil {
		t.Fatalf("generateAppleClientSecret: %v", err)
	}

	// Parse WITHOUT verifying the signature (we don't hold Apple's pub key here);
	// assert the header alg + the registered claims shape.
	parser := jwt.NewParser()
	var claims jwt.RegisteredClaims
	tok, _, err := parser.ParseUnverified(secret, &claims)
	if err != nil {
		t.Fatalf("parse client secret: %v", err)
	}

	if alg, _ := tok.Header["alg"].(string); alg != "ES256" {
		t.Errorf("alg = %q, want ES256", alg)
	}
	if kid, _ := tok.Header["kid"].(string); kid != "KEYID5678" {
		t.Errorf("kid header = %q, want KEYID5678", kid)
	}
	if claims.Issuer != "TEAMID1234" {
		t.Errorf("iss = %q, want TEAMID1234 (team id)", claims.Issuer)
	}
	if claims.Subject != "com.justindonnaruma.service" {
		t.Errorf("sub = %q, want services id", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "https://appleid.apple.com" {
		t.Errorf("aud = %v, want [https://appleid.apple.com]", claims.Audience)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("exp missing")
	}
	if claims.ExpiresAt.After(time.Now().Add(180 * 24 * time.Hour)) {
		t.Errorf("exp %v exceeds now+180d (Apple max)", claims.ExpiresAt.Time)
	}
	if claims.IssuedAt == nil {
		t.Error("iat missing")
	}
}

// Bad key input is surfaced as an error, not a panic.
func TestGenerateAppleClientSecret_BadKey(t *testing.T) {
	if _, err := generateAppleClientSecret("not-a-pem", "T", "S", "K"); err == nil {
		t.Fatal("expected error for malformed private key, got nil")
	}
}

// Test list case 2: appleClaimsToIdentity sets EmailVerified=true and
// ProviderSub=sub; the first-auth name from the form `user` field populates
// DisplayName.
func TestAppleClaimsToIdentity_FirstAuthName(t *testing.T) {
	claims := map[string]any{
		"sub":            "apple-sub-001",
		"email":          "Relay@privaterelay.appleid.com",
		"email_verified": "true",
	}
	formUser := `{"name":{"firstName":"Ada","lastName":"Lovelace"},"email":"relay@privaterelay.appleid.com"}`

	id := appleClaimsToIdentity(claims, formUser)

	if id.Provider != "apple" {
		t.Errorf("Provider = %q, want apple", id.Provider)
	}
	if id.ProviderSub != "apple-sub-001" {
		t.Errorf("ProviderSub = %q, want apple-sub-001", id.ProviderSub)
	}
	if !id.EmailVerified {
		t.Error("Apple identity must be EmailVerified=true (Apple verifies relay emails)")
	}
	if id.Email != "relay@privaterelay.appleid.com" {
		t.Errorf("Email = %q, want lowercased relay address", id.Email)
	}
	if id.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want \"Ada Lovelace\" from first-auth form user field", id.DisplayName)
	}
}

// Test list case 2 (cont.): absent name leaves DisplayName empty (repeat sign-in,
// where Apple omits the user field) — Provision keeps the existing display_name.
func TestAppleClaimsToIdentity_NoName(t *testing.T) {
	claims := map[string]any{
		"sub":            "apple-sub-002",
		"email":          "person@example.com",
		"email_verified": true,
	}

	id := appleClaimsToIdentity(claims, "")

	if id.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty on repeat auth (no form user field)", id.DisplayName)
	}
	if !id.EmailVerified {
		t.Error("Apple identity must be EmailVerified=true")
	}
	if id.ProviderSub != "apple-sub-002" {
		t.Errorf("ProviderSub = %q, want apple-sub-002", id.ProviderSub)
	}

	// Malformed form user JSON must not break mapping; DisplayName stays empty.
	id2 := appleClaimsToIdentity(claims, "{not json")
	if id2.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty for malformed form user JSON", id2.DisplayName)
	}
}

// Test list case 3: Facebook graph {id,name,email} → verified=false Identity.
func TestFacebookToIdentity_WithEmail(t *testing.T) {
	id := facebookToIdentity("fb-123", "Grace Hopper", "Grace@Example.com")

	if id.Provider != "facebook" {
		t.Errorf("Provider = %q, want facebook", id.Provider)
	}
	if id.ProviderSub != "fb-123" {
		t.Errorf("ProviderSub = %q, want fb-123", id.ProviderSub)
	}
	if id.Email != "grace@example.com" {
		t.Errorf("Email = %q, want lowercased grace@example.com", id.Email)
	}
	if id.EmailVerified {
		t.Error("Facebook email must be EmailVerified=false (Graph does not verify)")
	}
	if id.DisplayName != "Grace Hopper" {
		t.Errorf("DisplayName = %q, want Grace Hopper", id.DisplayName)
	}
}

// Test list case 3 (cont.): missing email → Email "" (email-less Provision path).
func TestFacebookToIdentity_NoEmail(t *testing.T) {
	id := facebookToIdentity("fb-456", "No Email User", "")

	if id.Email != "" {
		t.Errorf("Email = %q, want empty when Facebook returns no email", id.Email)
	}
	if id.EmailVerified {
		t.Error("Facebook with no email must be EmailVerified=false")
	}
	if id.ProviderSub != "fb-456" {
		t.Errorf("ProviderSub = %q, want fb-456", id.ProviderSub)
	}
}

// Test list case 4: X users/me {data:{id,name}} → ALWAYS email-less, unverified.
func TestXToIdentity_AlwaysEmailLess(t *testing.T) {
	id := xToIdentity("x-789", "Alan Turing")

	if id.Provider != "x" {
		t.Errorf("Provider = %q, want x", id.Provider)
	}
	if id.ProviderSub != "x-789" {
		t.Errorf("ProviderSub = %q, want x-789", id.ProviderSub)
	}
	if id.Email != "" {
		t.Errorf("Email = %q, want ALWAYS empty for X (API v2 never returns email)", id.Email)
	}
	if id.EmailVerified {
		t.Error("X identity must be EmailVerified=false (always email-less)")
	}
	if id.DisplayName != "Alan Turing" {
		t.Errorf("DisplayName = %q, want Alan Turing", id.DisplayName)
	}
}

// Test list case 5: PKCE round-trip. initiateX generates a verifier, stores it in
// the state JWT, and the AuthCodeURL carries an S256 code_challenge. parseStateJWT
// recovers the SAME verifier for the callback Exchange.
func TestInitiateX_PKCERoundTrip(t *testing.T) {
	svc, _, _ := newCustomTestService(t)

	redirectURI := "com.justindonnaruma.app://auth/social/callback"
	authURL, state, err := svc.initiateX(redirectURI)
	if err != nil {
		t.Fatalf("initiateX: %v", err)
	}
	if state == "" {
		t.Fatal("expected non-empty state JWT")
	}

	provider, gotRedirect, verifier, _, err := svc.parseStateJWT(state)
	if err != nil {
		t.Fatalf("parseStateJWT: %v", err)
	}
	if provider != "x" {
		t.Errorf("state provider = %q, want x", provider)
	}
	if gotRedirect != redirectURI {
		t.Errorf("state redirect = %q, want %q", gotRedirect, redirectURI)
	}
	if verifier == "" {
		t.Fatal("PKCE verifier must be stored in the state JWT for server-side PKCE")
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if !strings.Contains(u.Host, "twitter.com") && !strings.Contains(u.Host, "x.com") {
		t.Errorf("auth URL host = %q, want X/Twitter authorize host", u.Host)
	}
	q := u.Query()
	if q.Get("code_challenge") == "" {
		t.Error("auth URL must carry a PKCE code_challenge")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("state") != state {
		t.Errorf("auth URL state = %q, want the returned state JWT", q.Get("state"))
	}
	scope := q.Get("scope")
	for _, want := range []string{"tweet.read", "users.read", "offline.access"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope %q missing %q", scope, want)
		}
	}
}

// initiateX rejects a non-allowlisted redirect at initiate time (Pitfall 4).
func TestInitiateX_RejectsUnallowlistedRedirect(t *testing.T) {
	svc, _, _ := newCustomTestService(t)
	if _, _, err := svc.initiateX("https://evil.example.com/steal"); err == nil {
		t.Fatal("expected error for non-allowlisted redirect_uri, got nil")
	}
}

// Test list case 6: POST /auth/social/facebook/deletion returns 200 with a JSON
// body carrying {url, confirmation_code} (Meta's required shape) and records an
// audit log entry. It performs NO real deletion.
func TestFacebookDeletion_Returns200WithConfirmationAndAudit(t *testing.T) {
	svc, _, users := newCustomTestService(t)

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	body := strings.NewReader("signed_request=fake.payload")
	req := httptest.NewRequest(http.MethodPost, "/auth/social/facebook/deletion", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		URL              string `json:"url"`
		ConfirmationCode string `json:"confirmation_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON body %q: %v", rec.Body.String(), err)
	}
	if resp.ConfirmationCode == "" {
		t.Error("response body missing confirmation_code (Meta requires it)")
	}
	if resp.URL == "" {
		t.Error("response body missing status url (Meta requires it)")
	}

	// The deletion request must be audited (best-effort, but the fake records it).
	if len(users.auditLogs) == 0 {
		t.Fatal("expected a deletion-request audit log entry, got none")
	}
	if users.auditLogs[0].Action != "social.facebook.deletion_request" {
		t.Errorf("audit action = %q, want social.facebook.deletion_request", users.auditLogs[0].Action)
	}
}
