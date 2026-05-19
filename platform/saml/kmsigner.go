package saml

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
)

// ErrUnsupportedHash is returned by KMSSigner.Sign when the caller's
// crypto.SignerOpts.HashFunc() is not one of crypto.SHA256, crypto.SHA384,
// or crypto.SHA512. SAML signers under AOID's policy do NOT permit SHA-1
// (forbidden by FIPS 186-5 and most modern verifiers) and do not permit
// SHA-224 (no production-grade SAML library accepts it).
var ErrUnsupportedHash = errors.New("saml/kmsigner: unsupported hash function (want SHA-256/384/512)")

// Backend is the minimum surface a KMS-resident signer must expose for
// KMSSigner to drive it. It is a strict subset of platform/kms.KMSSigner so
// fakes and the production KMS implementation can satisfy it.
//
// All Backend implementations MUST be safe for concurrent use.
type Backend interface {
	// Public returns the public key associated with this Backend's
	// private signing material. For RSA keys this is an *rsa.PublicKey;
	// for ECDSA P-256 keys this is an *ecdsa.PublicKey.
	Public() crypto.PublicKey

	// Sign signs digest with the underlying key. For RSA backends, the
	// signature is in PKCS#1 v1.5 form; for ECDSA backends, it is the
	// ASN.1-DER encoding of the (r, s) pair (the wire format every cloud
	// KMS — AWS, Azure, GCP — produces).
	//
	// Sign MUST honour the crypto.Signer contract: digest is pre-hashed,
	// rand may be nil (the backend should consult its own RNG), and opts
	// MUST be non-nil with opts.HashFunc() matching the digest length.
	Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error)
}

// KMSSigner adapts a platform/kms.KMSSigner-shaped Backend to the standard
// library's crypto.Signer interface so that goxmldsig's SigningContext can
// sign SAML assertions/requests using HSM- or KMS-resident keys.
//
// The wrapper does not cache any signing material; the underlying Backend
// is invoked once per Sign call. Verification of the produced signatures
// uses the standard library (rsa.VerifyPKCS1v15 / ecdsa.Verify) and never
// round-trips back to the KMS — KMSSigner.Public() returns the public key
// extracted from Cert, which callers cache.
//
// # Threading
//
// KMSSigner values are immutable after construction. The underlying
// Backend MUST be safe for concurrent use (every platform/kms provider
// already satisfies this).
type KMSSigner struct {
	// Signer performs the underlying signing operation. In production
	// this is a platform/kms.KMSSigner (which embeds crypto.Signer); in
	// tests it can be any Backend-shaped fake.
	Signer Backend
	// KeyID is the stable, log-safe identifier of the signing key (AWS
	// ARN, Azure key URL, PKCS#11 CKA_LABEL). Callers attach this to the
	// JWS `kid` header or the SAML KeyInfo / KeyName element.
	KeyID string
	// Cert is the X.509 certificate carrying the signing public key. It
	// is the source of truth for Public() and the only certificate
	// embedded in the SAML KeyInfo block (one-cert chain). Production
	// callers wishing to embed an intermediate chain should compose at
	// the SigningContext level, not here.
	Cert *x509.Certificate
}

// Public returns Cert.PublicKey, or nil if Cert is nil. crypto.Signer's
// contract does not require a non-nil return for misconfigured signers, so
// callers MUST treat a nil return as a configuration error and abort.
func (s *KMSSigner) Public() crypto.PublicKey {
	if s == nil || s.Cert == nil {
		return nil
	}
	return s.Cert.PublicKey
}

// Sign delegates to the underlying Backend after validating that the
// requested hash function is one of the SAML-approved set
// (SHA-256/384/512). The digest must already be the output of the
// requested hash function (per the crypto.Signer contract).
//
// rand may be nil; the Backend will use its own randomness source (cloud
// KMSes always do).
func (s *KMSSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if s == nil || s.Signer == nil {
		return nil, errors.New("saml/kmsigner: nil KMSSigner or backend")
	}
	if opts == nil {
		return nil, errors.New("saml/kmsigner: nil SignerOpts")
	}
	switch opts.HashFunc() {
	case crypto.SHA256, crypto.SHA384, crypto.SHA512:
		// allowed
	default:
		return nil, fmt.Errorf("%w: got %v", ErrUnsupportedHash, opts.HashFunc())
	}
	sig, err := s.Signer.Sign(rand, digest, opts)
	if err != nil {
		return nil, fmt.Errorf("saml/kmsigner: backend Sign (keyID=%s): %w", s.KeyID, err)
	}
	return sig, nil
}
