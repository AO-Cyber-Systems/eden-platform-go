package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/url"
	"testing"
)

func newSPCertAndSigner(t *testing.T) (*KMSSigner, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	cert := newRSACert(t, priv)
	backend := &fakeRSAKMSBackend{priv: priv, alg: "RS256", keyID: "sp-key"}
	return &KMSSigner{Signer: backend, KeyID: "sp-key", Cert: cert}, priv
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	return u
}

func TestNewSP_OktaMetadata(t *testing.T) {
	ResetMetadataCache()
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		TenantSlug:  "acme",
		IdpID:       "okta-prod",
		EntityID:    "https://aoid.example.com/sp/acme",
		AcsURL:      mustURL(t, "https://aoid.example.com/sp/acme/acs"),
		MetadataURL: mustURL(t, "https://aoid.example.com/sp/acme/metadata"),
		IDPMetadata: []byte(metadataOktaXML),
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	mw, err := NewSP(opts)
	if err != nil {
		t.Fatalf("NewSP: %v", err)
	}
	if mw == nil {
		t.Fatal("NewSP returned nil middleware")
	}
	if mw.ServiceProvider.EntityID != "https://aoid.example.com/sp/acme" {
		t.Fatalf("EntityID mismatch: %s", mw.ServiceProvider.EntityID)
	}
	if mw.ServiceProvider.AcsURL.String() != "https://aoid.example.com/sp/acme/acs" {
		t.Fatalf("ACS URL mismatch: %s", mw.ServiceProvider.AcsURL.String())
	}
	if mw.ServiceProvider.IDPMetadata == nil {
		t.Fatal("ServiceProvider.IDPMetadata is nil after NewSP")
	}
}

func TestNewSP_EntraMetadata(t *testing.T) {
	ResetMetadataCache()
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		TenantSlug:  "globex",
		IdpID:       "entra-prod",
		EntityID:    "https://aoid.example.com/sp/globex",
		AcsURL:      mustURL(t, "https://aoid.example.com/sp/globex/acs"),
		MetadataURL: mustURL(t, "https://aoid.example.com/sp/globex/metadata"),
		IDPMetadata: []byte(metadataEntraXML),
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	mw, err := NewSP(opts)
	if err != nil {
		t.Fatalf("NewSP entra: %v", err)
	}
	if mw == nil {
		t.Fatal("nil middleware")
	}
	if mw.ServiceProvider.EntityID != "https://aoid.example.com/sp/globex" {
		t.Fatalf("entra EntityID: %s", mw.ServiceProvider.EntityID)
	}
}

func TestNewSP_EmptyMetadataRejected(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "x",
		AcsURL:      mustURL(t, "https://x/acs"),
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: nil,
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for empty IDPMetadata, got nil")
	}
	if !errors.Is(err, ErrSPMissingMetadata) {
		t.Fatalf("expected ErrSPMissingMetadata, got %v", err)
	}
}

func TestNewSP_NilSignerRejected(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "x",
		AcsURL:      mustURL(t, "https://x/acs"),
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: []byte(metadataOktaXML),
		SPCert:      signer.Cert,
		SPSigner:    nil,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for nil SPSigner, got nil")
	}
	if !errors.Is(err, ErrSPMissingSigner) {
		t.Fatalf("expected ErrSPMissingSigner, got %v", err)
	}
}

func TestNewSP_NilCertRejected(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "x",
		AcsURL:      mustURL(t, "https://x/acs"),
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: []byte(metadataOktaXML),
		SPCert:      nil,
		SPSigner:    signer,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for nil SPCert, got nil")
	}
	if !errors.Is(err, ErrSPMissingCert) {
		t.Fatalf("expected ErrSPMissingCert, got %v", err)
	}
}

func TestNewSP_MalformedMetadataWrappedError(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "x",
		AcsURL:      mustURL(t, "https://x/acs"),
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: []byte("<not-saml/>"),
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for malformed metadata, got nil")
	}
	if !errors.Is(err, ErrParseMetadata) {
		t.Fatalf("expected wrapped ErrParseMetadata, got %v", err)
	}
}

func TestNewSP_RequiresEntityID(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "",
		AcsURL:      mustURL(t, "https://x/acs"),
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: []byte(metadataOktaXML),
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for empty EntityID, got nil")
	}
}

func TestNewSP_RequiresAcsURL(t *testing.T) {
	signer, _ := newSPCertAndSigner(t)
	opts := SPOptions{
		EntityID:    "x",
		AcsURL:      nil,
		MetadataURL: mustURL(t, "https://x/metadata"),
		IDPMetadata: []byte(metadataOktaXML),
		SPCert:      signer.Cert,
		SPSigner:    signer,
	}
	_, err := NewSP(opts)
	if err == nil {
		t.Fatal("expected error for nil AcsURL, got nil")
	}
}
