package piv_test

// Test list:
//   - TestExtractFASCN_FromCACFixture — load testdata/sample_cac_cert.pem;
//     ExtractFASC_N returns 16 bytes whose hex matches the literal value
//     written into the fixture by the openssl recipe.
//   - TestExtractFASCN_NoSAN_ReturnsErrNoFASCN — cert with no SAN ext at
//     all; assert ErrNoFASCN.
//   - TestExtractFASCN_UPNOnly_ReturnsErrNoFASCN — load
//     testdata/sample_piv_cert.pem (UPN OtherName only, no FASC-N);
//     assert ErrNoFASCN.
//   - TestExtractFASCN_MultiOtherName_FindsFASCN — synthetic cert with
//     BOTH a UPN OtherName (first) AND a FASC-N OtherName (second);
//     assert the parser walks past the UPN entry and returns the
//     FASC-N bytes.
//   - TestExtractFASCN_NilCert_ReturnsErrNoFASCN — nil input → ErrNoFASCN.
//   - TestFormatFASCNHex_Roundtrip — FormatFASC_NHex emits lowercase hex
//     with no separator; hex.DecodeString roundtrips back to the input.

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/mtls/piv"
)

// fascnFixtureHex is the literal hex string written into the FASC-N
// OtherName OCTET STRING by testdata/README.md's openssl recipe. The
// fixture happy-path test compares ExtractFASC_N's bytes to this value.
const fascnFixtureHex = "d013ce4f8210d8b612835866e7ee99f4"

// craftMultiOtherNameCert returns a stub *x509.Certificate whose SAN
// extension carries TWO OtherName entries in order:
//
//  1. Microsoft UPN OtherName with the given UPN UTF8String value.
//  2. FASC-N OtherName with the supplied raw bytes wrapped in an OCTET
//     STRING.
//
// Used to drive the ExtractFASC_N "walk past the UPN entry" branch
// without regenerating openssl fixtures.
func craftMultiOtherNameCert(t *testing.T, upn string, fascn []byte) *x509.Certificate {
	t.Helper()

	// Build the UPN OtherName as a [0] IMPLICIT GeneralName.
	upnValue, err := asn1.Marshal(upn)
	if err != nil {
		t.Fatalf("marshal upn utf8: %v", err)
	}
	upnExplicit, err := asn1.Marshal(asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        0,
		IsCompound: true,
		Bytes:      upnValue,
	})
	if err != nil {
		t.Fatalf("marshal upn [0] explicit: %v", err)
	}
	upnContent := struct {
		OID     asn1.ObjectIdentifier
		Wrapper asn1.RawValue
	}{OID: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3}}
	if _, err := asn1.Unmarshal(upnExplicit, &upnContent.Wrapper); err != nil {
		t.Fatalf("unmarshal upn wrapper: %v", err)
	}
	upnSeq, err := asn1.Marshal(upnContent)
	if err != nil {
		t.Fatalf("marshal upn OtherName seq: %v", err)
	}
	upnGN := make([]byte, len(upnSeq))
	copy(upnGN, upnSeq)
	upnGN[0] = 0xA0 // IMPLICIT [0] GeneralName

	// Build the FASC-N OtherName as a [0] IMPLICIT GeneralName whose
	// inner ANY is an OCTET STRING.
	fascnOctet, err := asn1.Marshal(fascn)
	if err != nil {
		t.Fatalf("marshal fascn octet string: %v", err)
	}
	fascnExplicit, err := asn1.Marshal(asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        0,
		IsCompound: true,
		Bytes:      fascnOctet,
	})
	if err != nil {
		t.Fatalf("marshal fascn [0] explicit: %v", err)
	}
	fascnContent := struct {
		OID     asn1.ObjectIdentifier
		Wrapper asn1.RawValue
	}{OID: asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 6, 6}}
	if _, err := asn1.Unmarshal(fascnExplicit, &fascnContent.Wrapper); err != nil {
		t.Fatalf("unmarshal fascn wrapper: %v", err)
	}
	fascnSeq, err := asn1.Marshal(fascnContent)
	if err != nil {
		t.Fatalf("marshal fascn OtherName seq: %v", err)
	}
	fascnGN := make([]byte, len(fascnSeq))
	copy(fascnGN, fascnSeq)
	fascnGN[0] = 0xA0

	// SAN value is SEQUENCE OF GeneralName containing both entries.
	sanValue, err := asn1.Marshal([]asn1.RawValue{
		{FullBytes: upnGN},
		{FullBytes: fascnGN},
	})
	if err != nil {
		t.Fatalf("marshal SAN sequence: %v", err)
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

func TestExtractFASCN_FromCACFixture(t *testing.T) {
	cert := loadCertEDIPI(t, "sample_cac_cert.pem")
	raw, err := piv.ExtractFASC_N(cert)
	if err != nil {
		t.Fatalf("ExtractFASC_N err: %v", err)
	}
	got := hex.EncodeToString(raw)
	if got != fascnFixtureHex {
		t.Errorf("ExtractFASC_N hex = %q, want %q (len(raw)=%d)", got, fascnFixtureHex, len(raw))
	}
}

func TestExtractFASCN_NoSAN_ReturnsErrNoFASCN(t *testing.T) {
	cert := &x509.Certificate{}
	raw, err := piv.ExtractFASC_N(cert)
	if !errors.Is(err, piv.ErrNoFASCN) {
		t.Fatalf("ExtractFASC_N err = %v, want ErrNoFASCN", err)
	}
	if raw != nil {
		t.Errorf("ExtractFASC_N raw = %x, want nil", raw)
	}
}

func TestExtractFASCN_UPNOnly_ReturnsErrNoFASCN(t *testing.T) {
	cert := loadCertEDIPI(t, "sample_piv_cert.pem")
	raw, err := piv.ExtractFASC_N(cert)
	if !errors.Is(err, piv.ErrNoFASCN) {
		t.Fatalf("ExtractFASC_N err = %v, want ErrNoFASCN", err)
	}
	if raw != nil {
		t.Errorf("ExtractFASC_N raw = %x, want nil", raw)
	}
}

func TestExtractFASCN_MultiOtherName_FindsFASCN(t *testing.T) {
	want := []byte{0xD0, 0x13, 0xCE, 0x4F, 0x82, 0x10, 0xD8, 0xB6, 0x12, 0x83, 0x58, 0x66, 0xE7, 0xEE, 0x99, 0xF4}
	cert := craftMultiOtherNameCert(t, "1234567890@mil", want)
	got, err := piv.ExtractFASC_N(cert)
	if err != nil {
		t.Fatalf("ExtractFASC_N err: %v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("ExtractFASC_N = %x, want %x", got, want)
	}
}

func TestExtractFASCN_NilCert_ReturnsErrNoFASCN(t *testing.T) {
	raw, err := piv.ExtractFASC_N(nil)
	if !errors.Is(err, piv.ErrNoFASCN) {
		t.Fatalf("ExtractFASC_N(nil) err = %v, want ErrNoFASCN", err)
	}
	if raw != nil {
		t.Errorf("ExtractFASC_N(nil) raw = %x, want nil", raw)
	}
}

func TestFormatFASCNHex_Roundtrip(t *testing.T) {
	want := []byte{0xD0, 0x13, 0xCE, 0x4F, 0x82, 0x10, 0xD8, 0xB6, 0x12, 0x83, 0x58, 0x66, 0xE7, 0xEE, 0x99, 0xF4}
	hexStr := piv.FormatFASC_NHex(want)
	if hexStr != "d013ce4f8210d8b612835866e7ee99f4" {
		t.Errorf("FormatFASC_NHex = %q, want lowercase no-separator hex", hexStr)
	}
	got, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("hex.DecodeString err: %v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("roundtrip mismatch: got %x, want %x", got, want)
	}
}
