package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"
)

// TestRFC7638Thumbprint_RSAExampleVector replays the canonical example from
// RFC 7638 §3.4 exactly. The expected thumbprint
// "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs" is the most important proof
// of correctness in this package — if this fails, our canonical JSON
// construction (field order, escaping) is wrong.
func TestRFC7638Thumbprint_RSAExampleVector(t *testing.T) {
	t.Parallel()
	// Exact n from RFC 7638 §3.1 example (also §3.4) — pasted verbatim.
	const exampleN = "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx" +
		"4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4" +
		"n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4" +
		"QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zg" +
		"dAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQF" +
		"h6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCu" +
		"r-kEgU8awapJzKnqDKgw"
	const exampleE = "AQAB"
	const want = "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"

	jwk := JWK{
		Kty: "RSA",
		Alg: "RS256",
		Use: "sig",
		Kid: "2011-04-29",
		N:   exampleN,
		E:   exampleE,
	}
	got, err := RFC7638Thumbprint(jwk)
	if err != nil {
		t.Fatalf("RFC7638Thumbprint: %v", err)
	}
	if got != want {
		t.Errorf("thumbprint mismatch:\n got %s\nwant %s", got, want)
	}
}

// TestRFC7638Thumbprint_EC computes a thumbprint for an EC JWK and asserts
// that it (a) succeeds, (b) is the expected 43-char base64url-no-pad SHA-256,
// (c) is deterministic — same input twice produces the same output.
// No published RFC vector for EC at this depth, so this is a smoke test of
// determinism + shape rather than a wire-format conformance test.
func TestRFC7638Thumbprint_EC(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-256: %v", err)
	}
	jwk, err := MarshalECPublic(&priv.PublicKey, "k", "ES256", "sig")
	if err != nil {
		t.Fatalf("MarshalECPublic: %v", err)
	}
	tp1, err := RFC7638Thumbprint(jwk)
	if err != nil {
		t.Fatalf("RFC7638Thumbprint: %v", err)
	}
	// SHA-256 in base64url-no-pad is 43 chars.
	if len(tp1) != 43 {
		t.Errorf("thumbprint length = %d, want 43", len(tp1))
	}
	tp2, err := RFC7638Thumbprint(jwk)
	if err != nil {
		t.Fatalf("RFC7638Thumbprint (second): %v", err)
	}
	if tp1 != tp2 {
		t.Errorf("non-deterministic thumbprint: %s vs %s", tp1, tp2)
	}
	// Changing kid/alg/use must NOT change the thumbprint — only required
	// members (crv, kty, x, y for EC) participate per RFC 7638 §3.2.
	mutated := jwk
	mutated.Kid = "different-kid"
	mutated.Alg = "different-alg"
	mutated.Use = "different-use"
	tp3, err := RFC7638Thumbprint(mutated)
	if err != nil {
		t.Fatalf("RFC7638Thumbprint (mutated): %v", err)
	}
	if tp3 != tp1 {
		t.Errorf("thumbprint should ignore kid/alg/use but changed: %s vs %s", tp1, tp3)
	}
}

// TestRFC7638Thumbprint_MissingRequiredFields rejects JWKs that lack any of
// the RFC 7638 §3.2 required members.
func TestRFC7638Thumbprint_MissingRequiredFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		jwk  JWK
		want string // substring expected in error
	}{
		{
			name: "EC missing X",
			jwk:  JWK{Kty: "EC", Crv: "P-256", Y: "yyy"},
			want: "x",
		},
		{
			name: "EC missing Y",
			jwk:  JWK{Kty: "EC", Crv: "P-256", X: "xxx"},
			want: "y",
		},
		{
			name: "EC missing crv",
			jwk:  JWK{Kty: "EC", X: "xxx", Y: "yyy"},
			want: "crv",
		},
		{
			name: "RSA missing N",
			jwk:  JWK{Kty: "RSA", E: "AQAB"},
			want: "n",
		},
		{
			name: "RSA missing E",
			jwk:  JWK{Kty: "RSA", N: "nnn"},
			want: "e",
		},
		{
			name: "unsupported kty",
			jwk:  JWK{Kty: "oct"},
			want: "oct",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := RFC7638Thumbprint(tc.jwk)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Errorf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}
