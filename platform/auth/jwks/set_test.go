package jwks

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"
)

// fakeSigner is a hand-built Signer implementation for testing AddSigningKey
// without pulling in the real *crypto.ActiveSigner type (which lives in
// platform/kms and would create an import cycle for tests sitting in this
// package).
type fakeSigner struct {
	pub crypto.PublicKey
	alg string
}

func (f *fakeSigner) Public() crypto.PublicKey  { return f.pub }
func (f *fakeSigner) SigningAlgorithm() string { return f.alg }

// TestSet_AddSigningKey_ES256 builds a P-256 signer, adds it to a Set,
// marshals to JSON, and asserts top-level "keys" array length=1 + kty="EC".
func TestSet_AddSigningKey_ES256(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256 key: %v", err)
	}
	signer := &fakeSigner{pub: &priv.PublicKey, alg: "ES256"}

	set := Set{}
	if err := set.AddSigningKey(signer, "kid-es-1"); err != nil {
		t.Fatalf("AddSigningKey: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("len(Keys) = %d, want 1", len(set.Keys))
	}
	if set.Keys[0].Kty != "EC" {
		t.Errorf("kty = %q, want EC", set.Keys[0].Kty)
	}
	if set.Keys[0].Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", set.Keys[0].Alg)
	}
	if set.Keys[0].Use != "sig" {
		t.Errorf("use = %q, want sig", set.Keys[0].Use)
	}
	if set.Keys[0].Kid != "kid-es-1" {
		t.Errorf("kid = %q, want kid-es-1", set.Keys[0].Kid)
	}

	body, err := json.Marshal(&set)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	s := string(body)
	if !strings.HasPrefix(s, `{"keys":[`) {
		t.Errorf("JSON should start with {\"keys\":[ — got %s", s)
	}
	if !strings.Contains(s, `"kty":"EC"`) {
		t.Errorf("JSON missing EC kty: %s", s)
	}
}

// TestSet_AddSigningKey_RS256 builds an RSA signer, adds it, asserts kty="RSA".
func TestSet_AddSigningKey_RS256(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA: %v", err)
	}
	signer := &fakeSigner{pub: &priv.PublicKey, alg: "RS256"}

	set := Set{}
	if err := set.AddSigningKey(signer, "kid-rsa-1"); err != nil {
		t.Fatalf("AddSigningKey: %v", err)
	}
	if len(set.Keys) != 1 || set.Keys[0].Kty != "RSA" {
		t.Errorf("expected single RSA key, got %+v", set.Keys)
	}
}

// TestSet_AddSigningKey_RejectsUnsupportedAlg confirms ML-DSA and any other
// unrecognized algorithm string returns an error mentioning ES256/RS256.
func TestSet_AddSigningKey_RejectsUnsupportedAlg(t *testing.T) {
	t.Parallel()
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	signer := &fakeSigner{pub: &priv.PublicKey, alg: "ML-DSA-65"}
	set := Set{}
	err := set.AddSigningKey(signer, "k")
	if err == nil {
		t.Fatalf("expected error for ML-DSA-65, got nil")
	}
	if !strings.Contains(err.Error(), "ES256") || !strings.Contains(err.Error(), "RS256") {
		t.Errorf("error %q should mention ES256 and RS256", err.Error())
	}
}

// TestSet_AddSigningKey_AlgPublicKeyMismatch verifies an RSA signer claiming
// ES256 (or EC signer claiming RS256) returns a clear error rather than
// silently emitting wrong-shaped JWK.
func TestSet_AddSigningKey_AlgPublicKeyMismatch(t *testing.T) {
	t.Parallel()
	// EC key advertised as RS256.
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	signer := &fakeSigner{pub: &priv.PublicKey, alg: "RS256"}
	set := Set{}
	if err := set.AddSigningKey(signer, "k"); err == nil {
		t.Errorf("EC key claiming RS256 should fail")
	}
	// RSA key advertised as ES256.
	rsapriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer2 := &fakeSigner{pub: &rsapriv.PublicKey, alg: "ES256"}
	if err := set.AddSigningKey(signer2, "k"); err == nil {
		t.Errorf("RSA key claiming ES256 should fail")
	}
}

// TestSet_AddSigningKey_NilSigner rejects a nil signer.
func TestSet_AddSigningKey_NilSigner(t *testing.T) {
	t.Parallel()
	set := Set{}
	if err := set.AddSigningKey(nil, "k"); err == nil {
		t.Errorf("nil signer should fail")
	}
}

// TestSet_AddSigningKey_EmptyKid rejects an empty kid.
func TestSet_AddSigningKey_EmptyKid(t *testing.T) {
	t.Parallel()
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	signer := &fakeSigner{pub: &priv.PublicKey, alg: "ES256"}
	set := Set{}
	if err := set.AddSigningKey(signer, ""); err == nil {
		t.Errorf("empty kid should fail")
	}
}

// TestSet_MarshalJSON_EmptyKeys verifies an empty Set marshals to
// {"keys":[]} — the "keys" field is REQUIRED by RFC 7517 §5 even when empty.
func TestSet_MarshalJSON_EmptyKeys(t *testing.T) {
	t.Parallel()
	set := Set{}
	body, err := json.Marshal(&set)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(body) != `{"keys":[]}` {
		t.Errorf("empty Set: got %q, want {\"keys\":[]}", string(body))
	}
}

// TestSet_MarshalJSON_DeterministicOrder adds three keys and asserts that the
// JSON output preserves insertion order. AOID's convention is ACTIVE key
// first, then retiring keys in retirement order.
func TestSet_MarshalJSON_DeterministicOrder(t *testing.T) {
	t.Parallel()
	priv1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	priv2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	priv3, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	set := Set{}
	if err := set.AddSigningKey(&fakeSigner{pub: &priv1.PublicKey, alg: "ES256"}, "kid-active"); err != nil {
		t.Fatalf("add active: %v", err)
	}
	if err := set.AddSigningKey(&fakeSigner{pub: &priv2.PublicKey, alg: "ES256"}, "kid-retiring-1"); err != nil {
		t.Fatalf("add retiring 1: %v", err)
	}
	if err := set.AddSigningKey(&fakeSigner{pub: &priv3.PublicKey, alg: "ES256"}, "kid-retiring-2"); err != nil {
		t.Fatalf("add retiring 2: %v", err)
	}

	body, err := json.Marshal(&set)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	s := string(body)
	// Order must be active → retiring-1 → retiring-2 in the JSON byte stream.
	i0 := strings.Index(s, `"kid":"kid-active"`)
	i1 := strings.Index(s, `"kid":"kid-retiring-1"`)
	i2 := strings.Index(s, `"kid":"kid-retiring-2"`)
	if i0 < 0 || i1 < 0 || i2 < 0 {
		t.Fatalf("missing one of the kid markers: %s", s)
	}
	if !(i0 < i1 && i1 < i2) {
		t.Errorf("order broken: active@%d retiring-1@%d retiring-2@%d", i0, i1, i2)
	}

	// Marshaling twice must be byte-identical (no map iteration randomness).
	body2, _ := json.Marshal(&set)
	if string(body) != string(body2) {
		t.Errorf("non-deterministic marshal: first=%s second=%s", body, body2)
	}
}
