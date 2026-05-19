package logingov

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/oidcrp"
)

// newStubLoginGovOP returns an httptest.Server that serves the Login.gov-
// shaped discovery document loaded from testdata/stub_discovery.json with
// the __STUB__ placeholder rewritten to the test server's own URL.
//
// The returned server also serves an empty JWKS for /api/openid_connect/certs
// and a placeholder token handler that returns 404 — Task 4's exchange tests
// will provide a server with a real token handler. The Task 3 tests only
// exercise discovery + URL building, which only need the discovery doc.
func newStubLoginGovOP(t *testing.T) *httptest.Server {
	t.Helper()
	tmpl, err := os.ReadFile("testdata/stub_discovery.json")
	if err != nil {
		t.Fatalf("read stub_discovery.json: %v", err)
	}

	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(string(tmpl), "__STUB__", srv.URL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/api/openid_connect/certs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
	mux.HandleFunc("/api/openid_connect/token", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // placeholder; Task 4 substitutes a real handler
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fortyThreeBytePKCE returns a string of length 43 — the minimum
// code_verifier length per RFC 7636 §4.1 and what oidcrp.BuildAuthURL
// enforces. We use a fixed value (not random) in tests so URL assertions
// are reproducible.
func fortyThreeBytePKCE() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 43 'a's
}

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

// TestNewClient_HappyPath stands up a stub Login.gov OP, builds a Client
// against it, and confirms discovery populated the token endpoint URL.
func TestNewClient_HappyPath(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)

	pc := oidcrp.NewProviderCache()
	vc := oidcrp.NewVerifierCache()

	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "urn:gov:gsa:openidconnect:test:aoid",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/federate/logingov/callback",
		SigningKey:  key,
		SigningKID:  "kid-1",
	}
	ctx := context.Background()
	c, err := NewClient(ctx, cfg, pc, vc)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned nil client")
	}
	wantToken := srv.URL + "/api/openid_connect/token"
	if c.tokenEndpoint != wantToken {
		t.Fatalf("tokenEndpoint = %q, want %q", c.tokenEndpoint, wantToken)
	}
	if c.provider == nil {
		t.Fatal("provider not populated")
	}
	if c.oauthConfig == nil || c.oauthConfig.ClientID != cfg.ClientID {
		t.Fatalf("oauthConfig.ClientID = %v, want %v", c.oauthConfig, cfg.ClientID)
	}
}

func TestNewClient_NilSigningKey(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   "https://example.invalid",
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  nil,
	}
	_, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if !errors.Is(err, ErrSigningKeyMissing) {
		t.Fatalf("want ErrSigningKeyMissing, got %v", err)
	}
}

func TestNewClient_KeyTooShort(t *testing.T) {
	key := testRSAKey(t, 1024)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   "https://example.invalid",
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
	}
	_, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if !errors.Is(err, ErrSigningKeyTooShort) {
		t.Fatalf("want ErrSigningKeyTooShort, got %v", err)
	}
}

// TestNewClient_DefaultsScopes confirms that empty cfg.Scopes is replaced
// by ["openid","email"] — Login.gov's required minimum.
func TestNewClient_DefaultsScopes(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
		Scopes:      nil, // explicitly nil; expect default fill-in
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if len(c.cfg.Scopes) != 2 || c.cfg.Scopes[0] != "openid" || c.cfg.Scopes[1] != "email" {
		t.Fatalf("default scopes = %v, want [openid email]", c.cfg.Scopes)
	}
}

// TestNewClient_DefaultsHTTPClient confirms a nil cfg.HTTPClient is
// replaced with http.DefaultClient. We verify by pointer identity, which
// is the strict contract.
func TestNewClient_DefaultsHTTPClient(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
		HTTPClient:  nil,
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.httpClient != http.DefaultClient {
		t.Fatalf("httpClient = %p, want http.DefaultClient %p", c.httpClient, http.DefaultClient)
	}
}

// TestBuildAuthURL_DefaultACR confirms the default acr_values is the
// auth-only URN when cfg.ACRValues is empty and extraACR is nil.
func TestBuildAuthURL_DefaultACR(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	authURL, err := c.BuildAuthURL("state-1", "nonce-1", fortyThreeBytePKCE(), nil)
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	acr := u.Query().Get("acr_values")
	if acr != "urn:acr.login.gov:auth-only" {
		t.Fatalf("acr_values = %q, want %q", acr, "urn:acr.login.gov:auth-only")
	}
}

// TestBuildAuthURL_ConfigPlusExtraACR confirms cfg.ACRValues is the
// base, and extraACR values are appended space-separated per OIDC
// acr_values syntax.
func TestBuildAuthURL_ConfigPlusExtraACR(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
		ACRValues:   []string{"urn:acr.login.gov:verified-facial-match-required"},
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	authURL, err := c.BuildAuthURL("state-1", "nonce-1", fortyThreeBytePKCE(),
		[]string{"http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true"})
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	acr := u.Query().Get("acr_values")
	wantTokens := []string{
		"urn:acr.login.gov:verified-facial-match-required",
		"http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true",
	}
	for _, tok := range wantTokens {
		if !strings.Contains(acr, tok) {
			t.Fatalf("acr_values %q missing %q", acr, tok)
		}
	}
	// They must be space-separated (OIDC acr_values syntax).
	if !strings.Contains(acr, " ") {
		t.Fatalf("acr_values %q not space-separated", acr)
	}
}

// TestBuildAuthURL_PKCEAndNonce confirms PKCE (S256) and the nonce
// parameter are present — this is the load-bearing security contract
// inherited from oidcrp.BuildAuthURL.
func TestBuildAuthURL_PKCEAndNonce(t *testing.T) {
	srv := newStubLoginGovOP(t)
	key := testRSAKey(t, 2048)
	cfg := Config{
		TenantID:    "tenant-1",
		ClientID:    "client-id",
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/cb",
		SigningKey:  key,
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	authURL, err := c.BuildAuthURL("state-1", "nonce-xyz", fortyThreeBytePKCE(), nil)
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Fatal("code_challenge param missing")
	}
	if q.Get("nonce") != "nonce-xyz" {
		t.Fatalf("nonce = %q, want nonce-xyz", q.Get("nonce"))
	}
	if q.Get("state") != "state-1" {
		t.Fatalf("state = %q, want state-1", q.Get("state"))
	}
	if q.Get("client_id") != cfg.ClientID {
		t.Fatalf("client_id = %q, want %q", q.Get("client_id"), cfg.ClientID)
	}
	if q.Get("redirect_uri") != cfg.RedirectURL {
		t.Fatalf("redirect_uri = %q, want %q", q.Get("redirect_uri"), cfg.RedirectURL)
	}
}

// TestStubDiscovery_LoadsAndPlaceholderReplaced is a smoke test for the
// testdata fixture itself; if Login.gov ever drifts its discovery shape
// we want a single failing test pointing here.
func TestStubDiscovery_LoadsAndPlaceholderReplaced(t *testing.T) {
	raw, err := os.ReadFile("testdata/stub_discovery.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !strings.Contains(string(raw), "__STUB__") {
		t.Fatal("fixture missing __STUB__ placeholder — substitution would silently no-op")
	}
	// Validate as JSON.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	for _, k := range []string{
		"issuer", "authorization_endpoint", "token_endpoint",
		"jwks_uri", "id_token_signing_alg_values_supported",
		"token_endpoint_auth_methods_supported",
	} {
		if _, ok := m[k]; !ok {
			t.Fatalf("fixture missing required key %q", k)
		}
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
