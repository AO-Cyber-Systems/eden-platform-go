package oidcrp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// fakeOP stands up a complete OIDC provider for ExchangeAndVerify tests:
// discovery doc, JWKS, and a token endpoint that issues an RS256-signed ID
// token. Override knobs let tests inject nonce-mismatch / missing-id-token.
type fakeOP struct {
	t       *testing.T
	srv     *httptest.Server
	rsaKey  *rsa.PrivateKey
	kid     string
	clientID string

	// Per-test injection:
	codeToNonce map[string]string // code -> nonce baked into the ID token
	omitIDToken atomic.Bool       // if true, /token returns only access_token
	codeUsed    map[string]bool
}

func newFakeOP(t *testing.T, clientID string) *fakeOP {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	op := &fakeOP{
		t:           t,
		rsaKey:      rsaKey,
		kid:         "test-kid-1",
		clientID:    clientID,
		codeToNonce: make(map[string]string),
		codeUsed:    make(map[string]bool),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", op.handleDiscovery)
	mux.HandleFunc("/jwks", op.handleJWKS)
	mux.HandleFunc("/token", op.handleToken)
	op.srv = httptest.NewServer(mux)
	t.Cleanup(op.srv.Close)
	return op
}

func (op *fakeOP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"issuer":                                op.srv.URL,
		"authorization_endpoint":                op.srv.URL + "/auth",
		"token_endpoint":                        op.srv.URL + "/token",
		"jwks_uri":                              op.srv.URL + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (op *fakeOP) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := &op.rsaKey.PublicKey
	nBytes := pub.N.Bytes()
	eBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(eBytes, uint64(pub.E))
	// Trim leading zero bytes from E.
	i := 0
	for i < len(eBytes)-1 && eBytes[i] == 0 {
		i++
	}
	eBytes = eBytes[i:]
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": op.kid,
			"n":   base64.RawURLEncoding.EncodeToString(nBytes),
			"e":   base64.RawURLEncoding.EncodeToString(eBytes),
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

func (op *fakeOP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := r.Form.Get("code")
	nonce, ok := op.codeToNonce[code]
	if !ok {
		http.Error(w, "unknown code", http.StatusBadRequest)
		return
	}
	if op.codeUsed[code] {
		http.Error(w, "code already used", http.StatusBadRequest)
		return
	}
	op.codeUsed[code] = true

	resp := map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if !op.omitIDToken.Load() {
		resp["id_token"] = op.signIDToken(nonce)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// signIDToken issues a minimal RS256 ID token. We hand-roll the JWS rather
// than pull a JWT lib into the test to keep oidcrp's tests free of indirect
// deps that test the lib instead of the package under test.
func (op *fakeOP) signIDToken(nonce string) string {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": op.kid,
	}
	now := time.Now()
	payload := map[string]any{
		"iss":   op.srv.URL,
		"sub":   "user-123",
		"aud":   op.clientID,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"nonce": nonce,
		"email": "user@example.com",
		"name":  "Test User",
	}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(payload)
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(hb) + "." + enc.EncodeToString(pb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, op.rsaKey, hashSHA256, sum[:])
	if err != nil {
		op.t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + enc.EncodeToString(sig)
}

// hashSHA256 is the hash identifier used with rsa.SignPKCS1v15. Held as a
// package var so the test file doesn't need to repeat the crypto.Hash
// reference at every call site.
var hashSHA256 = crypto.SHA256

func TestBuildAuthURL_ContainsPKCEAndNonce(t *testing.T) {
	t.Parallel()
	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://op.example.com/auth",
			TokenURL: "https://op.example.com/token",
		},
		RedirectURL: "https://app.example.com/cb",
		Scopes:      []string{"openid", "email"},
	}
	verifier := oauth2.GenerateVerifier()
	authURL, err := BuildAuthURL(cfg, "STATE-XYZ", "NONCE-ABC", verifier, nil)
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if q.Get("code_challenge") == "" {
		t.Error("missing code_challenge")
	}
	if got := q.Get("nonce"); got != "NONCE-ABC" {
		t.Errorf("nonce = %q, want NONCE-ABC", got)
	}
	if got := q.Get("state"); got != "STATE-XYZ" {
		t.Errorf("state = %q, want STATE-XYZ", got)
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
}

func TestBuildAuthURL_ShortVerifierRejected(t *testing.T) {
	t.Parallel()
	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{AuthURL: "https://op/auth"},
	}
	_, err := BuildAuthURL(cfg, "s", "n", "too-short", nil)
	if err == nil {
		t.Fatal("expected error on short PKCE verifier, got nil")
	}
}

func TestBuildAuthURL_EmptyStateNonceRejected(t *testing.T) {
	t.Parallel()
	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{AuthURL: "https://op/auth"},
	}
	v := oauth2.GenerateVerifier()
	if _, err := BuildAuthURL(cfg, "", "n", v, nil); err == nil {
		t.Error("expected error on empty state")
	}
	if _, err := BuildAuthURL(cfg, "s", "", v, nil); err == nil {
		t.Error("expected error on empty nonce")
	}
}

func TestBuildAuthURL_ExtrasAppended(t *testing.T) {
	t.Parallel()
	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{AuthURL: "https://op/auth"},
	}
	v := oauth2.GenerateVerifier()
	extras := []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("prompt", "login")}
	authURL, err := BuildAuthURL(cfg, "s", "n", v, extras)
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	if !strings.Contains(authURL, "prompt=login") {
		t.Errorf("extras not appended: %s", authURL)
	}
}

func TestExchangeAndVerify_HappyPath(t *testing.T) {
	t.Parallel()
	op := newFakeOP(t, "client-A")
	code := "auth-code-1"
	nonce := "nonce-happy"
	op.codeToNonce[code] = nonce

	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{
			AuthURL:  op.srv.URL + "/auth",
			TokenURL: op.srv.URL + "/token",
		},
		RedirectURL: "https://app/cb",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, op.srv.URL)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "client-A"})

	idt, tok, claims, err := ExchangeAndVerify(ctx, cfg, verifier, code, oauth2.GenerateVerifier(), nonce)
	if err != nil {
		t.Fatalf("ExchangeAndVerify: %v", err)
	}
	if tok.AccessToken != "fake-access-token" {
		t.Errorf("access_token = %q", tok.AccessToken)
	}
	if idt.Subject != "user-123" {
		t.Errorf("sub = %q, want user-123", idt.Subject)
	}
	if claims["email"] != "user@example.com" {
		t.Errorf("email = %v, want user@example.com", claims["email"])
	}
	if claims["nonce"] != nonce {
		t.Errorf("nonce claim = %v", claims["nonce"])
	}
}

func TestExchangeAndVerify_NonceMismatch(t *testing.T) {
	t.Parallel()
	op := newFakeOP(t, "client-A")
	code := "auth-code-2"
	op.codeToNonce[code] = "nonce-from-op"

	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{
			AuthURL:  op.srv.URL + "/auth",
			TokenURL: op.srv.URL + "/token",
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, op.srv.URL)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "client-A"})

	_, _, _, err = ExchangeAndVerify(ctx, cfg, verifier, code, oauth2.GenerateVerifier(), "DIFFERENT-stored-nonce")
	if !errors.Is(err, ErrNonceMismatch) {
		t.Fatalf("expected ErrNonceMismatch, got: %v", err)
	}
}

func TestExchangeAndVerify_MissingIDToken(t *testing.T) {
	t.Parallel()
	op := newFakeOP(t, "client-A")
	op.omitIDToken.Store(true)
	code := "auth-code-3"
	op.codeToNonce[code] = "n"

	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{
			AuthURL:  op.srv.URL + "/auth",
			TokenURL: op.srv.URL + "/token",
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, op.srv.URL)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "client-A"})

	_, _, _, err = ExchangeAndVerify(ctx, cfg, verifier, code, oauth2.GenerateVerifier(), "n")
	if !errors.Is(err, ErrMissingIDToken) {
		t.Fatalf("expected ErrMissingIDToken, got: %v", err)
	}
}

func TestExchangeAndVerify_ExchangeFails(t *testing.T) {
	t.Parallel()
	// Token endpoint that always 500s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	cfg := &oauth2.Config{
		ClientID: "client-A",
		Endpoint: oauth2.Endpoint{TokenURL: srv.URL},
	}
	// We don't need a real verifier here — Exchange fails before Verify.
	_, _, _, err := ExchangeAndVerify(context.Background(), cfg, nil, "code", oauth2.GenerateVerifier(), "n")
	if err == nil {
		t.Fatal("expected error on Exchange failure, got nil")
	}
}

