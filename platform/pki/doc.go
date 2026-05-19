// Package pki is a small, dependency-free internal CA primitive built on
// stdlib crypto/x509 + golang.org/x/crypto/ocsp.
//
// # What this package provides
//
// Bootstrap (root + intermediate self/co-sign), leaf issuance from CSR or
// public key, CRL building (RFC 5280), and an OCSP responder (RFC 6960). All
// signing routes through a crypto.Signer interface, so the CA private key
// material never leaves an HSM-backed signer (eden's platform/kms.KMSSigner
// satisfies crypto.Signer).
//
// # Security model
//
//   - The root CA private key signs ONLY the intermediate. After bootstrap
//     it should stay offline (or behind an HSM with a Sign-permission policy
//     limited to a single short-lived bootstrap window).
//   - The intermediate CA signs leaves + CRLs + OCSP responses. Its private
//     key is held by an HSM-backed KMSSigner in production.
//   - Leaf templates must declare explicit NotBefore + NotAfter; this
//     package does NOT default them (a zero time.Time produces a cert valid
//     forever, which is wrong).
//   - CSR signatures MUST be verified before reading the requester's public
//     key — IssueLeafFromCSR enforces this via csr.CheckSignature().
//   - Serials are 159-bit cryptographically random per RFC 5280 §4.1.2.2.
//
// # What this package does NOT do
//
//   - Persist the CRL number — callers track it in their own database and
//     pass it in on each call to BuildCRL.
//   - Implement the OCSPStore lookup — callers provide an implementation of
//     the OCSPStore interface backed by their revocation database.
//   - Schedule CRL refresh — that's a per-deployment policy decision.
//   - Implement OCSP request signing (clients almost never sign OCSP
//     requests; if you need it, parse the OCSP request with
//     golang.org/x/crypto/ocsp directly and verify the signature yourself).
//
// # Usage
//
// Bootstrap a root + intermediate at deployment time (one-time op):
//
//	rootSigner := kms.NewSigner(...)          // HSM-backed
//	rootCert, _, err := pki.BootstrapRoot(rootSigner, pkix.Name{CommonName: "Acme Root CA"}, 20*365*24*time.Hour)
//	if err != nil { return err }
//
//	interSigner := kms.NewSigner(...)          // HSM-backed, distinct key
//	interCert, _, err := pki.BootstrapIntermediate(rootSigner, rootCert, interSigner.Public(), pkix.Name{CommonName: "Acme Issuing CA"}, 10*365*24*time.Hour)
//	if err != nil { return err }
//
// Issue a leaf from a CSR (mTLS client cert flow):
//
//	ca := pki.NewCA(interCert, interSigner)
//	template := &x509.Certificate{
//	    NotBefore:   time.Now().Add(-1 * time.Minute),
//	    NotAfter:    time.Now().Add(90 * 24 * time.Hour),
//	    KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
//	    ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
//	}
//	leafDER, leafCert, err := ca.IssueLeafFromCSR(ctx, csrDER, template)
//
// Build a CRL (called periodically by AOID's revocation publisher):
//
//	crlDER, err := pki.BuildCRL(interCert, interSigner, revoked, crlNumber, thisUpdate, nextUpdate)
//
// Respond to OCSP requests:
//
//	resp := pki.NewOCSPResponder(interCert, interSigner, ocspStore, 10*time.Minute)
//	respDER, contentType, err := resp.RespondToRequest(ctx, reqDER)
package pki
