package piv_test

// Test list:
//   - TestExtractUPN_FixtureCertHasUPN — load testdata/sample_piv_cert.pem;
//     ExtractUPN returns "john.doe.1234567890@mil".
//   - TestExtractUPN_FixtureWithoutUPN_ReturnsErrNoUPN — fixture without
//     OtherName UPN extension; assert err is ErrNoUPN.
//   - TestExtractUPN_NilCert — ExtractUPN(nil) returns ("", ErrNoUPN).
//   - TestExtractUPN_CertWithSANButNoOtherName — cert with only rfc822Name
//     SAN; assert ErrNoUPN.
//   - TestExtractUPN_MalformedSANBytes — synthetic cert with malformed SAN
//     ext; assert returned error wraps a parse failure.

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/mtls/piv"
)

func loadCert(t *testing.T, name string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("no PEM block in %s", name)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return cert
}

func TestExtractUPN_FixtureCertHasUPN(t *testing.T) {
	cert := loadCert(t, "sample_piv_cert.pem")
	upn, err := piv.ExtractUPN(cert)
	if err != nil {
		t.Fatalf("ExtractUPN err: %v", err)
	}
	want := "john.doe.1234567890@mil"
	if upn != want {
		t.Errorf("ExtractUPN = %q, want %q", upn, want)
	}
}

func TestExtractUPN_FixtureWithoutUPN_ReturnsErrNoUPN(t *testing.T) {
	cert := loadCert(t, "sample_piv_no_upn.pem")
	upn, err := piv.ExtractUPN(cert)
	if !errors.Is(err, piv.ErrNoUPN) {
		t.Fatalf("ExtractUPN err = %v, want ErrNoUPN", err)
	}
	if upn != "" {
		t.Errorf("ExtractUPN upn = %q, want empty", upn)
	}
}

func TestExtractUPN_NilCert(t *testing.T) {
	upn, err := piv.ExtractUPN(nil)
	if !errors.Is(err, piv.ErrNoUPN) {
		t.Fatalf("ExtractUPN(nil) err = %v, want ErrNoUPN", err)
	}
	if upn != "" {
		t.Errorf("ExtractUPN(nil) upn = %q, want empty", upn)
	}
}

func TestExtractUPN_CertWithSANButNoOtherName(t *testing.T) {
	// Same fixture as no-UPN — its SAN is rfc822Name only, exercising
	// the "SAN present but no OtherName" branch specifically.
	cert := loadCert(t, "sample_piv_no_upn.pem")
	if len(cert.EmailAddresses) == 0 {
		t.Fatalf("fixture invariant broken: expected rfc822Name SAN, got Extensions=%v", cert.Extensions)
	}
	_, err := piv.ExtractUPN(cert)
	if !errors.Is(err, piv.ErrNoUPN) {
		t.Fatalf("ExtractUPN err = %v, want ErrNoUPN", err)
	}
}

func TestExtractUPN_MalformedSANBytes(t *testing.T) {
	// Build a cert in memory with a SAN extension whose value bytes are
	// garbage. asn1.Unmarshal must fail on the outer SEQUENCE OF
	// GeneralName, and ExtractUPN must surface the wrapped parse error.
	cert := &x509.Certificate{
		Extensions: []pkix.Extension{
			{
				Id:    asn1.ObjectIdentifier{2, 5, 29, 17}, // SAN
				Value: []byte{0xFF, 0x00, 0x42, 0x13},      // garbage
			},
		},
	}
	upn, err := piv.ExtractUPN(cert)
	if err == nil {
		t.Fatalf("expected parse error, got upn=%q", upn)
	}
	if errors.Is(err, piv.ErrNoUPN) {
		t.Fatalf("expected wrapped parse error, got ErrNoUPN")
	}
	if upn != "" {
		t.Errorf("expected empty upn, got %q", upn)
	}
}
