package jwks

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"strings"
	"testing"
)

// TestMarshalECPublic_HappyPath generates a P-256 key, marshals to JWK,
// decodes X/Y back to *big.Int, and asserts equality with the original.
// Also asserts that X and Y are exactly 32 bytes (43 chars base64url-no-pad).
func TestMarshalECPublic_HappyPath(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256 key: %v", err)
	}
	jwk, err := MarshalECPublic(&priv.PublicKey, "kid-es256-test", "ES256", "sig")
	if err != nil {
		t.Fatalf("MarshalECPublic: %v", err)
	}
	if jwk.Kty != "EC" {
		t.Errorf("kty = %q, want EC", jwk.Kty)
	}
	if jwk.Crv != "P-256" {
		t.Errorf("crv = %q, want P-256", jwk.Crv)
	}
	if jwk.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", jwk.Alg)
	}
	if jwk.Use != "sig" {
		t.Errorf("use = %q, want sig", jwk.Use)
	}
	if jwk.Kid != "kid-es256-test" {
		t.Errorf("kid = %q, want kid-es256-test", jwk.Kid)
	}
	// base64url-no-pad of 32 bytes = ceil(32*4/3) = 43 chars.
	if len(jwk.X) != 43 {
		t.Errorf("len(X) = %d, want 43 (32 bytes base64url-no-pad)", len(jwk.X))
	}
	if len(jwk.Y) != 43 {
		t.Errorf("len(Y) = %d, want 43 (32 bytes base64url-no-pad)", len(jwk.Y))
	}
	// Round-trip: decode X/Y bytes and compare to original *big.Int.
	xb, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		t.Fatalf("decode X: %v", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		t.Fatalf("decode Y: %v", err)
	}
	if len(xb) != 32 {
		t.Errorf("decoded X len = %d, want 32", len(xb))
	}
	if len(yb) != 32 {
		t.Errorf("decoded Y len = %d, want 32", len(yb))
	}
	gotX := new(big.Int).SetBytes(xb)
	gotY := new(big.Int).SetBytes(yb)
	if gotX.Cmp(priv.PublicKey.X) != 0 {
		t.Errorf("X round-trip mismatch: got %x, want %x", gotX, priv.PublicKey.X)
	}
	if gotY.Cmp(priv.PublicKey.Y) != 0 {
		t.Errorf("Y round-trip mismatch: got %x, want %x", gotY, priv.PublicKey.Y)
	}
}

// TestMarshalECPublic_DeterministicOutput asserts that marshaling the same
// public key twice produces byte-identical JWKs (no randomness in encoding).
func TestMarshalECPublic_DeterministicOutput(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256 key: %v", err)
	}
	j1, err := MarshalECPublic(&priv.PublicKey, "k", "ES256", "sig")
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	j2, err := MarshalECPublic(&priv.PublicKey, "k", "ES256", "sig")
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if j1 != j2 {
		t.Errorf("deterministic marshal failed: %+v vs %+v", j1, j2)
	}
}

// TestMarshalECPublic_RejectsWrongCurve verifies that P-384 and P-521 keys
// are rejected with an error mentioning the curve name. P-256 is the only
// supported curve for now.
func TestMarshalECPublic_RejectsWrongCurve(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		curve     elliptic.Curve
		wantInMsg string
	}{
		{"P-384", elliptic.P384(), "P-384"},
		{"P-521", elliptic.P521(), "P-521"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			priv, err := ecdsa.GenerateKey(tc.curve, rand.Reader)
			if err != nil {
				t.Fatalf("generate %s key: %v", tc.name, err)
			}
			_, err = MarshalECPublic(&priv.PublicKey, "k", "ES384", "sig")
			if err == nil {
				t.Fatalf("MarshalECPublic with %s: want error, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("error %q does not mention curve %s", err.Error(), tc.wantInMsg)
			}
			if !strings.Contains(err.Error(), "P-256") {
				t.Errorf("error %q should mention P-256 (only supported curve)", err.Error())
			}
		})
	}
}

// TestMarshalECPublic_NilKey expects a clear "nil public key" error.
func TestMarshalECPublic_NilKey(t *testing.T) {
	t.Parallel()
	_, err := MarshalECPublic(nil, "k", "ES256", "sig")
	if err == nil {
		t.Fatalf("MarshalECPublic(nil): want error, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error %q should mention nil", err.Error())
	}
}

// TestMarshalRSAPublic_HappyPath generates a 2048-bit RSA key, marshals,
// decodes N+E back, and asserts equality. Verifies E is "AQAB" (the
// base64url-no-pad encoding of 65537 = 0x010001).
func TestMarshalRSAPublic_HappyPath(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate 2048-bit RSA: %v", err)
	}
	jwk, err := MarshalRSAPublic(&priv.PublicKey, "kid-rs256-test", "RS256", "sig")
	if err != nil {
		t.Fatalf("MarshalRSAPublic: %v", err)
	}
	if jwk.Kty != "RSA" {
		t.Errorf("kty = %q, want RSA", jwk.Kty)
	}
	if jwk.Alg != "RS256" {
		t.Errorf("alg = %q, want RS256", jwk.Alg)
	}
	if jwk.Use != "sig" {
		t.Errorf("use = %q, want sig", jwk.Use)
	}
	if jwk.Kid != "kid-rs256-test" {
		t.Errorf("kid = %q, want kid-rs256-test", jwk.Kid)
	}
	// E for 65537 = 0x01 0x00 0x01 → base64url-no-pad("AQAB")
	if jwk.E != "AQAB" {
		t.Errorf("E = %q, want AQAB (65537)", jwk.E)
	}
	// N round-trip.
	nb, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		t.Fatalf("decode N: %v", err)
	}
	gotN := new(big.Int).SetBytes(nb)
	if gotN.Cmp(priv.PublicKey.N) != 0 {
		t.Errorf("N round-trip mismatch")
	}
	// EC fields must be empty on RSA JWK.
	if jwk.X != "" || jwk.Y != "" || jwk.Crv != "" {
		t.Errorf("RSA JWK leaked EC fields: crv=%q x=%q y=%q", jwk.Crv, jwk.X, jwk.Y)
	}
}

// TestMarshalRSAPublic_DeterministicOutput asserts that marshaling the same
// RSA public key twice produces byte-identical JWKs.
func TestMarshalRSAPublic_DeterministicOutput(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA: %v", err)
	}
	j1, err := MarshalRSAPublic(&priv.PublicKey, "k", "RS256", "sig")
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	j2, err := MarshalRSAPublic(&priv.PublicKey, "k", "RS256", "sig")
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if j1 != j2 {
		t.Errorf("deterministic marshal failed: %+v vs %+v", j1, j2)
	}
}

// TestMarshalRSAPublic_NilKey expects a clear "nil public key" error.
func TestMarshalRSAPublic_NilKey(t *testing.T) {
	t.Parallel()
	_, err := MarshalRSAPublic(nil, "k", "RS256", "sig")
	if err == nil {
		t.Fatalf("MarshalRSAPublic(nil): want error, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error %q should mention nil", err.Error())
	}
}

// TestMarshalRSAPublic_WarnsSmallKey captures slog output and verifies that
// marshaling a 1024-bit RSA key emits a "smaller than 2048 bits" warning.
// 1024-bit is operationally unsafe but accepted (RFC 7518 has no minimum).
func TestMarshalRSAPublic_WarnsSmallKey(t *testing.T) {
	// NOT parallel — manipulates the package-level default slog logger.
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate 1024-bit RSA: %v", err)
	}
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	slog.SetDefault(slog.New(h))

	_, err = MarshalRSAPublic(&priv.PublicKey, "k", "RS256", "sig")
	if err != nil {
		t.Fatalf("MarshalRSAPublic(1024-bit): unexpected error %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "2048") {
		t.Errorf("expected warn to mention 2048-bit minimum, got %q", out)
	}
}

// TestMarshalRSAPublic_3072Bit verifies 3072-bit keys marshal cleanly (no
// warning, correct round-trip).
func TestMarshalRSAPublic_3072Bit(t *testing.T) {
	t.Parallel()
	priv, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatalf("generate 3072-bit RSA: %v", err)
	}
	jwk, err := MarshalRSAPublic(&priv.PublicKey, "k", "RS256", "sig")
	if err != nil {
		t.Fatalf("MarshalRSAPublic: %v", err)
	}
	nb, _ := base64.RawURLEncoding.DecodeString(jwk.N)
	if new(big.Int).SetBytes(nb).Cmp(priv.PublicKey.N) != 0 {
		t.Errorf("3072-bit N round-trip failed")
	}
}

// TestJWK_JSONShape marshals a representative EC JWK and asserts all
// required EC JSON fields appear in the output. RSA-specific fields must be
// absent (omitempty).
func TestJWK_JSONShape(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256 key: %v", err)
	}
	jwk, err := MarshalECPublic(&priv.PublicKey, "kid-shape-test", "ES256", "sig")
	if err != nil {
		t.Fatalf("MarshalECPublic: %v", err)
	}
	body, err := json.Marshal(jwk)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		`"kty":"EC"`,
		`"crv":"P-256"`,
		`"alg":"ES256"`,
		`"use":"sig"`,
		`"kid":"kid-shape-test"`,
		`"x":"`,
		`"y":"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("EC JWK JSON missing %q: %s", want, s)
		}
	}
	for _, banned := range []string{`"n":`, `"e":`, `"d":`, `"p":`, `"q":`} {
		if strings.Contains(s, banned) {
			t.Errorf("EC JWK JSON leaked %q: %s", banned, s)
		}
	}
}
