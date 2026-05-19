package spiffe

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestLeafTemplateSVID_Shape(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/x")
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := time.Now().Add(time.Hour)
	tmpl := LeafTemplateSVID(id, notBefore, notAfter)
	if tmpl == nil {
		t.Fatal("LeafTemplateSVID returned nil")
	}

	// Subject MUST be empty per X.509-SVID §3.
	if tmpl.Subject.CommonName != "" {
		t.Errorf("Subject.CommonName=%q, want empty", tmpl.Subject.CommonName)
	}
	if len(tmpl.Subject.Names) != 0 {
		t.Errorf("len(Subject.Names)=%d, want 0", len(tmpl.Subject.Names))
	}
	if len(tmpl.Subject.ExtraNames) != 0 {
		t.Errorf("len(Subject.ExtraNames)=%d, want 0", len(tmpl.Subject.ExtraNames))
	}

	// URIs must have exactly one entry, the SPIFFE id, with Scheme=spiffe.
	if len(tmpl.URIs) != 1 {
		t.Fatalf("len(URIs)=%d, want 1", len(tmpl.URIs))
	}
	if tmpl.URIs[0].Scheme != "spiffe" {
		t.Errorf("URIs[0].Scheme=%q, want spiffe", tmpl.URIs[0].Scheme)
	}
	if tmpl.URIs[0].Host != "aoid.local" {
		t.Errorf("URIs[0].Host=%q, want aoid.local", tmpl.URIs[0].Host)
	}
	if tmpl.URIs[0].Path != "/sa/x" {
		t.Errorf("URIs[0].Path=%q, want /sa/x", tmpl.URIs[0].Path)
	}

	// DNSNames MUST be nil (URI SAN only per spec §3).
	if tmpl.DNSNames != nil {
		t.Errorf("DNSNames=%v, want nil", tmpl.DNSNames)
	}
	// EmailAddresses and IPAddresses likewise.
	if tmpl.EmailAddresses != nil {
		t.Errorf("EmailAddresses=%v, want nil", tmpl.EmailAddresses)
	}
	if tmpl.IPAddresses != nil {
		t.Errorf("IPAddresses=%v, want nil", tmpl.IPAddresses)
	}

	// IsCA must be false; BasicConstraintsValid must be true so the
	// CA:FALSE basic-constraint extension is serialized.
	if tmpl.IsCA {
		t.Error("IsCA=true, want false")
	}
	if !tmpl.BasicConstraintsValid {
		t.Error("BasicConstraintsValid=false, want true")
	}

	// KeyUsage must include DigitalSignature.
	// SPIFFE X.509-SVID §3: leaf is for workload identity — DigitalSignature is required.
	// KeyEncipherment is explicitly NOT required (and discouraged for ECDSA keys).
	if tmpl.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("KeyUsage missing DigitalSignature")
	}
	if tmpl.KeyUsage&x509.KeyUsageKeyEncipherment != 0 {
		t.Error("KeyUsage must NOT include KeyEncipherment (SPIFFE spec discourages for ECDSA)")
	}
	if tmpl.KeyUsage&x509.KeyUsageCertSign != 0 {
		t.Error("KeyUsage must NOT include CertSign (leaf, not CA)")
	}

	// ExtKeyUsage must include ClientAuth + ServerAuth (workload identity is bidirectional).
	hasClient := false
	hasServer := false
	for _, eku := range tmpl.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			hasClient = true
		}
		if eku == x509.ExtKeyUsageServerAuth {
			hasServer = true
		}
	}
	if !hasClient {
		t.Error("ExtKeyUsage missing ClientAuth")
	}
	if !hasServer {
		t.Error("ExtKeyUsage missing ServerAuth")
	}

	// SerialNumber MUST be nil — pki.CA assigns at signing time.
	if tmpl.SerialNumber != nil {
		t.Errorf("SerialNumber=%v, want nil (caller assigns)", tmpl.SerialNumber)
	}
}

func TestLeafTemplateSVID_NotBeforeNotAfterRespected(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/x")
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	tmpl := LeafTemplateSVID(id, notBefore, notAfter)
	if !tmpl.NotBefore.Equal(notBefore) {
		t.Errorf("NotBefore=%v, want %v", tmpl.NotBefore, notBefore)
	}
	if !tmpl.NotAfter.Equal(notAfter) {
		t.Errorf("NotAfter=%v, want %v", tmpl.NotAfter, notAfter)
	}
}

func TestLeafTemplateSVID_FreshTemplatePerCall(t *testing.T) {
	t.Parallel()

	id, err := ParseID("spiffe://aoid.local/sa/x")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a := LeafTemplateSVID(id, now, now.Add(time.Hour))
	b := LeafTemplateSVID(id, now, now.Add(time.Hour))
	if a == b {
		t.Fatal("LeafTemplateSVID returned the same pointer twice — must be fresh per call")
	}
	// Mutating a must NOT affect b.
	a.DNSNames = []string{"injected.example"}
	if b.DNSNames != nil {
		t.Error("template b was mutated through alias of a")
	}
	// URIs slices must be independent.
	if &a.URIs[0] == &b.URIs[0] {
		// Different *url.URL pointers OK if they alias the same underlying object — assert not.
	}
	a.URIs[0].Path = "/HIJACKED"
	if b.URIs[0].Path == "/HIJACKED" {
		t.Error("URIs[0] is shared between calls — must be independent *url.URL")
	}
}

func TestMaxSVIDTTL_Constant(t *testing.T) {
	t.Parallel()

	if MaxSVIDTTL != 24*time.Hour {
		t.Errorf("MaxSVIDTTL=%v, want 24h", MaxSVIDTTL)
	}
}

func TestDefaultSVIDTTL_Constant(t *testing.T) {
	t.Parallel()

	if DefaultSVIDTTL != 1*time.Hour {
		t.Errorf("DefaultSVIDTTL=%v, want 1h", DefaultSVIDTTL)
	}
	if DefaultSVIDTTL > MaxSVIDTTL {
		t.Errorf("DefaultSVIDTTL=%v exceeds MaxSVIDTTL=%v", DefaultSVIDTTL, MaxSVIDTTL)
	}
}
