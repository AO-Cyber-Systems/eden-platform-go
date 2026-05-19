package logingov

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/cap/oidc/clientassertion"
)

// testRSAKey generates a fresh RSA key of the requested bit length.
// We generate 2048-bit keys inside tests rather than checking in a
// fixture so the test suite never depends on disk artifacts containing
// "key material" the security team has to scan.
func testRSAKey(tb testing.TB, bits int) *rsa.PrivateKey {
	tb.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		tb.Fatalf("generate RSA-%d key: %v", bits, err)
	}
	return k
}

// decodeJWTSegment base64url-decodes one of the JWT's three '.'-separated
// segments. Per RFC 7515 the encoding is base64url WITHOUT padding.
func decodeJWTSegment(tb testing.TB, seg string) []byte {
	tb.Helper()
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		tb.Fatalf("decode JWT segment %q: %v", seg, err)
	}
	return b
}

// parseJWTHeader returns the decoded JWT header (alg, kid, typ).
func parseJWTHeader(tb testing.TB, jwt string) map[string]any {
	tb.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		tb.Fatalf("JWT shape: want 3 segments, got %d", len(parts))
	}
	var hdr map[string]any
	if err := json.Unmarshal(decodeJWTSegment(tb, parts[0]), &hdr); err != nil {
		tb.Fatalf("unmarshal header: %v", err)
	}
	return hdr
}

// parseJWTClaims returns the decoded JWT claims (iss, sub, aud, exp, iat, jti).
// Audience is decoded as []any since RFC 7519 allows either string or array.
func parseJWTClaims(tb testing.TB, jwt string) map[string]any {
	tb.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		tb.Fatalf("JWT shape: want 3 segments, got %d", len(parts))
	}
	var claims map[string]any
	if err := json.Unmarshal(decodeJWTSegment(tb, parts[1]), &claims); err != nil {
		tb.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

func TestSignClientAssertion_HappyPath(t *testing.T) {
	key := testRSAKey(t, 2048)
	const clientID = "urn:gov:gsa:openidconnect.profiles:sp:sso:test:aoid"
	const audience = "https://idp.int.identitysandbox.gov/api/openid_connect/token"

	assertion, assertionType, err := SignClientAssertion(clientID, audience, key, "")
	if err != nil {
		t.Fatalf("SignClientAssertion: %v", err)
	}
	if assertion == "" {
		t.Fatalf("assertion is empty")
	}
	if assertionType != clientassertion.JWTTypeParam {
		t.Fatalf("assertionType = %q, want %q", assertionType, clientassertion.JWTTypeParam)
	}
	if assertionType != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		t.Fatalf("assertionType drift from RFC 7523 §2.2 constant")
	}

	// Header: alg=RS256, typ=JWT.
	hdr := parseJWTHeader(t, assertion)
	if hdr["alg"] != "RS256" {
		t.Fatalf("header alg = %v, want RS256", hdr["alg"])
	}
	if hdr["typ"] != "JWT" {
		t.Fatalf("header typ = %v, want JWT", hdr["typ"])
	}

	// Claims: iss=sub=clientID, aud contains audience, jti non-empty,
	// exp > iat. Note: NewJWTWithRSAKey sets aud as a JSON array.
	claims := parseJWTClaims(t, assertion)
	if claims["iss"] != clientID {
		t.Fatalf("iss = %v, want %v", claims["iss"], clientID)
	}
	if claims["sub"] != clientID {
		t.Fatalf("sub = %v, want %v", claims["sub"], clientID)
	}
	switch aud := claims["aud"].(type) {
	case string:
		if aud != audience {
			t.Fatalf("aud (string) = %v, want %v", aud, audience)
		}
	case []any:
		found := false
		for _, a := range aud {
			if a == audience {
				found = true
			}
		}
		if !found {
			t.Fatalf("aud (array) %v does not contain %v", aud, audience)
		}
	default:
		t.Fatalf("aud claim has unexpected type %T", claims["aud"])
	}
	jti, _ := claims["jti"].(string)
	if jti == "" {
		t.Fatalf("jti claim empty - RFC 7523 §3 requires unique jti")
	}
	expF, _ := claims["exp"].(float64)
	iatF, _ := claims["iat"].(float64)
	if expF <= iatF {
		t.Fatalf("exp (%v) must be > iat (%v)", expF, iatF)
	}
	lifetime := time.Unix(int64(expF), 0).Sub(time.Unix(int64(iatF), 0))
	// hashicorp/cap clientassertion hardcodes 5-minute lifetime per RFC 7523
	// §3 (max 5 minutes is recommended). We just assert it's within
	// reasonable bounds rather than nailing an exact value.
	if lifetime < 30*time.Second || lifetime > 10*time.Minute {
		t.Fatalf("assertion lifetime %v out of acceptable range [30s, 10m]", lifetime)
	}
}

func TestSignClientAssertion_WithKID(t *testing.T) {
	key := testRSAKey(t, 2048)
	const expectedKID = "logingov-rp-2026q2"
	assertion, _, err := SignClientAssertion("client-id", "https://op.example/token", key, expectedKID)
	if err != nil {
		t.Fatalf("SignClientAssertion with kid: %v", err)
	}
	hdr := parseJWTHeader(t, assertion)
	if hdr["kid"] != expectedKID {
		t.Fatalf("header kid = %v, want %v", hdr["kid"], expectedKID)
	}
}

func TestSignClientAssertion_NilKey(t *testing.T) {
	_, _, err := SignClientAssertion("client-id", "https://op.example/token", nil, "")
	if !errors.Is(err, ErrSigningKeyMissing) {
		t.Fatalf("want ErrSigningKeyMissing, got %v", err)
	}
}

func TestSignClientAssertion_KeyTooShort(t *testing.T) {
	key := testRSAKey(t, 1024)
	_, _, err := SignClientAssertion("client-id", "https://op.example/token", key, "")
	if !errors.Is(err, ErrSigningKeyTooShort) {
		t.Fatalf("want ErrSigningKeyTooShort, got %v", err)
	}
}

// TestSignClientAssertion_SignatureVerifies confirms the produced JWT
// signature validates against the corresponding public key. This is the
// load-bearing security property — if the signature didn't verify,
// Login.gov would reject the assertion and the flow would fail in prod.
func TestSignClientAssertion_SignatureVerifies(t *testing.T) {
	key := testRSAKey(t, 2048)
	assertion, _, err := SignClientAssertion("client-id", "https://op.example/token", key, "kid-1")
	if err != nil {
		t.Fatalf("SignClientAssertion: %v", err)
	}
	if err := verifyJWTRS256(assertion, &key.PublicKey); err != nil {
		t.Fatalf("signature verify failed: %v", err)
	}
}

// TestSignClientAssertion_ConcurrentUniqueJTI confirms the assertion is
// safe to call from many goroutines AND that each invocation produces a
// distinct `jti`. A duplicate jti would let Login.gov reject the second
// assertion under jti-replay defences.
func TestSignClientAssertion_ConcurrentUniqueJTI(t *testing.T) {
	key := testRSAKey(t, 2048)
	const N = 100

	type result struct {
		jti string
		err error
	}
	results := make(chan result, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			a, _, err := SignClientAssertion("client-id", "https://op.example/token", key, "")
			if err != nil {
				results <- result{err: err}
				return
			}
			claims := parseJWTClaims(t, a)
			jti, _ := claims["jti"].(string)
			results <- result{jti: jti}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]bool, N)
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent sign error: %v", r.err)
		}
		if r.jti == "" {
			t.Fatalf("empty jti in concurrent batch")
		}
		if seen[r.jti] {
			t.Fatalf("duplicate jti %q across concurrent calls", r.jti)
		}
		seen[r.jti] = true
	}
	if len(seen) != N {
		t.Fatalf("expected %d unique jti values, got %d", N, len(seen))
	}
}
