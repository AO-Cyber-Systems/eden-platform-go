package spiffe

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"time"
)

// TTL constants for SPIFFE X.509-SVIDs.
//
// The spec recommends short-lived credentials — these constants give
// callers a sensible default and an enforced ceiling. The signing CA
// (platform/pki.CA) clamps caller-requested TTLs to MaxSVIDTTL.
const (
	// MaxSVIDTTL is the maximum acceptable lifetime for a leaf SVID.
	// SPIFFE recommends "short-lived" without naming a number — 24h is
	// the operational ceiling AOID uses for workload identities.
	MaxSVIDTTL = 24 * time.Hour

	// DefaultSVIDTTL is the lifetime SVID-issuing services pick when the
	// caller does not specify one.
	DefaultSVIDTTL = 1 * time.Hour
)

// LeafTemplateSVID returns a fresh *x509.Certificate template shaped per
// the SPIFFE X.509-SVID specification §3.
//
// The returned template intentionally omits the SerialNumber — the
// signing CA (platform/pki.CA) assigns serials at issuance time. The
// template also omits subject- and issuer-key-id (the CA derives both
// from the CSR public key and the CA cert respectively).
//
// Spec invariants enforced here:
//
//   - Subject: empty pkix.Name. The X.509-SVID spec mandates empty Subject;
//     leaf identity is communicated exclusively via the URI SAN.
//   - URIs: exactly one entry, the SPIFFE ID URL.
//   - DNSNames / EmailAddresses / IPAddresses: nil. URI-SAN-only.
//   - KeyUsage: DigitalSignature. NOT KeyEncipherment (the SPIFFE spec
//     discourages it because ECDSA keys cannot encipher and most SVIDs are
//     ECDSA P-256).
//   - ExtKeyUsage: ClientAuth + ServerAuth — workload identity is
//     bidirectional (both mTLS client and server roles).
//   - IsCA: false, BasicConstraintsValid: true (serializes CA:FALSE).
//
// Each call returns a freshly-allocated *x509.Certificate AND a freshly-
// allocated *url.URL inside URIs[0], so concurrent callers may freely
// mutate the returned template without aliasing.
func LeafTemplateSVID(id SPIFFEID, notBefore, notAfter time.Time) *x509.Certificate {
	// Fresh *url.URL per call so callers mutating template.URIs[0]
	// (or otherwise re-using the SPIFFEID) cannot pollute one another.
	uri := &url.URL{
		Scheme: "spiffe",
		Host:   string(id.TrustDomain),
		Path:   id.Path,
	}
	return &x509.Certificate{
		// SerialNumber intentionally nil — caller (pki.CA) assigns.
		Subject:               pkix.Name{},
		URIs:                  []*url.URL{uri},
		DNSNames:              nil,
		EmailAddresses:        nil,
		IPAddresses:           nil,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:                  false,
		BasicConstraintsValid: true,
	}
}
