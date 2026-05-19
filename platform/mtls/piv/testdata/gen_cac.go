//go:build ignore

// gen_cac.go generates the DoD-CAC-shaped synthetic fixtures consumed
// by TRD 07-01 tests:
//
//   - sample_cac_root.pem / sample_cac_root.key.pem  — self-signed test
//     trust anchor (regenerated; matches the openssl recipe in README).
//   - sample_cac_cert.pem  — leaf signed by sample_cac_root that carries
//     BOTH a Microsoft UPN OtherName (value "1234567890@mil") AND a
//     FASC-N OtherName (16-byte OCTET STRING value
//     D013CE4F8210D8B612835866E7EE99F4).
//   - dod_truststore_test.pem — concatenation of sample_cac_root.pem +
//     sample_piv_root.pem, used as the input to LoadDoDTrustStore tests.
//
// Why Go instead of openssl: OpenSSL's config-file OtherName syntax
// cannot express a raw OCTET STRING value for FASC-N. Building the
// extension via crypto/x509 + encoding/asn1 is straightforward and
// produces byte-identical, deterministic fixtures.
//
// Run from the package directory:
//
//	go run ./testdata/gen_cac.go
//
// This file is excluded from the normal build by `//go:build ignore`.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

// FASC-N hex value embedded in the fixture. Mirrored verbatim by the
// fascnFixtureHex constant in fascn_test.go.
const fascnHex = "D013CE4F8210D8B612835866E7EE99F4"

// upnValue is the Microsoft UPN OtherName value carried by the leaf.
// "1234567890@mil" is the EDIPI form parsed by ExtractEDIPI.
const upnValue = "1234567890@mil"

var (
	oidSAN          = asn1.ObjectIdentifier{2, 5, 29, 17}
	oidMicrosoftUPN = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3}
	oidFASCN        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 6, 6}
)

func main() {
	// 1. Generate root.
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	must(err)
	rootTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Country:            []string{"US"},
			Organization:       []string{"U.S. Government"},
			OrganizationalUnit: []string{"DoD-TEST"},
			CommonName:         "DoD JITC Test Root CA",
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	must(err)
	rootCert, err := x509.ParseCertificate(rootDER)
	must(err)

	mustWrite("testdata/sample_cac_root.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))
	mustWrite("testdata/sample_cac_root.key.pem", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rootKey)}))

	// 2. Generate leaf with UPN + FASC-N OtherNames.
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	must(err)
	sanBytes, err := buildSAN()
	must(err)
	leafTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "DOE.JOHN.A.1234567890"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		ExtraExtensions: []pkix.Extension{
			{Id: oidSAN, Value: sanBytes},
		},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, rootCert, &leafKey.PublicKey, rootKey)
	must(err)
	mustWrite("testdata/sample_cac_cert.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}))
	mustWrite("testdata/sample_cac_cert.key.pem", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}))

	// 3. Build dod_truststore_test.pem (sample_cac_root + sample_piv_root).
	cacRootPEM := mustRead("testdata/sample_cac_root.pem")
	pivRootPEM := mustRead("testdata/sample_piv_root.pem")
	bundle := append(cacRootPEM, pivRootPEM...)
	mustWrite("testdata/dod_truststore_test.pem", bundle)

	println("wrote testdata/sample_cac_root.pem")
	println("wrote testdata/sample_cac_cert.pem")
	println("wrote testdata/dod_truststore_test.pem")
}

// buildSAN constructs a SubjectAltName extension value (SEQUENCE OF
// GeneralName) containing two OtherName entries: the Microsoft UPN
// (value "1234567890@mil") and the FASC-N (raw 16-byte OCTET STRING
// from fascnHex). The order matches a real DoD CAC where the UPN
// appears first.
func buildSAN() ([]byte, error) {
	upnGN, err := buildOtherNameUTF8(oidMicrosoftUPN, upnValue)
	if err != nil {
		return nil, err
	}
	fascnRaw, err := hex.DecodeString(fascnHex)
	if err != nil {
		return nil, err
	}
	fascnGN, err := buildOtherNameOctet(oidFASCN, fascnRaw)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal([]asn1.RawValue{
		{FullBytes: upnGN},
		{FullBytes: fascnGN},
	})
}

// buildOtherNameUTF8 builds a GeneralName otherName [0] IMPLICIT whose
// inner ANY-DEFINED-BY-OID is a UTF8String. The `explicit,tag:0,utf8`
// struct tag drives Go's encoding/asn1 to emit the proper [0] EXPLICIT
// wrapper around the UTF8String — required by RFC 5280's OtherName
// definition. (Using asn1.RawValue with an explicit tag does NOT add
// the wrapper; that's a documented Go quirk.)
func buildOtherNameUTF8(oid asn1.ObjectIdentifier, value string) ([]byte, error) {
	other := struct {
		OID   asn1.ObjectIdentifier
		Value string `asn1:"explicit,tag:0,utf8"`
	}{
		OID:   oid,
		Value: value,
	}
	raw, err := asn1.Marshal(other)
	if err != nil {
		return nil, err
	}
	if raw[0] != 0x30 {
		panic("expected SEQUENCE tag for OtherName")
	}
	raw[0] = 0xA0 // [0] IMPLICIT GeneralName
	return raw, nil
}

// buildOtherNameOctet builds a GeneralName otherName [0] IMPLICIT whose
// inner ANY-DEFINED-BY-OID is an OCTET STRING wrapping value bytes.
// Go's encoding/asn1 has no struct-tag idiom for "OCTET STRING with
// EXPLICIT [0] wrapper", so we hand-assemble the [0] EXPLICIT wrap
// around an OCTET STRING TLV using asn1.RawValue. The resulting bytes
// are byte-identical to what openssl emits for the same SAN entry.
func buildOtherNameOctet(oid asn1.ObjectIdentifier, value []byte) ([]byte, error) {
	// 1. Emit the inner OCTET STRING TLV.
	octetTLV, err := asn1.Marshal(value) // []byte → OCTET STRING TLV
	if err != nil {
		return nil, err
	}
	// 2. Wrap it in [0] EXPLICIT (context-specific, constructed,
	// tag 0). asn1.RawValue carrying constructed bytes round-trips
	// through Marshal cleanly.
	explicitWrap, err := asn1.Marshal(asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        0,
		IsCompound: true,
		Bytes:      octetTLV,
	})
	if err != nil {
		return nil, err
	}
	// 3. Wrap (OID, EXPLICIT[0]) in a SEQUENCE.
	other := struct {
		OID     asn1.ObjectIdentifier
		Wrapper asn1.RawValue
	}{OID: oid}
	if _, err := asn1.Unmarshal(explicitWrap, &other.Wrapper); err != nil {
		return nil, err
	}
	raw, err := asn1.Marshal(other)
	if err != nil {
		return nil, err
	}
	if raw[0] != 0x30 {
		panic("expected SEQUENCE tag for OtherName")
	}
	raw[0] = 0xA0 // [0] IMPLICIT GeneralName
	return raw, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	must(err)
	return b
}

func mustWrite(p string, b []byte) {
	must(os.WriteFile(p, b, 0o600))
}
