// Package goxmldsig (forked subpackage) tests for the crypto.Signer-based
// SigningContext factory used by platform/saml.
//
// These tests deliberately do not import platform/kms — fake crypto.Signer
// values back the test cases so the package stays a leaf import.
package goxmldsig

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// rsaSigner is a thin crypto.Signer wrapper that delegates to an in-process
// *rsa.PrivateKey but reports through the crypto.Signer surface only —
// proving the factory does not depend on the concrete *rsa.PrivateKey type.
type rsaSigner struct {
	priv *rsa.PrivateKey
}

func (s *rsaSigner) Public() crypto.PublicKey { return &s.priv.PublicKey }
func (s *rsaSigner) Sign(r io.Reader, d []byte, o crypto.SignerOpts) ([]byte, error) {
	return s.priv.Sign(r, d, o)
}

// ecdsaSigner does the same for an ECDSA P-256 key, returning ASN.1-DER
// signatures (the wire format every cloud KMS uses).
type ecdsaSigner struct {
	priv *ecdsa.PrivateKey
}

func (s *ecdsaSigner) Public() crypto.PublicKey { return &s.priv.PublicKey }
func (s *ecdsaSigner) Sign(r io.Reader, d []byte, _ crypto.SignerOpts) ([]byte, error) {
	rr, ss, err := ecdsa.Sign(r, s.priv, d)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(struct{ R, S *big.Int }{rr, ss})
}

func newSelfSignedRSA(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
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
	return priv, cert
}

func newSelfSignedECDSA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-ec"},
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
	return priv, cert
}

// TestNewSigningContextSigner_RSA_SignAndValidate proves that:
//  1. The factory returns a non-nil SigningContext.
//  2. The context signs an enveloped XML element.
//  3. The standard goxmldsig validator (using the cert's public key)
//     accepts the resulting signature after a serialize → re-parse cycle
//     (matches the upstream validate_test.go pattern: validate on a fresh
//     document tree rather than on the in-memory etree the signer mutated).
func TestNewSigningContextSigner_RSA_SignAndValidate(t *testing.T) {
	priv, cert := newSelfSignedRSA(t)
	signer := &rsaSigner{priv: priv}

	ctx, err := NewSigningContextSigner(signer, [][]byte{cert.Raw})
	if err != nil {
		t.Fatalf("NewSigningContextSigner: %v", err)
	}
	if ctx == nil {
		t.Fatal("returned nil SigningContext")
	}

	// Build a single-root <payload ID="abc">hi</payload> document and sign it.
	// goxmldsig's enveloped validator expects the Reference URI to resolve
	// against the document root, so we sign the root element directly.
	doc := etree.NewDocument()
	payload := doc.CreateElement("payload")
	payload.CreateAttr("ID", "abc")
	payload.SetText("hi")

	signed, err := ctx.SignEnveloped(payload)
	if err != nil {
		t.Fatalf("SignEnveloped: %v", err)
	}

	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signed)
	bs, err := signedDoc.WriteToBytes()
	if err != nil {
		t.Fatalf("WriteToBytes: %v", err)
	}

	freshDoc := etree.NewDocument()
	if err := freshDoc.ReadFromBytes(bs); err != nil {
		t.Fatalf("ReadFromBytes: %v", err)
	}

	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	validator := dsig.NewDefaultValidationContext(store)

	if _, err := validator.Validate(freshDoc.Root()); err != nil {
		t.Fatalf("Validate signed element: %v", err)
	}
}

// TestNewSigningContextSigner_ECDSA_SignAndValidate ensures the same factory
// works with ECDSA-P256 keys — proving the dsig context's getPublicKeyAlgorithm
// path correctly handles ECDSA (the whole point of accepting crypto.Signer
// rather than *rsa.PrivateKey).
func TestNewSigningContextSigner_ECDSA_SignAndValidate(t *testing.T) {
	priv, cert := newSelfSignedECDSA(t)
	signer := &ecdsaSigner{priv: priv}

	ctx, err := NewSigningContextSigner(signer, [][]byte{cert.Raw})
	if err != nil {
		t.Fatalf("NewSigningContextSigner: %v", err)
	}
	if err := ctx.SetSignatureMethod(dsig.ECDSASHA256SignatureMethod); err != nil {
		t.Fatalf("SetSignatureMethod ECDSA: %v", err)
	}

	doc := etree.NewDocument()
	payload := doc.CreateElement("payload")
	payload.CreateAttr("ID", "ec-1")
	payload.SetText("ec hi")

	signed, err := ctx.SignEnveloped(payload)
	if err != nil {
		t.Fatalf("SignEnveloped (ECDSA): %v", err)
	}

	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signed)
	bs, err := signedDoc.WriteToBytes()
	if err != nil {
		t.Fatalf("WriteToBytes: %v", err)
	}
	freshDoc := etree.NewDocument()
	if err := freshDoc.ReadFromBytes(bs); err != nil {
		t.Fatalf("ReadFromBytes: %v", err)
	}

	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	validator := dsig.NewDefaultValidationContext(store)
	if _, err := validator.Validate(freshDoc.Root()); err != nil {
		t.Fatalf("Validate ECDSA-signed element: %v", err)
	}
}

func TestNewSigningContextSigner_NilSigner(t *testing.T) {
	if _, err := NewSigningContextSigner(nil, nil); err == nil {
		t.Fatal("expected error for nil signer, got nil")
	}
}

func TestNewSigningContextSigner_MissingCertChain(t *testing.T) {
	priv, _ := newSelfSignedRSA(t)
	if _, err := NewSigningContextSigner(&rsaSigner{priv: priv}, nil); err == nil {
		t.Fatal("expected error for empty cert chain, got nil")
	}
	if _, err := NewSigningContextSigner(&rsaSigner{priv: priv}, [][]byte{}); err == nil {
		t.Fatal("expected error for empty cert chain (length 0), got nil")
	}
}

// TestMemoryX509KeyStoreSigner_GetKeyPair confirms the keystore implementation
// returns the *crypto.Signer* (not *rsa.PrivateKey) when called, satisfying
// the TRD truth: "MemoryX509KeyStoreSigner whose GetKeyPair() returns the
// crypto.Signer (not *rsa.PrivateKey)".
func TestMemoryX509KeyStoreSigner_GetKeyPair(t *testing.T) {
	priv, cert := newSelfSignedRSA(t)
	signer := &rsaSigner{priv: priv}
	ks := &MemoryX509KeyStoreSigner{Signer: signer, CertChain: [][]byte{cert.Raw}}

	gotSigner, gotCerts, err := ks.GetKeyPair()
	if err != nil {
		t.Fatalf("GetKeyPair: %v", err)
	}
	if gotSigner != signer {
		t.Fatalf("GetKeyPair returned different signer; want %p got %p", signer, gotSigner)
	}
	if len(gotCerts) != 1 || !bytes.Equal(gotCerts[0], cert.Raw) {
		t.Fatalf("GetKeyPair returned wrong cert chain: %v", gotCerts)
	}
}

func TestMemoryX509KeyStoreSigner_GetChain(t *testing.T) {
	priv, cert := newSelfSignedRSA(t)
	ks := &MemoryX509KeyStoreSigner{Signer: &rsaSigner{priv: priv}, CertChain: [][]byte{cert.Raw}}

	chain, err := ks.GetChain()
	if err != nil {
		t.Fatalf("GetChain: %v", err)
	}
	if len(chain) != 1 || !bytes.Equal(chain[0], cert.Raw) {
		t.Fatalf("unexpected chain: %v", chain)
	}
}

// sentinelErrorSigner returns a known error from Sign so we can verify the
// error path inside NewSigningContextSigner-produced contexts.
type sentinelErrorSigner struct {
	pub crypto.PublicKey
	err error
}

func (s *sentinelErrorSigner) Public() crypto.PublicKey { return s.pub }
func (s *sentinelErrorSigner) Sign(_ io.Reader, _ []byte, _ crypto.SignerOpts) ([]byte, error) {
	return nil, s.err
}

func TestNewSigningContextSigner_PropagatesSignError(t *testing.T) {
	priv, cert := newSelfSignedRSA(t)
	wantErr := errors.New("kms unavailable")
	signer := &sentinelErrorSigner{pub: &priv.PublicKey, err: wantErr}

	ctx, err := NewSigningContextSigner(signer, [][]byte{cert.Raw})
	if err != nil {
		t.Fatalf("NewSigningContextSigner: %v", err)
	}

	doc := etree.NewDocument()
	root := doc.CreateElement("root")
	payload := root.CreateElement("payload")
	payload.CreateAttr("ID", "abc")
	if _, err := ctx.SignEnveloped(payload); !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
}
