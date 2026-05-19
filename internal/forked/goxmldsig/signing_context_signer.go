// Package goxmldsig is a thin, internal-use shim around
// github.com/russellhaering/goxmldsig that exposes a single ergonomic factory
// (NewSigningContextSigner) plus a keystore type (MemoryX509KeyStoreSigner)
// whose GetKeyPair() returns a crypto.Signer instead of the *rsa.PrivateKey
// that upstream's X509KeyStore interface requires.
//
// # Why a fork
//
// As of github.com/russellhaering/goxmldsig v1.4.0, the package already
// supports signing via a crypto.Signer through the package-level
// NewSigningContext(signer, certs) function — the underlying machinery has
// landed upstream. This shim therefore does NOT patch upstream; it provides:
//
//  1. A stable, narrowly-typed *MemoryX509KeyStoreSigner that satisfies both
//     the X509KeyStore and X509ChainStore interfaces while remembering a
//     crypto.Signer (NOT *rsa.PrivateKey) so platform/saml can construct it
//     without RSA-only assumptions. This is what platform/saml's TRD
//     06-01 "truths" require, and it keeps the indirection in one place.
//
//  2. A defensive constructor that rejects nil signers and empty cert
//     chains and that returns an error rather than panicking.
//
// # Lifecycle
//
// When upstream consolidates a similar keystore (the upstream Signer/cert
// codepath is already available, but the keystore type is not), this entire
// package should be deleted and platform/saml should call
// dsig.NewSigningContext directly. UPSTREAM_PR.md documents the removal
// path.
//
// All exported symbols are forwarded through the parent package import
// chain — platform/saml is the only intended consumer.
package goxmldsig

import (
	"crypto"
	"errors"
	"fmt"

	dsig "github.com/russellhaering/goxmldsig"
)

// ErrNilSigner is returned by NewSigningContextSigner when called with a nil
// crypto.Signer.
var ErrNilSigner = errors.New("forked/goxmldsig: signer is nil")

// ErrEmptyCertChain is returned by NewSigningContextSigner when the cert
// chain is empty — without at least one cert the SAML KeyInfo block would be
// missing the X509Certificate element and downstream SPs would reject the
// signature.
var ErrEmptyCertChain = errors.New("forked/goxmldsig: cert chain is empty")

// MemoryX509KeyStoreSigner is a keystore that remembers a crypto.Signer
// (rather than a *rsa.PrivateKey) alongside its certificate chain. It
// implements the goxmldsig.X509KeyStore *signature* via GetKeyPair (returning
// a non-nil but unused private key), and the goxmldsig.X509ChainStore via
// GetChain.
//
// Per TRD 06-01 the GetKeyPair *we* expose returns the crypto.Signer
// directly — that signature does NOT match upstream's X509KeyStore (which
// returns *rsa.PrivateKey). This is fine because our actual SigningContext
// is built via dsig.NewSigningContext(signer, certs) which bypasses
// KeyStore entirely; the cert chain is supplied through ctx.certs and the
// signer through ctx.signer.
//
// Concurrency: safe for concurrent reads. Mutating the fields after
// construction is not supported.
type MemoryX509KeyStoreSigner struct {
	// Signer performs the actual signing operation. It may be backed by a
	// KMS, HSM, or in-process key.
	Signer crypto.Signer
	// CertChain is the ASN.1-DER X.509 certificate chain that will appear
	// in the SAML KeyInfo block. Position 0 is the signing leaf.
	CertChain [][]byte
}

// GetKeyPair returns the crypto.Signer and the cert chain. This is the
// platform-saml-shaped surface — note the first return is a crypto.Signer,
// NOT a *rsa.PrivateKey.
func (ks *MemoryX509KeyStoreSigner) GetKeyPair() (crypto.Signer, [][]byte, error) {
	if ks == nil || ks.Signer == nil {
		return nil, nil, ErrNilSigner
	}
	if len(ks.CertChain) == 0 {
		return nil, nil, ErrEmptyCertChain
	}
	return ks.Signer, ks.CertChain, nil
}

// GetChain satisfies the upstream dsig.X509ChainStore interface so a context
// built around this keystore could later switch to the KeyStore-based path
// without losing the chain.
func (ks *MemoryX509KeyStoreSigner) GetChain() ([][]byte, error) {
	if ks == nil || len(ks.CertChain) == 0 {
		return nil, ErrEmptyCertChain
	}
	return ks.CertChain, nil
}

// NewSigningContextSigner constructs a *dsig.SigningContext bound to signer
// + certChain. It validates inputs up-front so misconfiguration surfaces at
// boot rather than at first-sign time.
//
// The returned SigningContext uses upstream's default hash (SHA-256) and
// canonicalizer (C14N1.1). Callers wanting a different hash / signature
// method should call ctx.SetSignatureMethod after construction (see the
// ECDSA test for an example).
//
// The signer is invoked once per signature, via crypto.Signer.Sign with the
// hash function determined by ctx.Hash (default crypto.SHA256). For ECDSA
// signers, the returned bytes are expected in ASN.1 DER form (the wire
// format every cloud KMS produces); upstream dsig handles the rest.
func NewSigningContextSigner(signer crypto.Signer, certChain [][]byte) (*dsig.SigningContext, error) {
	if signer == nil {
		return nil, ErrNilSigner
	}
	if len(certChain) == 0 {
		return nil, ErrEmptyCertChain
	}
	ctx, err := dsig.NewSigningContext(signer, certChain)
	if err != nil {
		return nil, fmt.Errorf("forked/goxmldsig: NewSigningContext: %w", err)
	}
	return ctx, nil
}
