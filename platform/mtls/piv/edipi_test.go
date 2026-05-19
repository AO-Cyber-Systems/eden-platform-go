package piv_test

// Test list:
//   - TestExtractEDIPI_FromCACFixture — load testdata/sample_cac_cert.pem;
//     ExtractEDIPI returns "1234567890", nil.
//   - TestExtractEDIPI_NonEDIPIUPN — synthesise a cert whose UPN does not
//     match ^[0-9]{10}@mil$; assert ErrInvalidEDIPI.
//   - TestExtractEDIPI_NoUPN_ReturnsErrNoEDIPI — load
//     testdata/sample_piv_no_upn.pem; assert ErrNoEDIPI.
//   - TestExtractEDIPI_NilCert_ReturnsErrNoEDIPI — nil input → ErrNoEDIPI.
//   - TestExtractEDIPI_TestEDIPIRejectedByDefault — cert with UPN
//     0000000001@mil → ErrInvalidEDIPI when RejectTestEDIPI is true.
//   - TestExtractEDIPI_TestEDIPIAcceptedWhenOverridden — same cert, after
//     SetRejectTestEDIPIForTest(t, false) → returns "0000000001", nil.
//   - TestExtractEDIPI_ConcurrentReads — 100 goroutines call ExtractEDIPI
//     in parallel; no race when run with -race.

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/mtls/piv"
)

func loadCertEDIPI(t *testing.T, name string) *x509.Certificate {
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

// craftUPNCert returns a stub *x509.Certificate with a SubjectAltName
// extension carrying a single Microsoft-UPN OtherName SAN whose value is
// the supplied UTF8String.  Used to drive the EDIPI parser without
// regenerating openssl fixtures for every variant.
func craftUPNCert(t *testing.T, upn string) *x509.Certificate {
	t.Helper()

	upnValueBytes, err := asn1.Marshal(upn)
	if err != nil {
		t.Fatalf("marshal upn utf8: %v", err)
	}
	// Wrap value as [0] EXPLICIT
	explicitWrapped, err := asn1.Marshal(asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        0,
		IsCompound: true,
		Bytes:      upnValueBytes,
	})
	if err != nil {
		t.Fatalf("marshal [0] EXPLICIT wrapper: %v", err)
	}

	otherNameContent := struct {
		OID     asn1.ObjectIdentifier
		Wrapper asn1.RawValue
	}{
		OID: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3},
	}
	if _, err := asn1.Unmarshal(explicitWrapped, &otherNameContent.Wrapper); err != nil {
		t.Fatalf("unmarshal back wrapper: %v", err)
	}

	otherNameDER, err := asn1.Marshal(otherNameContent)
	if err != nil {
		t.Fatalf("marshal OtherName SEQUENCE: %v", err)
	}
	// otherNameDER is a SEQUENCE; turn it into IMPLICIT [0] GeneralName by
	// replacing the leading tag byte 0x30 with 0xA0 (context-specific,
	// constructed, tag 0).
	otherNameGN := make([]byte, len(otherNameDER))
	copy(otherNameGN, otherNameDER)
	otherNameGN[0] = 0xA0

	// Wrap [GeneralName] into a SEQUENCE OF GeneralName (the SAN value).
	sanValue, err := asn1.Marshal([]asn1.RawValue{{FullBytes: otherNameGN}})
	if err != nil {
		t.Fatalf("marshal SAN seq: %v", err)
	}

	return &x509.Certificate{
		Extensions: []pkix.Extension{
			{
				Id:    asn1.ObjectIdentifier{2, 5, 29, 17}, // SAN
				Value: sanValue,
			},
		},
	}
}

func TestExtractEDIPI_FromCACFixture(t *testing.T) {
	cert := loadCertEDIPI(t, "sample_cac_cert.pem")
	got, err := piv.ExtractEDIPI(cert)
	if err != nil {
		t.Fatalf("ExtractEDIPI err: %v", err)
	}
	want := "1234567890"
	if got != want {
		t.Errorf("ExtractEDIPI = %q, want %q", got, want)
	}
}

func TestExtractEDIPI_NonEDIPIUPN(t *testing.T) {
	cert := craftUPNCert(t, "john.doe@example.com")
	got, err := piv.ExtractEDIPI(cert)
	if !errors.Is(err, piv.ErrInvalidEDIPI) {
		t.Fatalf("ExtractEDIPI err = %v, want ErrInvalidEDIPI", err)
	}
	if got != "" {
		t.Errorf("ExtractEDIPI = %q, want empty", got)
	}
}

func TestExtractEDIPI_NoUPN_ReturnsErrNoEDIPI(t *testing.T) {
	cert := loadCertEDIPI(t, "sample_piv_no_upn.pem")
	got, err := piv.ExtractEDIPI(cert)
	if !errors.Is(err, piv.ErrNoEDIPI) {
		t.Fatalf("ExtractEDIPI err = %v, want ErrNoEDIPI", err)
	}
	if got != "" {
		t.Errorf("ExtractEDIPI = %q, want empty", got)
	}
}

func TestExtractEDIPI_NilCert_ReturnsErrNoEDIPI(t *testing.T) {
	got, err := piv.ExtractEDIPI(nil)
	if !errors.Is(err, piv.ErrNoEDIPI) {
		t.Fatalf("ExtractEDIPI(nil) err = %v, want ErrNoEDIPI", err)
	}
	if got != "" {
		t.Errorf("ExtractEDIPI(nil) = %q, want empty", got)
	}
}

func TestExtractEDIPI_TestEDIPIRejectedByDefault(t *testing.T) {
	cert := craftUPNCert(t, "0000000001@mil")
	got, err := piv.ExtractEDIPI(cert)
	if !errors.Is(err, piv.ErrInvalidEDIPI) {
		t.Fatalf("ExtractEDIPI for test EDIPI err = %v, want ErrInvalidEDIPI", err)
	}
	if got != "" {
		t.Errorf("ExtractEDIPI = %q, want empty", got)
	}
}

func TestExtractEDIPI_TestEDIPIAcceptedWhenOverridden(t *testing.T) {
	piv.SetRejectTestEDIPIForTest(t, false)
	cert := craftUPNCert(t, "0000000001@mil")
	got, err := piv.ExtractEDIPI(cert)
	if err != nil {
		t.Fatalf("ExtractEDIPI err = %v, want nil after override", err)
	}
	if got != "0000000001" {
		t.Errorf("ExtractEDIPI = %q, want %q", got, "0000000001")
	}
}

func TestExtractEDIPI_ConcurrentReads(t *testing.T) {
	cert := craftUPNCert(t, "1234567890@mil")
	var wg sync.WaitGroup
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			if _, err := piv.ExtractEDIPI(cert); err != nil {
				t.Errorf("ExtractEDIPI concurrent err: %v", err)
			}
		}()
	}
	wg.Wait()
}
