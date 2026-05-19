package saml

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"hash"
	"io"
	"math/big"
	"testing"
	"time"
)

// fakeRSAKMSBackend implements platform/kms.KMSSigner-shaped behaviour by
// delegating to an in-process *rsa.PrivateKey. SAML's wrapper must drive
// signatures through this interface only.
type fakeRSAKMSBackend struct {
	priv      *rsa.PrivateKey
	alg       string
	keyID     string
	calls     int
	lastHash  crypto.Hash
	lastError error // injected error
}

func (b *fakeRSAKMSBackend) Public() crypto.PublicKey  { return &b.priv.PublicKey }
func (b *fakeRSAKMSBackend) KeyID() string             { return b.keyID }
func (b *fakeRSAKMSBackend) SigningAlgorithm() string  { return b.alg }
func (b *fakeRSAKMSBackend) HealthCheck(_ interface{}) error { return nil }

func (b *fakeRSAKMSBackend) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	b.calls++
	if b.lastError != nil {
		return nil, b.lastError
	}
	b.lastHash = opts.HashFunc()
	return rsa.SignPKCS1v15(r, b.priv, opts.HashFunc(), digest)
}

// fakeECDSAKMSBackend returns ASN.1-DER ECDSA signatures (matches all cloud
// KMS providers AOID uses in production).
type fakeECDSAKMSBackend struct {
	priv  *ecdsa.PrivateKey
	keyID string
}

func (b *fakeECDSAKMSBackend) Public() crypto.PublicKey { return &b.priv.PublicKey }
func (b *fakeECDSAKMSBackend) KeyID() string            { return b.keyID }
func (b *fakeECDSAKMSBackend) SigningAlgorithm() string { return "ES256" }
func (b *fakeECDSAKMSBackend) HealthCheck(_ interface{}) error { return nil }

func (b *fakeECDSAKMSBackend) Sign(r io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	rr, ss, err := ecdsa.Sign(r, b.priv, digest)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(struct{ R, S *big.Int }{rr, ss})
}

func newRSACert(t *testing.T, priv *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "aoid-saml-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

func newECDSACert(t *testing.T, priv *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "aoid-saml-test-ec"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

func TestKMSSigner_PublicReturnsCertPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	backend := &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "kms://kid-1"}

	signer := &KMSSigner{Signer: backend, KeyID: "kms://kid-1", Cert: cert}
	pub := signer.Public()
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("Public() not *rsa.PublicKey: %T", pub)
	}
	if rsaPub.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("Public().N != cert.PublicKey.N")
	}
}

func TestKMSSigner_RSA_SignVerify_TableDriven(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	backend := &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "kid-rsa"}
	signer := &KMSSigner{Signer: backend, KeyID: "kid-rsa", Cert: cert}

	cases := []struct {
		name  string
		hash  crypto.Hash
		newFn func() hash.Hash
	}{
		{"SHA256", crypto.SHA256, sha256.New},
		{"SHA384", crypto.SHA384, sha512.New384},
		{"SHA512", crypto.SHA512, sha512.New},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.newFn()
			_, _ = h.Write([]byte("payload-" + tc.name))
			digest := h.Sum(nil)

			sig, err := signer.Sign(rand.Reader, digest, tc.hash)
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			if err := rsa.VerifyPKCS1v15(cert.PublicKey.(*rsa.PublicKey), tc.hash, digest, sig); err != nil {
				t.Fatalf("VerifyPKCS1v15: %v", err)
			}
			if backend.lastHash != tc.hash {
				t.Fatalf("backend.lastHash=%v want %v", backend.lastHash, tc.hash)
			}
		})
	}
}

func TestKMSSigner_ECDSA_SignVerify(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	cert := newECDSACert(t, priv)
	backend := &fakeECDSAKMSBackend{priv: priv, keyID: "kid-ec"}
	signer := &KMSSigner{Signer: backend, KeyID: "kid-ec", Cert: cert}

	digest := sha256.Sum256([]byte("ec-payload"))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// ECDSA signature should be ASN.1-DER. Decode and verify with the cert
	// pubkey.
	var ecSig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(sig, &ecSig); err != nil {
		t.Fatalf("asn1.Unmarshal ECDSA: %v", err)
	}
	if !ecdsa.Verify(cert.PublicKey.(*ecdsa.PublicKey), digest[:], ecSig.R, ecSig.S) {
		t.Fatal("ecdsa.Verify rejected ASN.1-DER signature")
	}
}

func TestKMSSigner_UnsupportedHash(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert := newRSACert(t, priv)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "kid-rsa"},
		KeyID:  "kid-rsa",
		Cert:   cert,
	}

	// SHA-1 (crypto.SHA1) is explicitly NOT in the allowed set per the
	// TRD truth — SAML signers under AOID's policy use SHA-256/384/512 only.
	_, err := signer.Sign(rand.Reader, make([]byte, 20), crypto.SHA1)
	if err == nil {
		t.Fatal("expected error for SHA1, got nil")
	}
	if !errors.Is(err, ErrUnsupportedHash) {
		t.Fatalf("expected ErrUnsupportedHash, got %v", err)
	}
}

func TestKMSSigner_NilCert(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := &KMSSigner{
		Signer: &fakeRSAKMSBackend{priv: priv, alg: "RS256"},
		Cert:   nil,
	}
	defer func() {
		if r := recover(); r != nil {
			// expected: nil-cert Public() should be a defined error or nil
			t.Fatalf("Public() panicked on nil cert: %v", r)
		}
	}()
	if got := signer.Public(); got != nil {
		t.Fatalf("Public() with nil cert returned non-nil: %T", got)
	}
}

func TestKMSSigner_PropagatesBackendError(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert := newRSACert(t, priv)
	want := errors.New("kms unavailable")
	backend := &fakeRSAKMSBackend{priv: priv, alg: "RS256", lastError: want}
	signer := &KMSSigner{Signer: backend, KeyID: "kid-rsa", Cert: cert}

	_, err := signer.Sign(rand.Reader, make([]byte, 32), crypto.SHA256)
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
}
