//go:build ignore

// gen_expired.go generates sample_piv_expired.pem — a leaf cert signed
// by sample_piv_root.pem with NotAfter in 1991 — used by validator_test
// to assert ErrChainExpired. Run from the package directory:
//
//	go run ./testdata/gen_expired.go
//
// Output: testdata/sample_piv_expired.pem + testdata/sample_piv_expired.key.pem
//
// This file is excluded from the normal build by `//go:build ignore`.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

func main() {
	rootCertPEM := mustRead("testdata/sample_piv_root.pem")
	rootKeyPEM := mustRead("testdata/sample_piv_root.key.pem")

	rootBlock, _ := pem.Decode(rootCertPEM)
	rootCert, err := x509.ParseCertificate(rootBlock.Bytes)
	must(err)

	keyBlock, _ := pem.Decode(rootKeyPEM)
	rootKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		// try PKCS#8
		k, err2 := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		must(err2)
		rootKey = k.(*rsa.PrivateKey)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	must(err)

	// Microsoft UPN OID
	oidMicrosoftUPN := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3}

	// Build OtherName SAN value manually so the expired fixture also
	// carries a UPN (mirrors the valid leaf shape; useful for ExtractUPN
	// tests too).
	otherName := struct {
		OID   asn1.ObjectIdentifier
		Value asn1.RawValue `asn1:"explicit,tag:0"`
	}{
		OID: oidMicrosoftUPN,
		Value: asn1.RawValue{
			Class:      asn1.ClassUniversal,
			Tag:        asn1.TagUTF8String,
			IsCompound: false,
			Bytes:      []byte("expired.user@mil"),
		},
	}
	otherNameRaw, err := asn1.Marshal(otherName)
	must(err)

	// Wrap as GeneralName otherName [0] IMPLICIT — strip the outer
	// SEQUENCE tag and prepend a context-specific [0] tag.
	if otherNameRaw[0] != 0x30 {
		panic("expected SEQUENCE")
	}
	// Replace tag 0x30 (SEQUENCE) with 0xA0 (context-specific [0],
	// constructed). length octets follow as-is.
	otherNameRaw[0] = 0xA0

	sanBytes, err := asn1.Marshal([]asn1.RawValue{
		{FullBytes: otherNameRaw},
	})
	must(err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Expired PIV User"},
		NotBefore:    time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		ExtraExtensions: []pkix.Extension{
			{
				Id:    asn1.ObjectIdentifier{2, 5, 29, 17}, // SAN
				Value: sanBytes,
			},
		},
		PolicyIdentifiers: []asn1.ObjectIdentifier{
			{2, 16, 840, 1, 101, 3, 2, 1, 3, 7},
		},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, &leafKey.PublicKey, rootKey)
	must(err)

	mustWrite("testdata/sample_piv_expired.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	mustWrite("testdata/sample_piv_expired.key.pem", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)}))
	println("wrote testdata/sample_piv_expired.pem")
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
