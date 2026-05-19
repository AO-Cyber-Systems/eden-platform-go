package logingov

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/oidcrp"
)

// ---------------------------------------------------------------------------
// Stub Login.gov OP — supports private_key_jwt verification and signed
// ID token issuance.
//
// The stub is configurable per-test via opSpec; tests construct one with
// the claim overrides they want (acr, nonce echo, force status, etc.) and
// the stub server takes care of the rest.
// ---------------------------------------------------------------------------

// opSpec controls how the stub OP behaves on the token endpoint.
type opSpec struct {
	// opSigningKey is the OP's RSA key used to sign ID tokens. JWKS
	// at /api/openid_connect/certs publishes this key's public half.
	opSigningKey *rsa.PrivateKey

	// rpVerifyKey is the RP's public key used to verify the inbound
	// client_assertion JWT. Tests register this with the stub at startup.
	rpVerifyKey *rsa.PublicKey

	// expectedAud is the audience the OP requires in the client_assertion.
	// Defaults to the stub's token endpoint URL.
	expectedAud string

	// idTokenIssuer is the iss claim placed in the ID token. Must match
	// what the RP's verifier expects (which is cfg.IssuerURL → stub URL).
	idTokenIssuer string

	// idTokenAudience is the aud claim placed in the ID token. Must match
	// the RP's ClientID.
	idTokenAudience string

	// returnedSub is the sub claim. Defaults to "stub-sub-uuid" if unset.
	returnedSub string

	// returnedEmail is the email claim. Defaults to "test@example.gov".
	returnedEmail string

	// returnedACR is the acr claim. Defaults to "urn:acr.login.gov:auth-only".
	returnedACR string

	// returnedNonce is the nonce claim. If empty, the OP echoes whatever
	// nonce the test sets via the client_assertion or by other means;
	// tests typically just set this directly to the desired value.
	returnedNonce string

	// forceStatus, if non-zero, replaces the response with that status
	// code and a JSON body containing an "error" field.
	forceStatus int

	// dropIDToken, if true, returns a successful response that omits
	// the id_token field entirely.
	dropIDToken bool

	// rejectAssertion, if true, returns 401 unauthorized without
	// inspecting the assertion (simulates Login.gov rejecting the
	// signature/clientID).
	rejectAssertion bool
}

// newStubExchangeOP returns an httptest.Server pre-configured for
// exchange testing. The discovery doc points at the same server's
// endpoints, the JWKS publishes spec.opSigningKey's public half, and
// the token endpoint verifies inbound assertions against spec.rpVerifyKey.
func newStubExchangeOP(t *testing.T, spec *opSpec) *httptest.Server {
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
		jwks := buildJWKS(t, &spec.opSigningKey.PublicKey, "op-kid-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	})

	mux.HandleFunc("/api/openid_connect/token", func(w http.ResponseWriter, r *http.Request) {
		if spec.forceStatus != 0 {
			w.WriteHeader(spec.forceStatus)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"forced","status":%d}`, spec.forceStatus)))
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			http.Error(w, "want form content-type", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
			return
		}
		ca := r.PostForm.Get("client_assertion")
		cat := r.PostForm.Get("client_assertion_type")
		if cat != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
			http.Error(w, "wrong client_assertion_type", http.StatusBadRequest)
			return
		}
		if ca == "" {
			http.Error(w, "missing client_assertion", http.StatusBadRequest)
			return
		}
		if spec.rejectAssertion {
			http.Error(w, "client_assertion rejected", http.StatusUnauthorized)
			return
		}
		// Verify the assertion's signature against the registered RP public key.
		if err := verifyJWTRS256(ca, spec.rpVerifyKey); err != nil {
			http.Error(w, "client_assertion signature invalid: "+err.Error(), http.StatusUnauthorized)
			return
		}
		// Parse assertion claims and check aud.
		parts := strings.Split(ca, ".")
		if len(parts) != 3 {
			http.Error(w, "client_assertion shape invalid", http.StatusBadRequest)
			return
		}
		claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			http.Error(w, "client_assertion claims decode: "+err.Error(), http.StatusBadRequest)
			return
		}
		var claims map[string]any
		if err := json.Unmarshal(claimsRaw, &claims); err != nil {
			http.Error(w, "client_assertion claims unmarshal: "+err.Error(), http.StatusBadRequest)
			return
		}
		audOK := false
		expectedAud := spec.expectedAud
		if expectedAud == "" {
			expectedAud = srv.URL + "/api/openid_connect/token"
		}
		switch aud := claims["aud"].(type) {
		case string:
			audOK = aud == expectedAud
		case []any:
			for _, a := range aud {
				if a == expectedAud {
					audOK = true
				}
			}
		}
		if !audOK {
			http.Error(w, fmt.Sprintf("client_assertion aud mismatch: got %v want %v", claims["aud"], expectedAud), http.StatusBadRequest)
			return
		}

		// Build the ID token.
		sub := spec.returnedSub
		if sub == "" {
			sub = "stub-sub-uuid"
		}
		email := spec.returnedEmail
		if email == "" {
			email = "test@example.gov"
		}
		acr := spec.returnedACR
		if acr == "" {
			acr = "urn:acr.login.gov:auth-only"
		}

		idClaims := map[string]any{
			"iss":            spec.idTokenIssuer,
			"sub":            sub,
			"aud":            spec.idTokenAudience,
			"exp":            time.Now().Add(5 * time.Minute).Unix(),
			"iat":            time.Now().Unix(),
			"nonce":          spec.returnedNonce,
			"email":          email,
			"email_verified": true,
			"acr":            acr,
		}
		idToken := signRS256JWT(t, spec.opSigningKey, "op-kid-1", idClaims)

		response := map[string]any{
			"token_type":   "Bearer",
			"expires_in":   300,
			"access_token": "stub-access-token",
		}
		if !spec.dropIDToken {
			response["id_token"] = idToken
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// signRS256JWT produces a minimal RS256-signed JWT with the given header
// kid and claims body. Implemented with the stdlib so we don't depend on
// the same library that produced the RP assertion.
func signRS256JWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	hdrJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(hdrJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("sign ID token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// buildJWKS produces a minimal JWKS containing the given RSA public key
// with the supplied kid. This is what /api/openid_connect/certs returns;
// go-oidc's verifier fetches it during ID token verification.
func buildJWKS(t *testing.T, pub *rsa.PublicKey, kid string) []byte {
	t.Helper()
	// JWK fields per RFC 7517 + RFC 7518.
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	// e is the exponent; for typical RSA keys it's 65537 → AQAB.
	// Encode pub.E as a big-endian byte slice without leading zeros.
	eBytes := bigEndianBytes(pub.E)
	e := base64.RawURLEncoding.EncodeToString(eBytes)

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": kid,
				"n":   n,
				"e":   e,
			},
		},
	}
	b, _ := json.Marshal(jwks)
	return b
}

func bigEndianBytes(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	out := []byte{}
	for e > 0 {
		out = append([]byte{byte(e & 0xff)}, out...)
		e >>= 8
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func mkExchangeClient(t *testing.T, srv *httptest.Server, rpKey *rsa.PrivateKey, clientID string) *Client {
	t.Helper()
	cfg := Config{
		TenantID:    "tenant-x",
		ClientID:    clientID,
		IssuerURL:   srv.URL,
		RedirectURL: "https://aoid.example/federate/logingov/callback",
		SigningKey:  rpKey,
		SigningKID:  "rp-kid-1",
	}
	c, err := NewClient(context.Background(), cfg, oidcrp.NewProviderCache(), oidcrp.NewVerifierCache())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestClientExchange_HappyPath_IAL1 confirms the end-to-end exchange flow
// against the stub OP: the RP signs an assertion, the OP verifies it +
// returns an ID token, the RP verifies the ID token + nonce, mapACR is
// applied. Default acr → ial_1.
func TestClientExchange_HappyPath_IAL1(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"
	const expectedNonce = "nonce-happy-path-1"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		returnedACR:     "urn:acr.login.gov:auth-only",
		returnedNonce:   expectedNonce,
		returnedSub:     "user-sub-uuid-001",
		returnedEmail:   "civilian@example.gov",
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL // discovery + id_token iss MUST match

	c := mkExchangeClient(t, srv, rpKey, clientID)

	id, err := c.Exchange(context.Background(), "test-auth-code", fortyThreeBytePKCE(), expectedNonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Sub != "user-sub-uuid-001" {
		t.Fatalf("Sub = %q, want user-sub-uuid-001", id.Sub)
	}
	if id.Email != "civilian@example.gov" {
		t.Fatalf("Email = %q", id.Email)
	}
	if !id.EmailVerified {
		t.Fatal("EmailVerified false")
	}
	if id.ACR != "urn:acr.login.gov:auth-only" {
		t.Fatalf("raw ACR = %q", id.ACR)
	}
	if id.AssuranceLevel != "ial_1" {
		t.Fatalf("AssuranceLevel = %q, want ial_1", id.AssuranceLevel)
	}
	if id.RawClaims == nil {
		t.Fatal("RawClaims not populated")
	}
}

// TestClientExchange_AAL3_PhishingResistant confirms the AAL escalation
// ACR is recognized by mapACR.
func TestClientExchange_AAL3_PhishingResistant(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"
	const nonce = "nonce-aal3"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		returnedACR:     "http://idmanagement.gov/ns/assurance/aal/2?phishing_resistant=true",
		returnedNonce:   nonce,
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL

	c := mkExchangeClient(t, srv, rpKey, clientID)
	id, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), nonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.AssuranceLevel != "aal_3" {
		t.Fatalf("AssuranceLevel = %q, want aal_3", id.AssuranceLevel)
	}
}

// TestClientExchange_NonceMismatch is the load-bearing security test:
// when the returned ID token's nonce claim differs from the storedNonce
// supplied by the caller, Exchange MUST return ErrNonceMismatch. A miss
// here would let attackers replay codes.
func TestClientExchange_NonceMismatch(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		returnedNonce:   "OP-returned-nonce-WRONG",
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "RP-stored-nonce-RIGHT")
	if !errors.Is(err, ErrNonceMismatch) {
		t.Fatalf("want ErrNonceMismatch, got %v", err)
	}
}

// TestClientExchange_TokenEndpoint500 confirms 5xx responses are wrapped
// with ErrTokenEndpointStatus so callers can detect retriable failures.
func TestClientExchange_TokenEndpoint500(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey: opKey,
		rpVerifyKey:  &rpKey.PublicKey,
		forceStatus:  http.StatusInternalServerError,
	}
	srv := newStubExchangeOP(t, spec)

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "any-nonce")
	if !errors.Is(err, ErrTokenEndpointStatus) {
		t.Fatalf("want ErrTokenEndpointStatus, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error %v does not preserve status code", err)
	}
}

// TestClientExchange_TokenEndpoint401_AssertionRejected confirms that
// when the OP rejects the client_assertion (e.g., wrong kid, key not
// registered), the resulting 401 maps to ErrTokenEndpointStatus.
func TestClientExchange_TokenEndpoint401_AssertionRejected(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		rejectAssertion: true,
	}
	srv := newStubExchangeOP(t, spec)

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "n")
	if !errors.Is(err, ErrTokenEndpointStatus) {
		t.Fatalf("want ErrTokenEndpointStatus, got %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error %v does not preserve 401 status", err)
	}
}

// TestClientExchange_MissingIDToken confirms a 200 response without an
// id_token field is rejected — indicates an OP misconfigured for OAuth2-
// only (no OIDC) or a scope mismatch.
func TestClientExchange_MissingIDToken(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		dropIDToken:     true,
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "n")
	if err == nil {
		t.Fatal("want error on missing id_token, got nil")
	}
	if !strings.Contains(err.Error(), "id_token") {
		t.Fatalf("error %v does not mention id_token", err)
	}
}

// TestClientExchange_RPAssertionVerified confirms the stub OP's
// signature check is actually exercised — protects against a future
// refactor that silently breaks SignClientAssertion's signature output.
func TestClientExchange_RPAssertionVerified(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	wrongKey := testRSAKey(t, 2048) // OP is told this key is the RP's, but RP signs with rpKey
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &wrongKey.PublicKey, // wrong key registered at OP
		idTokenAudience: clientID,
		returnedNonce:   "n",
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "n")
	if err == nil {
		t.Fatal("want OP-side assertion-verify failure, got nil")
	}
	// OP returns 401 → wrapped in ErrTokenEndpointStatus.
	if !errors.Is(err, ErrTokenEndpointStatus) {
		t.Fatalf("want ErrTokenEndpointStatus on RP assertion sig fail, got %v", err)
	}
}

// TestClientExchange_AssertionAudIsTokenEndpoint is a regression test for
// gotcha §G: the client_assertion aud claim MUST equal the token
// endpoint URL exactly, NOT the issuer URL. Login.gov enforces this.
// We assert it by configuring the stub to require a SPECIFIC aud and
// confirming Exchange supplies the token endpoint URL.
func TestClientExchange_AssertionAudIsTokenEndpoint(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		returnedNonce:   "n",
		// expectedAud is overridden below to be the token endpoint URL.
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL
	spec.expectedAud = srv.URL + "/api/openid_connect/token"

	c := mkExchangeClient(t, srv, rpKey, clientID)
	id, err := c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "n")
	if err != nil {
		t.Fatalf("Exchange (aud must be token endpoint): %v", err)
	}
	if id == nil {
		t.Fatal("ID nil")
	}
}

// TestClientExchange_Concurrent confirms Exchange is safe to call from
// many goroutines against one Client instance. -race must be clean.
func TestClientExchange_Concurrent(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"
	const nonce = "nonce-concurrent"

	spec := &opSpec{
		opSigningKey:    opKey,
		rpVerifyKey:     &rpKey.PublicKey,
		idTokenAudience: clientID,
		returnedNonce:   nonce,
	}
	srv := newStubExchangeOP(t, spec)
	spec.idTokenIssuer = srv.URL

	c := mkExchangeClient(t, srv, rpKey, clientID)

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := c.Exchange(context.Background(), fmt.Sprintf("code-%d", i), fortyThreeBytePKCE(), nonce)
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}

// TestClientExchange_RequestContentType ensures the RP sends the correct
// form-encoded Content-Type. The stub already rejects non-form requests,
// so a passing happy-path test indirectly validates this — but we add
// an explicit assertion here so a future refactor that silently switches
// to JSON gets caught early.
func TestClientExchange_RequestContentType(t *testing.T) {
	rpKey := testRSAKey(t, 2048)
	opKey := testRSAKey(t, 2048)
	const clientID = "test-rp-client-id"

	var observed string
	mux := http.NewServeMux()
	var srv *httptest.Server
	tmpl, _ := os.ReadFile("testdata/stub_discovery.json")
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(string(tmpl), "__STUB__", srv.URL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/api/openid_connect/certs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buildJWKS(t, &opKey.PublicKey, "op-kid-1"))
	})
	mux.HandleFunc("/api/openid_connect/token", func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Get("Content-Type")
		// Return 400 so we don't care about the rest of the flow.
		http.Error(w, "ok-stop-here", http.StatusBadRequest)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := mkExchangeClient(t, srv, rpKey, clientID)
	_, _ = c.Exchange(context.Background(), "code", fortyThreeBytePKCE(), "n")
	if !strings.HasPrefix(observed, "application/x-www-form-urlencoded") {
		t.Fatalf("Content-Type = %q, want application/x-www-form-urlencoded", observed)
	}
}
