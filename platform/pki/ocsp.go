package pki

import (
	"context"
	"crypto"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/ocsp"
)

// nonceOID is the OCSP nonce extension OID per RFC 6960 §4.4.1
// (id-pkix-ocsp-nonce = 1.3.6.1.5.5.7.48.1.2). When a request carries this
// extension, the responder MUST echo it in the response.
var nonceOID = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2}

// OCSPStore is the lookup interface that a deployment provides for the
// responder. Returning (nil, nil) means "no revocation record" — the
// responder will reply Good. Returning a non-nil RevokedCert produces a
// Revoked response with the embedded RevokedAt + Reason.
//
// Implementations should be safe for concurrent use.
type OCSPStore interface {
	GetRevocation(ctx context.Context, serial *big.Int) (*RevokedCert, error)
}

// OCSPResponder serves OCSP requests for certificates issued by `issuer`,
// signing responses with `signer` (typically CA.Signer() — the same key
// that issued the leaves). `store` looks up revocation state. Responses set
// NextUpdate = now + nextUpdateAfter.
//
// Zero value is not usable; construct via NewOCSPResponder.
type OCSPResponder struct {
	issuer          *x509.Certificate
	signer          crypto.Signer
	store           OCSPStore
	nextUpdateAfter time.Duration
}

// NewOCSPResponder constructs a responder. nextUpdateAfter controls the
// freshness window the responder advertises — clients refuse responses
// older than their cached copy's NextUpdate.
func NewOCSPResponder(
	issuer *x509.Certificate,
	signer crypto.Signer,
	store OCSPStore,
	nextUpdateAfter time.Duration,
) *OCSPResponder {
	return &OCSPResponder{
		issuer:          issuer,
		signer:          signer,
		store:           store,
		nextUpdateAfter: nextUpdateAfter,
	}
}

// RespondToRequest parses a DER-encoded OCSP request, looks up revocation
// state for the requested serial, and returns a signed DER response plus the
// HTTP Content-Type ("application/ocsp-response" per RFC 6960 §A.2).
//
// Behavior:
//   - Unknown serial (store returns nil) → ocsp.Good (RFC 6960 §4.2.2.2
//     explicitly permits this for authoritative responders that know the
//     full universe of issued certs).
//   - Known revoked serial → ocsp.Revoked with RevokedAt + RevocationReason.
//   - Request carries an id-pkix-ocsp-nonce extension → response echoes the
//     same extension (RFC 6960 §4.4.1).
//   - Malformed request → error.
//   - Store error → error.
//
// Callers (typically an HTTP handler) translate parse errors into HTTP 400
// and store errors into HTTP 500.
func (r *OCSPResponder) RespondToRequest(ctx context.Context, reqDER []byte) ([]byte, string, error) {
	req, err := ocsp.ParseRequest(reqDER)
	if err != nil {
		// Don't leak parser internals — caller will translate to HTTP 400.
		return nil, "", fmt.Errorf("pki: invalid ocsp request")
	}
	if req.SerialNumber == nil {
		return nil, "", errors.New("pki: ocsp request missing serial")
	}

	rev, err := r.store.GetRevocation(ctx, req.SerialNumber)
	if err != nil {
		return nil, "", fmt.Errorf("pki: ocsp store lookup: %w", err)
	}

	now := time.Now().UTC()
	tmpl := ocsp.Response{
		SerialNumber: req.SerialNumber,
		ThisUpdate:   now,
		NextUpdate:   now.Add(r.nextUpdateAfter),
		// NOTE: tmpl.Certificate is intentionally NOT set. We sign with
		// the same key that issued the leaf (the intermediate CA), so the
		// responder-identity-cert is the issuer itself; embedding it
		// would cause ocsp.ParseResponse to verify "issuer signed
		// embedded cert", which fails because the intermediate was
		// signed by the root, not by itself. Callers that need a
		// distinct delegated responder cert (RFC 6960 §4.2.2.2.1) should
		// extend this package.
	}
	if rev == nil {
		tmpl.Status = ocsp.Good
	} else {
		tmpl.Status = ocsp.Revoked
		tmpl.RevokedAt = rev.RevokedAt
		tmpl.RevocationReason = rev.Reason
	}

	// Nonce echo (RFC 6960 §4.4.1). ocsp.Request from golang.org/x/crypto
	// does NOT expose raw extensions, so we parse the OCSP request's
	// tbsRequest ASN.1 directly to pull any id-pkix-ocsp-nonce extension
	// out and echo it into the response's ExtraExtensions.
	if ext, ok := extractNonceExtension(reqDER); ok {
		tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, ext)
	}

	respDER, err := ocsp.CreateResponse(r.issuer, r.issuer, tmpl, r.signer)
	if err != nil {
		return nil, "", fmt.Errorf("pki: ocsp create response: %w", err)
	}
	return respDER, "application/ocsp-response", nil
}

// ocspRequestEnvelope mirrors the minimum ASN.1 surface of RFC 6960 §4.1.1
// OCSPRequest needed to pull out the optional requestExtensions block.
// Field types use asn1.RawValue so the asn1 package only validates the
// outer tags — we don't need to model the entire CertID/Request hierarchy.
type ocspRequestEnvelope struct {
	TBSRequest ocspTBSRequestEnvelope
	// Signature [0] EXPLICIT optional — ignored.
	OptionalSignature asn1.RawValue `asn1:"optional,explicit,tag:0"`
}

type ocspTBSRequestEnvelope struct {
	Version           int             `asn1:"optional,explicit,tag:0,default:0"`
	RequestorName     asn1.RawValue   `asn1:"optional,explicit,tag:1"`
	RequestList       []asn1.RawValue `asn1:"sequence"`
	RequestExtensions []pkix.Extension `asn1:"optional,explicit,tag:2"`
}

// extractNonceExtension returns the id-pkix-ocsp-nonce extension from a DER
// OCSP request, if present. The returned extension's Value is the raw OCTET
// STRING (matching how ocsp.CreateResponse marshals ExtraExtensions back into
// the response's responseExtensions).
func extractNonceExtension(reqDER []byte) (pkix.Extension, bool) {
	var env ocspRequestEnvelope
	if _, err := asn1.Unmarshal(reqDER, &env); err != nil {
		return pkix.Extension{}, false
	}
	for _, ext := range env.TBSRequest.RequestExtensions {
		if ext.Id.Equal(nonceOID) {
			return ext, true
		}
	}
	return pkix.Extension{}, false
}
