package pki

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
)

// Sentinel errors returned by CA methods. Tests and callers compare via
// errors.Is — we don't expose specific causes (e.g., signature mismatch vs.
// truncated CSR) to avoid leaking parser internals to attackers probing the
// issuance API.
var (
	// ErrCSRInvalid means the supplied CSR bytes are unparseable OR the
	// CSR's self-signature did not verify. Both conditions collapse into
	// one error to avoid leaking parser-state information.
	ErrCSRInvalid = errors.New("pki: csr invalid")

	// ErrInvalidTemplate is returned when the caller passes a nil template
	// to IssueLeaf / IssueLeafFromCSR.
	ErrInvalidTemplate = errors.New("pki: invalid template")
)

// CA wraps a parsed intermediate cert + its signer (crypto.Signer abstraction
// over the HSM-backed key) + a pluggable serial-number generator. The zero
// value of CA is NOT usable — always construct via NewCA.
type CA struct {
	intermediate *x509.Certificate
	signer       crypto.Signer
	serialGen    func() (*big.Int, error)
}

// Option mutates a CA at construction. Used by NewCA for test-only or
// per-deployment customization (deterministic serial generators, etc.).
type Option func(*CA)

// WithSerialGen overrides the default 159-bit random serial generator.
// Intended for unit tests (deterministic serials make assertions readable).
// Production callers should NOT override.
func WithSerialGen(fn func() (*big.Int, error)) Option {
	return func(c *CA) { c.serialGen = fn }
}

// NewCA constructs a CA ready to issue leaves. The intermediate cert must
// have IsCA=true and the signer's public key must match
// `intermediate.PublicKey` (callers are trusted on this — wiring is a
// boot-time concern).
func NewCA(intermediate *x509.Certificate, signer crypto.Signer, opts ...Option) *CA {
	c := &CA{
		intermediate: intermediate,
		signer:       signer,
		serialGen:    randomSerial,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Signer returns the underlying crypto.Signer. Exposed so that CRL builders
// and OCSP responders — which sign with the SAME key as the issuer — can
// share the signer without re-plumbing it through the CA struct.
func (c *CA) Signer() crypto.Signer { return c.signer }

// Intermediate returns the parsed intermediate cert (the issuing CA cert).
// Used by CRL/OCSP code to populate the "Issuer" field of revocation
// responses.
func (c *CA) Intermediate() *x509.Certificate { return c.intermediate }

// IssueLeafFromCSR issues a leaf cert binding `csrDER`'s public key to the
// claims in `template`. The CSR's self-signature is verified before its
// public key is trusted (otherwise an attacker could request a cert binding
// their identity to someone else's pubkey — RFC 2986 §3 attack).
//
// Field precedence (template > CSR):
//
//   - Subject: template wins if non-empty; else CSR's Subject is copied in.
//   - DNSNames: template wins if non-empty; else CSR's DNSNames are copied
//     in.
//   - URIs: template wins if non-empty; else CSR's URIs are copied in.
//
// This allows admin overrides (e.g., enforce SPIFFE URI matches caller's
// authenticated identity) while still allowing self-service flows where the
// CSR carries the desired SAN.
//
// The serial number is always overwritten with a fresh value from the CA's
// configured serialGen — the CSR's serial-number request (if any) is
// ignored, since the CA owns serial-number allocation.
//
// Returns ErrCSRInvalid on parse failure or signature-mismatch. Returns
// ErrInvalidTemplate if template is nil.
func (c *CA) IssueLeafFromCSR(_ context.Context, csrDER []byte, template *x509.Certificate) ([]byte, *x509.Certificate, error) {
	if template == nil {
		return nil, nil, ErrInvalidTemplate
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		// Wrap-but-hide: callers see ErrCSRInvalid only.
		return nil, nil, fmt.Errorf("%w: %v", ErrCSRInvalid, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrCSRInvalid, err)
	}

	serial, err := c.serialGen()
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeafFromCSR: serial: %w", err)
	}
	template.SerialNumber = serial

	// Fill template fields from CSR ONLY when template's corresponding
	// field is empty. Admin overrides take precedence (e.g., enforce
	// tenant-scoped SAN).
	if template.Subject.CommonName == "" && len(template.Subject.Names) == 0 && len(template.Subject.Country) == 0 && len(template.Subject.Organization) == 0 {
		template.Subject = csr.Subject
	}
	if len(template.DNSNames) == 0 {
		template.DNSNames = csr.DNSNames
	}
	if len(template.URIs) == 0 {
		template.URIs = csr.URIs
	}
	if len(template.IPAddresses) == 0 {
		template.IPAddresses = csr.IPAddresses
	}
	if len(template.EmailAddresses) == 0 {
		template.EmailAddresses = csr.EmailAddresses
	}

	der, err := x509.CreateCertificate(rand.Reader, template, c.intermediate, csr.PublicKey, c.signer)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeafFromCSR: create cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeafFromCSR: parse: %w", err)
	}
	return der, cert, nil
}

// IssueLeaf issues a leaf cert binding `pubKey` to the claims in `template`.
// Unlike IssueLeafFromCSR, the caller is responsible for proving ownership of
// `pubKey` out-of-band (e.g., the SVID issuance path where the workload
// authenticates via mTLS to a separate channel and then submits its pubkey).
//
// The serial number is always overwritten with a fresh value from the CA's
// configured serialGen.
//
// Returns ErrInvalidTemplate if template is nil.
func (c *CA) IssueLeaf(_ context.Context, template *x509.Certificate, pubKey crypto.PublicKey) ([]byte, *x509.Certificate, error) {
	if template == nil {
		return nil, nil, ErrInvalidTemplate
	}
	if pubKey == nil {
		return nil, nil, errors.New("pki: IssueLeaf: nil pubKey")
	}
	serial, err := c.serialGen()
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeaf: serial: %w", err)
	}
	template.SerialNumber = serial

	der, err := x509.CreateCertificate(rand.Reader, template, c.intermediate, pubKey, c.signer)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeaf: create cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: IssueLeaf: parse: %w", err)
	}
	return der, cert, nil
}
