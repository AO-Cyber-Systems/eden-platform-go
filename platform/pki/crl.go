package pki

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"math/big"
	"time"
)

// RevokedCert is the input shape for BuildCRL. Each entry corresponds to one
// revoked certificate.
//
// RFC 5280 §5.3.1 reason codes:
//
//	0  unspecified
//	1  keyCompromise
//	2  cACompromise
//	3  affiliationChanged
//	4  superseded
//	5  cessationOfOperation
//	6  certificateHold
//	8  removeFromCRL
//	9  privilegeWithdrawn
//	10 aACompromise
//
// (Code 7 is reserved/unused per the RFC.) Callers should validate Reason in
// their layer if they need to reject unsupported codes — this package
// trusts the input.
type RevokedCert struct {
	SerialNumber *big.Int
	RevokedAt    time.Time
	Reason       int
}

// BuildCRL produces a DER-encoded CRL signed by `signer` (typically the
// intermediate CA's signer obtained via CA.Signer()). The CRL is parented to
// `intermediate` so the resulting CRL's Issuer field matches the cert that
// originally signed the revoked leaves.
//
// `crlNumber` is the monotonically increasing CRL sequence number; callers
// store it in their database and pass the next value on each call (RFC 5280
// §5.2.3). Skipping values is permitted; reusing values is NOT.
//
// `thisUpdate` is the issue time of this CRL; `nextUpdate` is the time at
// which a newer CRL is expected to be available — clients refuse to use a
// CRL beyond nextUpdate.
func BuildCRL(
	intermediate *x509.Certificate,
	signer crypto.Signer,
	revoked []RevokedCert,
	crlNumber *big.Int,
	thisUpdate, nextUpdate time.Time,
) ([]byte, error) {
	if intermediate == nil {
		return nil, fmt.Errorf("pki: BuildCRL: nil intermediate")
	}
	if signer == nil {
		return nil, fmt.Errorf("pki: BuildCRL: nil signer")
	}
	if crlNumber == nil {
		return nil, fmt.Errorf("pki: BuildCRL: nil crlNumber")
	}
	entries := make([]x509.RevocationListEntry, 0, len(revoked))
	for _, r := range revoked {
		entries = append(entries, x509.RevocationListEntry{
			SerialNumber:   r.SerialNumber,
			RevocationTime: r.RevokedAt,
			ReasonCode:     r.Reason,
		})
	}
	tmpl := &x509.RevocationList{
		SignatureAlgorithm:        signatureAlgorithmFor(signer),
		Number:                    crlNumber,
		ThisUpdate:                thisUpdate,
		NextUpdate:                nextUpdate,
		RevokedCertificateEntries: entries,
	}
	der, err := x509.CreateRevocationList(rand.Reader, tmpl, intermediate, signer)
	if err != nil {
		return nil, fmt.Errorf("pki: BuildCRL: %w", err)
	}
	return der, nil
}
