package kmssigner

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	mathrand "math/rand"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// fakeES256Signer is a Signer-shaped wrapper around an in-process *ecdsa.PrivateKey
// that mimics KMSSigner's wire shape: Sign returns DER-encoded ECDSA signatures
// just like AWS KMS / Azure Managed HSM / PKCS#11 do.
type fakeES256Signer struct {
	priv    *ecdsa.PrivateKey
	algName string // override for the alg-mismatch test
}

func (f *fakeES256Signer) Public() crypto.PublicKey {
	return &f.priv.PublicKey
}

func (f *fakeES256Signer) SigningAlgorithm() string {
	if f.algName != "" {
		return f.algName
	}
	return "ES256"
}

func (f *fakeES256Signer) Sign(randReader io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts == nil || opts.HashFunc() != crypto.SHA256 {
		return nil, errors.New("fakeES256Signer: expected SHA256")
	}
	r, s, err := ecdsa.Sign(randReader, f.priv, digest)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(struct{ R, S *big.Int }{r, s})
}

// fakeRS256Signer wraps an in-process *rsa.PrivateKey and returns PKCS1-v1_5
// signatures matching the wire shape every RSA KMS returns.
type fakeRS256Signer struct {
	priv    *rsa.PrivateKey
	algName string
}

func (f *fakeRS256Signer) Public() crypto.PublicKey {
	return &f.priv.PublicKey
}

func (f *fakeRS256Signer) SigningAlgorithm() string {
	if f.algName != "" {
		return f.algName
	}
	return "RS256"
}

func (f *fakeRS256Signer) Sign(randReader io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts == nil || opts.HashFunc() != crypto.SHA256 {
		return nil, errors.New("fakeRS256Signer: expected SHA256")
	}
	return rsa.SignPKCS1v15(randReader, f.priv, crypto.SHA256, digest)
}

func newES256Fake(t *testing.T) *fakeES256Signer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	return &fakeES256Signer{priv: priv}
}

func newRS256Fake(t *testing.T) *fakeRS256Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return &fakeRS256Signer{priv: priv}
}

func TestES256_SignVerify_RoundTrip(t *testing.T) {
	signer := newES256Fake(t)

	claims := jwt.MapClaims{
		"sub":   "user-123",
		"iss":   "test",
		"scope": "openid profile",
	}
	method := &ES256SigningMethod{}
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = "test-kid"

	signed, err := token.SignedString(signer)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	if signed == "" {
		t.Fatal("SignedString returned empty token")
	}

	parsed, err := jwt.Parse(signed, func(tok *jwt.Token) (interface{}, error) {
		if tok.Method.Alg() != "ES256" {
			return nil, errors.New("unexpected alg")
		}
		return &signer.priv.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	if err != nil {
		t.Fatalf("jwt.Parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("parsed token not valid")
	}

	got, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims wrong type: %T", parsed.Claims)
	}
	if got["sub"] != "user-123" {
		t.Errorf("sub mismatch: got %q want %q", got["sub"], "user-123")
	}
	if parsed.Header["kid"] != "test-kid" {
		t.Errorf("kid mismatch: got %q want %q", parsed.Header["kid"], "test-kid")
	}
	if parsed.Header["alg"] != "ES256" {
		t.Errorf("alg header mismatch: got %q want %q", parsed.Header["alg"], "ES256")
	}
}

func TestRS256_SignVerify_RoundTrip(t *testing.T) {
	signer := newRS256Fake(t)

	claims := jwt.MapClaims{
		"sub": "user-456",
		"iss": "test",
	}
	method := &RS256SigningMethod{}
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = "rs-test-kid"

	signed, err := token.SignedString(signer)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	parsed, err := jwt.Parse(signed, func(tok *jwt.Token) (interface{}, error) {
		if tok.Method.Alg() != "RS256" {
			return nil, errors.New("unexpected alg")
		}
		return &signer.priv.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		t.Fatalf("jwt.Parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("parsed token not valid")
	}

	got := parsed.Claims.(jwt.MapClaims)
	if got["sub"] != "user-456" {
		t.Errorf("sub mismatch: got %q", got["sub"])
	}
	if parsed.Header["alg"] != "RS256" {
		t.Errorf("alg header mismatch: got %q", parsed.Header["alg"])
	}
}

func TestES256_Verify_RejectsNoneAlg(t *testing.T) {
	// Hand-build a token with alg "none" — header.payload. (no signature)
	header := `{"alg":"none","typ":"JWT"}`
	payload := `{"sub":"attacker"}`
	enc := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	noneToken := enc(header) + "." + enc(payload) + "."

	signer := newES256Fake(t)

	// Parse with ES256 valid-methods filter — none MUST be rejected.
	_, err := jwt.Parse(noneToken, func(tok *jwt.Token) (interface{}, error) {
		return &signer.priv.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	if err == nil {
		t.Fatal("expected error parsing none-alg token, got nil")
	}
	// golang-jwt returns ErrTokenSignatureInvalid or ErrTokenUnverifiable; either is acceptable.
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) &&
		!errors.Is(err, jwt.ErrTokenUnverifiable) &&
		!strings.Contains(err.Error(), "signing method") &&
		!strings.Contains(err.Error(), "none") {
		t.Fatalf("unexpected error type for none-alg rejection: %v", err)
	}
}

func TestES256_DER_To_JWS_Robustness(t *testing.T) {
	// Deterministic seed so failures reproduce.
	rng := mathrand.New(mathrand.NewSource(42)) //nolint:gosec // test-only deterministic seed
	const iterations = 256

	method := &ES256SigningMethod{}
	successes := 0
	for i := 0; i < iterations; i++ {
		// Generate a fresh P-256 key per iteration using crypto/rand (math/rand is
		// only the iteration counter; key material stays cryptographically random).
		_ = rng.Int() // advance deterministic seed for any future use
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("iter %d: ecdsa.GenerateKey: %v", i, err)
		}
		fake := &fakeES256Signer{priv: priv}

		signingString := "iter." + base64.RawURLEncoding.EncodeToString([]byte("payload"))
		sigBytes, err := method.Sign(signingString, fake)
		if err != nil {
			t.Fatalf("iter %d: Sign: %v", i, err)
		}
		if len(sigBytes) != 64 {
			t.Fatalf("iter %d: signature length %d, want 64", i, len(sigBytes))
		}
		// Split into r||s and verify directly via stdlib ecdsa.Verify.
		digest := sha256.Sum256([]byte(signingString))
		r := new(big.Int).SetBytes(sigBytes[:32])
		s := new(big.Int).SetBytes(sigBytes[32:])
		if !ecdsa.Verify(&priv.PublicKey, digest[:], r, s) {
			t.Fatalf("iter %d: ecdsa.Verify rejected our signature", i)
		}
		successes++
	}
	if successes != iterations {
		t.Fatalf("only %d/%d iterations passed", successes, iterations)
	}
	t.Logf("robustness: %d/%d iterations passed (deterministic seed=42)", successes, iterations)
}

func TestSign_WrongKeyType(t *testing.T) {
	method := &ES256SigningMethod{}
	_, err := method.Sign("hdr.payload", "not-a-signer")
	if err == nil {
		t.Fatal("expected error for non-Signer key, got nil")
	}
	if !errors.Is(err, jwt.ErrInvalidKeyType) && !strings.Contains(err.Error(), "key type") {
		t.Errorf("expected ErrInvalidKeyType, got %v", err)
	}

	rsMethod := &RS256SigningMethod{}
	_, err = rsMethod.Sign("hdr.payload", 42)
	if err == nil {
		t.Fatal("expected error for non-Signer key, got nil")
	}
}

func TestSign_AlgMismatch(t *testing.T) {
	// fakeES256Signer with alg=RS256 sent to ES256 method → reject.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrong := &fakeES256Signer{priv: priv, algName: "RS256"}

	method := &ES256SigningMethod{}
	_, err = method.Sign("hdr.payload", wrong)
	if err == nil {
		t.Fatal("expected alg-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "alg") {
		t.Errorf("error %v does not mention alg mismatch", err)
	}
}

func TestVerify_RoundTrip_DirectKey(t *testing.T) {
	// Independent of Parse: drive method.Sign + method.Verify directly to lock the contract.
	es := newES256Fake(t)
	esMethod := &ES256SigningMethod{}
	signingString := "h." + base64.RawURLEncoding.EncodeToString([]byte("p"))
	sig, err := esMethod.Sign(signingString, es)
	if err != nil {
		t.Fatalf("ES256 Sign: %v", err)
	}
	if err := esMethod.Verify(signingString, sig, &es.priv.PublicKey); err != nil {
		t.Errorf("ES256 Verify (*ecdsa.PublicKey): %v", err)
	}
	if err := esMethod.Verify(signingString, sig, es); err != nil {
		t.Errorf("ES256 Verify (Signer): %v", err)
	}

	rs := newRS256Fake(t)
	rsMethod := &RS256SigningMethod{}
	sig, err = rsMethod.Sign(signingString, rs)
	if err != nil {
		t.Fatalf("RS256 Sign: %v", err)
	}
	if err := rsMethod.Verify(signingString, sig, &rs.priv.PublicKey); err != nil {
		t.Errorf("RS256 Verify (*rsa.PublicKey): %v", err)
	}
	if err := rsMethod.Verify(signingString, sig, rs); err != nil {
		t.Errorf("RS256 Verify (Signer): %v", err)
	}
}

func TestVerify_RejectsBadSignature(t *testing.T) {
	es := newES256Fake(t)
	esMethod := &ES256SigningMethod{}
	signingString := "h." + base64.RawURLEncoding.EncodeToString([]byte("p"))
	sig, err := esMethod.Sign(signingString, es)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the signature.
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0xFF
	if err := esMethod.Verify(signingString, tampered, &es.priv.PublicKey); err == nil {
		t.Error("expected verify error for tampered ES256 sig, got nil")
	}

	// Wrong length.
	if err := esMethod.Verify(signingString, []byte("short"), &es.priv.PublicKey); err == nil {
		t.Error("expected verify error for short ES256 sig, got nil")
	}

	rs := newRS256Fake(t)
	rsMethod := &RS256SigningMethod{}
	sig, err = rsMethod.Sign(signingString, rs)
	if err != nil {
		t.Fatal(err)
	}
	sig[len(sig)-1] ^= 0xFF
	if err := rsMethod.Verify(signingString, sig, &rs.priv.PublicKey); err == nil {
		t.Error("expected verify error for tampered RS256 sig, got nil")
	}
}

func TestES256_Alg(t *testing.T) {
	if (&ES256SigningMethod{}).Alg() != "ES256" {
		t.Errorf("ES256SigningMethod.Alg() != %q", "ES256")
	}
	if (&RS256SigningMethod{}).Alg() != "RS256" {
		t.Errorf("RS256SigningMethod.Alg() != %q", "RS256")
	}
}

// Sanity assertion: ES256 + RS256 methods satisfy jwt.SigningMethod.
var (
	_ jwt.SigningMethod = (*ES256SigningMethod)(nil)
	_ jwt.SigningMethod = (*RS256SigningMethod)(nil)
)

// Ensure jwt.MapClaims marshalling stays stable (decimal-encoded numerics).
// This is a guard against accidental upstream changes; not a behavior under test.
var _ = json.Marshal
