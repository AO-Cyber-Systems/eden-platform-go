package issuer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/clients"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/composition"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/fixtures"
)

// testHarness boots an in-memory aoid composition + issuer and returns
// a ready-to-use httptest.Server.
type testHarness struct {
	srv     *httptest.Server
	svcs    *composition.Services
	iss     *Issuer
	fix     *fixtures.Fixture
	cfg     *config.Config
	secret  string
	client  string
	rURI    string
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	cfg := &config.Config{
		Issuer:             "http://localhost:8090",
		Environment:        "dev",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		ShutdownTimeout:    time.Second,
		AODexClientSecret:  "test-secret",
		AODexRedirectURIs:  []string{"http://aodex.test/cb"},
	}
	svcs, err := composition.BuildInMemory(cfg)
	if err != nil {
		t.Fatalf("BuildInMemory: %v", err)
	}
	t.Cleanup(func() { _ = svcs.Close() })

	fix, err := fixtures.Seed(context.Background(), svcs)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}

	iss := New(
		Config{
			Issuer:      cfg.Issuer,
			AuthCodeTTL: 10 * time.Minute,
			SessionTTL:  24 * time.Hour,
		},
		svcs.Auth,
		svcs.JWTManager,
		svcs.Clients,
		svcs.Auth,
	)
	iss.SecureCookies = false

	mux := http.NewServeMux()
	iss.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &testHarness{
		srv:    srv,
		svcs:   svcs,
		iss:    iss,
		fix:    fix,
		cfg:    cfg,
		secret: "test-secret",
		client: clients.AODexClientID,
		rURI:   "http://aodex.test/cb",
	}
}

// pkce returns (verifier, challenge) for an S256 PKCE pair.
func pkce(t *testing.T) (string, string) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// authorizeURL constructs an /oauth2/authorize URL with the harness's
// AODex client + the given pkce/state/nonce/scope.
func (h *testHarness) authorizeURL(challenge, state, nonce, scope string) string {
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.client)
	q.Set("redirect_uri", h.rURI)
	q.Set("scope", scope)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String()
}

func TestAuthorize_RendersLoginForm(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(h.authorizeURL(ch, "s1", "n1", "openid email"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type=%q want text/html", ct)
	}
	body, _ := io.ReadAll(res.Body)
	bs := string(body)
	if !strings.Contains(bs, `name="client_id" value="`+h.client+`"`) {
		t.Error("login form missing client_id hidden input")
	}
	if !strings.Contains(bs, "Sign in to AO ID") {
		t.Error("login form missing AO ID heading")
	}
	if !strings.Contains(bs, "AODex (pilot)") {
		t.Error("login form missing client display name")
	}
}

func TestAuthorize_UnknownClient_4xx(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", "missing-client")
	q.Set("redirect_uri", h.rURI)
	q.Set("scope", "openid")
	q.Set("code_challenge", ch)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(u.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want 400", res.StatusCode)
	}
}

func TestAuthorize_RedirectMismatch_4xx(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.client)
	q.Set("redirect_uri", "http://attacker.test/cb")
	q.Set("scope", "openid")
	q.Set("code_challenge", ch)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(u.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	// Must NOT redirect — exact 400 response.
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want 400 (no redirect for redirect mismatch)", res.StatusCode)
	}
}

func TestAuthorize_MissingPKCE_RedirectsErr(t *testing.T) {
	h := newHarness(t)
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.client)
	q.Set("redirect_uri", h.rURI)
	q.Set("scope", "openid")
	q.Set("state", "xyz")
	u.RawQuery = q.Encode()

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(u.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Errorf("status=%d want 302", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Query().Get("error") != "invalid_request" {
		t.Errorf("err=%q want invalid_request", loc.Query().Get("error"))
	}
	if loc.Query().Get("state") != "xyz" {
		t.Error("state lost in error redirect")
	}
}

func TestAuthorize_BadResponseType_RedirectsErr(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "token") // unsupported
	q.Set("client_id", h.client)
	q.Set("redirect_uri", h.rURI)
	q.Set("scope", "openid")
	q.Set("code_challenge", ch)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(u.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Errorf("status=%d want 302", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Query().Get("error") != "unsupported_response_type" {
		t.Errorf("err=%q", loc.Query().Get("error"))
	}
}

func TestAuthorize_MissingOpenidScope_RedirectsErr(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	u, _ := url.Parse(h.srv.URL + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.client)
	q.Set("redirect_uri", h.rURI)
	q.Set("scope", "email")
	q.Set("code_challenge", ch)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.Get(u.String())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Query().Get("error") != "invalid_scope" {
		t.Errorf("err=%q want invalid_scope", loc.Query().Get("error"))
	}
}

func TestAuthorize_PostLogin_HappyPath(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	form := url.Values{}
	form.Set("aoid_login", "1")
	form.Set("response_type", "code")
	form.Set("client_id", h.client)
	form.Set("redirect_uri", h.rURI)
	form.Set("scope", "openid email")
	form.Set("state", "xyz")
	form.Set("nonce", "n1")
	form.Set("code_challenge", ch)
	form.Set("code_challenge_method", "S256")
	form.Set("email", h.fix.ParentEmail)
	form.Set("password", h.fix.ParentPassword)

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.PostForm(h.srv.URL+"/oauth2/authorize", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Query().Get("code") == "" {
		t.Error("redirect missing code param")
	}
	if loc.Query().Get("state") != "xyz" {
		t.Errorf("state=%q lost", loc.Query().Get("state"))
	}
	// Session cookie should be set.
	hasCookie := false
	for _, c := range res.Cookies() {
		if c.Name == SessionCookieName {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Error("aoid_session cookie not set after login")
	}
}

func TestAuthorize_PostLogin_BadCreds(t *testing.T) {
	h := newHarness(t)
	_, ch := pkce(t)
	form := url.Values{}
	form.Set("aoid_login", "1")
	form.Set("response_type", "code")
	form.Set("client_id", h.client)
	form.Set("redirect_uri", h.rURI)
	form.Set("scope", "openid")
	form.Set("code_challenge", ch)
	form.Set("code_challenge_method", "S256")
	form.Set("email", h.fix.ParentEmail)
	form.Set("password", "wrong-password")

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.PostForm(h.srv.URL+"/oauth2/authorize", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status=%d want 200 (re-render form)", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Invalid email or password") {
		t.Error("expected inline error message")
	}
}

func TestCodeStore_SingleUse(t *testing.T) {
	s := NewMemoryCodeStore()
	defer s.Stop()
	c := AuthCode{
		Code:        "abc",
		ClientID:    "x",
		UserID:      "u",
		ExpiresAt:   time.Now().Add(time.Minute),
		CreatedAt:   time.Now(),
		Scope:       []string{"openid"},
		RedirectURI: "http://x/cb",
	}
	if err := s.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Consume(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "u" {
		t.Errorf("UserID=%q", got.UserID)
	}
	// reuse must fail
	if _, err := s.Consume(context.Background(), "abc"); err == nil {
		t.Error("reuse should fail")
	}
}

func TestCodeStore_Expired(t *testing.T) {
	s := NewMemoryCodeStore()
	defer s.Stop()
	c := AuthCode{Code: "exp", ExpiresAt: time.Now().Add(-time.Minute)}
	if err := s.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(context.Background(), "exp"); err == nil {
		t.Error("expired code should fail")
	}
}

// TestE2E exercises the full authorization-code + PKCE flow against a
// running issuer server: GET /authorize → POST creds → /token →
// /userinfo → refresh → repeat.
func TestE2E_FullOIDCFlow(t *testing.T) {
	h := newHarness(t)
	verifier, challenge := pkce(t)

	// Step 1: POST login submission directly (skip the GET render — that
	// is exercised by TestAuthorize_RendersLoginForm).
	form := url.Values{}
	form.Set("aoid_login", "1")
	form.Set("response_type", "code")
	form.Set("client_id", h.client)
	form.Set("redirect_uri", h.rURI)
	form.Set("scope", "openid email profile offline_access")
	form.Set("state", "STATE-1")
	form.Set("nonce", "NONCE-1")
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "S256")
	form.Set("email", h.fix.ParentEmail)
	form.Set("password", h.fix.ParentPassword)

	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.PostForm(h.srv.URL+"/oauth2/authorize", form)
	if err != nil {
		t.Fatalf("authorize POST: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("authorize status=%d", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in Location")
	}
	if loc.Query().Get("state") != "STATE-1" {
		t.Errorf("state mismatch")
	}

	// Step 2: exchange code for tokens.
	tokForm := url.Values{}
	tokForm.Set("grant_type", "authorization_code")
	tokForm.Set("code", code)
	tokForm.Set("redirect_uri", h.rURI)
	tokForm.Set("code_verifier", verifier)
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/oauth2/token", strings.NewReader(tokForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(h.client, h.secret)
	res, err = cli.Do(req)
	if err != nil {
		t.Fatalf("token POST: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("token status=%d body=%s", res.StatusCode, body)
	}
	var bundle struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if bundle.AccessToken == "" || bundle.IDToken == "" || bundle.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", bundle)
	}
	if bundle.TokenType != "Bearer" {
		t.Errorf("token_type=%q", bundle.TokenType)
	}

	// Step 3: validate access token + ID token via JWTManager.
	claims, err := h.svcs.JWTManager.ValidateAccessToken(bundle.AccessToken)
	if err != nil {
		t.Fatalf("validate access token: %v", err)
	}
	if claims.UserID == "" {
		t.Error("access token missing UserID")
	}

	// Step 4: hit /userinfo with the access token.
	uReq, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/oauth2/userinfo", nil)
	uReq.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
	res, err = cli.Do(uReq)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("userinfo status=%d body=%s", res.StatusCode, body)
	}
	var userClaims map[string]any
	_ = json.Unmarshal(body, &userClaims)
	if userClaims["email"] != h.fix.ParentEmail {
		t.Errorf("userinfo email=%v want %s", userClaims["email"], h.fix.ParentEmail)
	}
	if userClaims["sub"] != h.fix.ParentUserID.String() {
		t.Errorf("userinfo sub mismatch")
	}

	// Step 5: refresh.
	refreshForm := url.Values{}
	refreshForm.Set("grant_type", "refresh_token")
	refreshForm.Set("refresh_token", bundle.RefreshToken)
	refReq, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/oauth2/token", strings.NewReader(refreshForm.Encode()))
	refReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refReq.SetBasicAuth(h.client, h.secret)
	res, err = cli.Do(refReq)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", res.StatusCode, body)
	}
	var bundle2 struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(body, &bundle2)
	if bundle2.AccessToken == "" || bundle2.RefreshToken == "" {
		t.Errorf("refresh missing tokens: %+v", bundle2)
	}
	if bundle2.RefreshToken == bundle.RefreshToken {
		t.Error("refresh token not rotated")
	}

	// Step 6: reuse old refresh — must fail (rotation).
	res, err = cli.Do(refReq)
	if err != nil {
		t.Fatalf("refresh reuse: %v", err)
	}
	if res.StatusCode == http.StatusOK {
		t.Error("reused refresh token accepted — rotation failed")
	}
	res.Body.Close()
}

func TestToken_BadClientCreds(t *testing.T) {
	h := newHarness(t)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "x")
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// no Basic auth, no form creds
	cli := &http.Client{}
	res, err := cli.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", res.StatusCode)
	}
}

func TestToken_WrongSecret(t *testing.T) {
	h := newHarness(t)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "x")
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(h.client, "wrong")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", res.StatusCode)
	}
}

func TestToken_BadCodeVerifier(t *testing.T) {
	h := newHarness(t)
	_, challenge := pkce(t)
	// Mint a code through the authorize POST flow.
	form := url.Values{}
	form.Set("aoid_login", "1")
	form.Set("response_type", "code")
	form.Set("client_id", h.client)
	form.Set("redirect_uri", h.rURI)
	form.Set("scope", "openid")
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "S256")
	form.Set("email", h.fix.ParentEmail)
	form.Set("password", h.fix.ParentPassword)
	cli := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := cli.PostForm(h.srv.URL+"/oauth2/authorize", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	code := loc.Query().Get("code")

	// Now exchange with a bogus verifier.
	tf := url.Values{}
	tf.Set("grant_type", "authorization_code")
	tf.Set("code", code)
	tf.Set("redirect_uri", h.rURI)
	tf.Set("code_verifier", "bogus")
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/oauth2/token", strings.NewReader(tf.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(h.client, h.secret)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want 400 invalid_grant", r2.StatusCode)
	}
}

func TestUserinfo_MissingBearer(t *testing.T) {
	h := newHarness(t)
	res, err := http.Get(h.srv.URL + "/oauth2/userinfo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d", res.StatusCode)
	}
	if !strings.Contains(res.Header.Get("WWW-Authenticate"), "invalid_token") {
		t.Error("missing WWW-Authenticate header")
	}
}

func TestUserinfo_BadToken(t *testing.T) {
	h := newHarness(t)
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status=%d", res.StatusCode)
	}
}
