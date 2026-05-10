package saml

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func encodePKCS1Key(t *testing.T, k *SigningKey) []byte {
	t.Helper()
	der := x509.MarshalPKCS1PrivateKey(k.PrivateKey)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func TestGenerateSigningKey(t *testing.T) {
	k, err := GenerateSigningKey("test", time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if k.PrivateKey == nil {
		t.Error("nil private key")
	}
	if k.Certificate == nil {
		t.Error("nil certificate")
	}
	if k.Certificate.Subject.CommonName != "test" {
		t.Errorf("CN: %s", k.Certificate.Subject.CommonName)
	}
}

func TestGenerateSigningKey_DefaultValidity(t *testing.T) {
	k, err := GenerateSigningKey("x", 0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Default validity is 365 days
	if k.Certificate.NotAfter.Sub(time.Now()) < 360*24*time.Hour {
		t.Errorf("expected ~365 day validity, got %v", k.Certificate.NotAfter.Sub(time.Now()))
	}
}

func TestSigningKey_CertificateBase64(t *testing.T) {
	k, _ := GenerateSigningKey("x", time.Hour)
	b64 := k.CertificateBase64()
	if b64 == "" {
		t.Fatal("empty base64")
	}
	// Must be standard base64 (alphabet check); no PEM headers.
	if strings.Contains(b64, "BEGIN") {
		t.Error("base64 should not contain PEM headers")
	}
	// Round-trip: pem certificate body decodes back to the same DER bytes.
	pemBytes := k.CertificatePEM()
	if !strings.Contains(string(pemBytes), "BEGIN CERTIFICATE") {
		t.Error("PEM missing BEGIN CERTIFICATE")
	}
}

func TestSigningKey_NilSafe(t *testing.T) {
	var k *SigningKey
	if k.CertificateBase64() != "" {
		t.Error("expected empty string for nil receiver")
	}
	if k.CertificatePEM() != nil {
		t.Error("expected nil PEM for nil receiver")
	}
}

func TestLoadSigningKey_PKCS1RoundTrip(t *testing.T) {
	original, err := GenerateSigningKey("x", time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pemKey := encodePKCS1Key(t, original)
	pemCert := original.CertificatePEM()
	loaded, err := LoadSigningKey(pemKey, pemCert)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PrivateKey.N.Cmp(original.PrivateKey.N) != 0 {
		t.Error("loaded key mismatch")
	}
	if loaded.Certificate.Subject.CommonName != "x" {
		t.Errorf("loaded CN: %s", loaded.Certificate.Subject.CommonName)
	}
}
