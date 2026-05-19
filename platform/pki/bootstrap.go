package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ErrNonCARoot is returned by BootstrapIntermediate when the supplied
// "root" certificate does not have IsCA=true. Refusing here prevents
// accidentally creating an intermediate that won't chain-validate.
var ErrNonCARoot = errors.New("pki: parent cert is not a CA (IsCA=false)")

// backdate is how far into the past NotBefore is set, to absorb clock skew
// between issuance and use. RFC 5280 §4.1.2.5 permits any range; 5 minutes
// is a common industry default.
const backdate = 5 * time.Minute

// randomSerial returns a fresh 159-bit cryptographically random serial number
// suitable for use as an X.509 serial per RFC 5280 §4.1.2.2:
//
//	"Certificate users MUST be able to handle serialNumber values up to 20
//	 octets. Conforming CAs MUST NOT use serialNumber values longer than 20
//	 octets."
//
// 159 bits is large enough to make collisions cosmically unlikely while
// staying under the 20-octet (160-bit) ceiling.
func randomSerial() (*big.Int, error) {
	upper := new(big.Int).Lsh(big.NewInt(1), 159)
	n, err := rand.Int(rand.Reader, upper)
	if err != nil {
		return nil, fmt.Errorf("pki: generate serial: %w", err)
	}
	return n, nil
}

// signatureAlgorithmFor picks the JWS-compatible SignatureAlgorithm based on
// the signer's public-key shape. Returning x509.UnknownSignatureAlgorithm
// lets stdlib pick a default; we want to be explicit so the cert encodes a
// stable SignatureAlgorithm OID that consumers (and tests) can assert on.
func signatureAlgorithmFor(signer crypto.Signer) x509.SignatureAlgorithm {
	switch signer.Public().(type) {
	case *ecdsa.PublicKey:
		return x509.ECDSAWithSHA256
	case *rsa.PublicKey:
		return x509.SHA256WithRSA
	default:
		return x509.UnknownSignatureAlgorithm
	}
}

// BootstrapRoot creates a self-signed root CA certificate. The signer's
// public key is bound into the cert; the signer's private key signs it.
// The returned cert is suitable as a trust anchor; the DER is also returned
// for callers that want to persist or transmit it.
//
// Template fields applied:
//   - SerialNumber: 159-bit random per RFC 5280 §4.1.2.2
//   - NotBefore: now - 5 minutes (clock-skew backdate)
//   - NotAfter:  now + validity
//   - KeyUsage:  CertSign | CRLSign (required for a CA)
//   - IsCA + BasicConstraintsValid + MaxPathLen=1 (root may sign one tier of intermediate)
//   - SignatureAlgorithm: ECDSAWithSHA256 (EC signer) or SHA256WithRSA (RSA signer)
func BootstrapRoot(signer crypto.Signer, subject pkix.Name, validity time.Duration) (*x509.Certificate, []byte, error) {
	if signer == nil {
		return nil, nil, errors.New("pki: BootstrapRoot: nil signer")
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject,
		Issuer:                subject, // self-signed
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,     // root may sign ONE level of intermediate
		MaxPathLenZero:        false, // explicit: not zero
		SignatureAlgorithm:    signatureAlgorithmFor(signer),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, signer.Public(), signer)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: BootstrapRoot: create cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: BootstrapRoot: parse: %w", err)
	}
	return cert, der, nil
}

// BootstrapIntermediate creates an intermediate CA certificate signed by
// `rootSigner` (the root's private key) and chained to `rootCert` (the
// root's parsed cert — used as the parent for x509.CreateCertificate so the
// resulting cert's Issuer field matches the root's Subject).
//
// The intermediate's public key is `intermediatePub`; its private counterpart
// is held by a DIFFERENT signer (the intermediate signer) that the caller
// will use for leaf issuance + CRL signing.
//
// Constraints applied:
//   - MaxPathLenZero=true so the intermediate cannot sign further CA
//     certs — prevents privilege-escalation if the intermediate key is
//     compromised.
//   - KeyUsage = CertSign | CRLSign (intermediate signs leaves + CRLs).
//
// Returns ErrNonCARoot if rootCert.IsCA is false.
func BootstrapIntermediate(
	rootSigner crypto.Signer,
	rootCert *x509.Certificate,
	intermediatePub crypto.PublicKey,
	subject pkix.Name,
	validity time.Duration,
) (*x509.Certificate, []byte, error) {
	if rootSigner == nil {
		return nil, nil, errors.New("pki: BootstrapIntermediate: nil rootSigner")
	}
	if rootCert == nil {
		return nil, nil, errors.New("pki: BootstrapIntermediate: nil rootCert")
	}
	if !rootCert.IsCA {
		return nil, nil, ErrNonCARoot
	}
	if intermediatePub == nil {
		return nil, nil, errors.New("pki: BootstrapIntermediate: nil intermediatePub")
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject,
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true, // intermediate cannot sign further CAs
		SignatureAlgorithm:    signatureAlgorithmFor(rootSigner),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, intermediatePub, rootSigner)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: BootstrapIntermediate: create cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: BootstrapIntermediate: parse: %w", err)
	}
	return cert, der, nil
}
