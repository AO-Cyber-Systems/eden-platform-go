package piv

import (
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors returned by TrustValidator.ValidateChain. All are
// safe to compare with errors.Is — the validator always wraps a
// concrete underlying cause with %w so callers can branch on the
// sentinel while still surfacing detail in logs.
var (
	// ErrUntrustedChain wraps the underlying x509.Verify failure when
	// the leaf does not chain to one of the configured trust anchors.
	ErrUntrustedChain = errors.New("piv: chain does not validate against pinned trust anchors")

	// ErrChainExpired is returned when the leaf NotBefore is in the
	// future or NotAfter is in the past relative to the validator's
	// clock. Wraps a detail message with the actual dates.
	ErrChainExpired = errors.New("piv: leaf certificate is expired or not yet valid")

	// ErrPolicyNotSatisfied is returned when the validator was
	// constructed with a non-nil allowedPolicies list and the leaf
	// cert carries no PolicyIdentifier in that list.
	ErrPolicyNotSatisfied = errors.New("piv: no certificate policy matches allowed list")
)

// TrustValidator validates a PIV/CAC client certificate against a
// pinned trust-anchor pool, with optional policy-OID filtering for
// DoD assurance-level enforcement.
//
// The zero value is NOT usable — construct via NewTrustValidator.
//
// TrustValidator is safe for concurrent use after construction; the
// internal fields are read-only post-construction.
type TrustValidator struct {
	roots           *x509.CertPool
	allowedPolicies []asn1.ObjectIdentifier
	now             func() time.Time
}

// NewTrustValidator constructs a TrustValidator.
//
// Parameters:
//
//   - roots:           required. The pinned trust-anchor pool. In
//     production this is loaded from the DoD root bundle (e.g.,
//     /etc/ssl/certs/dod-roots.pem). Callers MUST NOT pass the system
//     pool — PIV validation is a pinned-anchor operation.
//
//   - allowedPolicies: optional. When non-nil, the leaf cert MUST list
//     at least one of these OIDs in its certificatePolicies extension
//     (parsed by go's x509 into Certificate.PolicyIdentifiers). When
//     nil, no policy filter is applied (suitable for admin /
//     non-federal deployments).
//
// To use a deterministic clock in tests, see WithFixedClock.
func NewTrustValidator(roots *x509.CertPool, allowedPolicies []asn1.ObjectIdentifier) *TrustValidator {
	return &TrustValidator{
		roots:           roots,
		allowedPolicies: allowedPolicies,
		now:             time.Now,
	}
}

// WithFixedClock replaces v's clock with the supplied function. Intended
// strictly for tests that need to assert ErrChainExpired or to validate
// a fixture cert outside its normal NotBefore/NotAfter window. Not part
// of the production API surface — exported only because go's test
// package boundary forbids access to unexported fields from _test.go in
// the `piv_test` package used for outside-in testing.
func WithFixedClock(v *TrustValidator, clock func() time.Time) {
	v.now = clock
}

// ValidateChain verifies that:
//
//  1. cert is non-nil and the validator was built with a non-nil root pool.
//  2. The current time falls within [cert.NotBefore, cert.NotAfter].
//  3. cert chains to one of v.roots, possibly through intermediates.
//     The chain-build uses ExtKeyUsageClientAuth so client-cert EKU is
//     enforced as part of the chain validation.
//  4. If v.allowedPolicies is non-nil, cert.PolicyIdentifiers must
//     intersect with the allow-list.
//
// Returns nil on success; otherwise a wrapped sentinel error per the
// package's Err* vars.
func (v *TrustValidator) ValidateChain(cert *x509.Certificate, intermediates *x509.CertPool) error {
	if cert == nil {
		return errors.New("piv: nil certificate")
	}
	if v.roots == nil {
		return errors.New("piv: TrustValidator constructed with nil roots")
	}

	now := v.now().UTC()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return fmt.Errorf("%w: NotBefore=%s NotAfter=%s now=%s",
			ErrChainExpired,
			cert.NotBefore.UTC().Format(time.RFC3339),
			cert.NotAfter.UTC().Format(time.RFC3339),
			now.Format(time.RFC3339))
	}

	opts := x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := cert.Verify(opts); err != nil {
		// Map known stdlib errors to sentinel; otherwise wrap as
		// ErrUntrustedChain so callers can branch on errors.Is.
		var invalid x509.CertificateInvalidError
		if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
			return fmt.Errorf("%w: %v", ErrChainExpired, err)
		}
		return fmt.Errorf("%w: %v", ErrUntrustedChain, err)
	}

	if v.allowedPolicies != nil {
		if !hasAllowedPolicy(cert, v.allowedPolicies) {
			return fmt.Errorf("%w: cert policies=%v allowed=%v",
				ErrPolicyNotSatisfied,
				cert.PolicyIdentifiers,
				v.allowedPolicies)
		}
	}

	return nil
}

// hasAllowedPolicy returns true iff any of cert's PolicyIdentifiers
// matches an entry in allowed. Empty cert.PolicyIdentifiers always
// returns false (the cert has no asserted policy to satisfy the
// filter).
func hasAllowedPolicy(cert *x509.Certificate, allowed []asn1.ObjectIdentifier) bool {
	for _, p := range cert.PolicyIdentifiers {
		for _, a := range allowed {
			if p.Equal(a) {
				return true
			}
		}
	}
	return false
}
