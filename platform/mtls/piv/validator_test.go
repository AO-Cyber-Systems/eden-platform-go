package piv_test

// Test list:
//   - TestTrustValidator_ValidChainPasses — fixture cert + fixture root pool;
//     ValidateChain returns nil.
//   - TestTrustValidator_WrongRootReturnsErrUntrustedChain — fixture cert +
//     a DIFFERENT root pool (unrelated self-signed CA); assert
//     ErrUntrustedChain wraps the underlying x509 error.
//   - TestTrustValidator_ExpiredCertReturnsErrChainExpired — fixture leaf
//     with notAfter=1991; assert ErrChainExpired.
//   - TestTrustValidator_PolicyFilter_AcceptedPolicyPasses — validator
//     constructed with allowedPolicies = the fixture cert's policy OID;
//     PASS.
//   - TestTrustValidator_PolicyFilter_RejectedPolicyFails — validator
//     constructed with a different policy OID; assert ErrPolicyNotSatisfied.
//   - TestTrustValidator_NoPolicyFilterSkipsCheck — allowedPolicies=nil;
//     cert passes regardless of its policies.
//   - TestTrustValidator_NilCertReturnsErr — ValidateChain(nil) returns a
//     non-nil error (NOT a panic).
//   - TestTrustValidator_NilRootsReturnsErr — TrustValidator built with
//     nil roots fails closed.

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/mtls/piv"
)

func mustLoadCert(t *testing.T, name string) *x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatalf("no PEM block in %s", name)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return cert
}

func mustPool(t *testing.T, names ...string) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatalf("read root %s: %v", n, err)
		}
		if !pool.AppendCertsFromPEM(b) {
			t.Fatalf("no certs parsed from %s", n)
		}
	}
	return pool
}

// fixedClock is exposed via the test-only WithFixedClock helper in
// validator.go to inject deterministic time without exporting the
// `now` field on TrustValidator.
func fixedClock(s string) func() time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func TestTrustValidator_ValidChainPasses(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	roots := mustPool(t, "sample_piv_root.pem")

	v := piv.NewTrustValidator(roots, nil)
	if err := v.ValidateChain(cert, nil); err != nil {
		t.Fatalf("ValidateChain err: %v", err)
	}
}

func TestTrustValidator_WrongRootReturnsErrUntrustedChain(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	// Pool contains ONLY the unrelated root, not the issuing root.
	roots := mustPool(t, "sample_piv_other_root.pem")

	v := piv.NewTrustValidator(roots, nil)
	err := v.ValidateChain(cert, nil)
	if !errors.Is(err, piv.ErrUntrustedChain) {
		t.Fatalf("ValidateChain err = %v, want ErrUntrustedChain", err)
	}
	// The wrapped error should be an x509.UnknownAuthorityError, asserted
	// indirectly via the message — errors.As against a stdlib concrete
	// type is brittle across go versions, so accept any wrapping that
	// retains a recognisable hint.
	if !strings.Contains(err.Error(), "signed by unknown authority") &&
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("want wrapped unknown-authority detail, got: %v", err)
	}
}

func TestTrustValidator_ExpiredCertReturnsErrChainExpired(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_expired.pem")
	roots := mustPool(t, "sample_piv_root.pem")

	v := piv.NewTrustValidator(roots, nil)
	err := v.ValidateChain(cert, nil)
	if !errors.Is(err, piv.ErrChainExpired) {
		t.Fatalf("ValidateChain err = %v, want ErrChainExpired", err)
	}
}

func TestTrustValidator_PolicyFilter_AcceptedPolicyPasses(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	roots := mustPool(t, "sample_piv_root.pem")
	allowed := []asn1.ObjectIdentifier{
		{2, 16, 840, 1, 101, 3, 2, 1, 3, 7}, // DoD medium-hardware (example)
	}

	v := piv.NewTrustValidator(roots, allowed)
	if err := v.ValidateChain(cert, nil); err != nil {
		t.Fatalf("ValidateChain err: %v", err)
	}
}

func TestTrustValidator_PolicyFilter_RejectedPolicyFails(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	roots := mustPool(t, "sample_piv_root.pem")
	allowed := []asn1.ObjectIdentifier{
		{2, 16, 840, 1, 101, 3, 2, 1, 3, 99}, // not present on the fixture
	}

	v := piv.NewTrustValidator(roots, allowed)
	err := v.ValidateChain(cert, nil)
	if !errors.Is(err, piv.ErrPolicyNotSatisfied) {
		t.Fatalf("ValidateChain err = %v, want ErrPolicyNotSatisfied", err)
	}
}

func TestTrustValidator_NoPolicyFilterSkipsCheck(t *testing.T) {
	// allowedPolicies = nil → no filter applied even though the cert
	// carries a policy OID.
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	roots := mustPool(t, "sample_piv_root.pem")

	v := piv.NewTrustValidator(roots, nil)
	if err := v.ValidateChain(cert, nil); err != nil {
		t.Fatalf("ValidateChain err: %v", err)
	}
}

func TestTrustValidator_NilCertReturnsErr(t *testing.T) {
	roots := mustPool(t, "sample_piv_root.pem")
	v := piv.NewTrustValidator(roots, nil)

	err := v.ValidateChain(nil, nil)
	if err == nil {
		t.Fatalf("expected error for nil cert, got nil")
	}
	// Must not panic, must surface a clear message.
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil-cert error message, got: %v", err)
	}
}

func TestTrustValidator_NilRootsReturnsErr(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_cert.pem")
	v := piv.NewTrustValidator(nil, nil)

	err := v.ValidateChain(cert, nil)
	if err == nil {
		t.Fatalf("expected error for nil roots, got nil")
	}
}

// Sanity: confirm the expired fixture really IS expired against a
// frozen clock that's "now-ish" — protects future maintainers from
// regenerating the fixture without checking the date.
func TestTrustValidator_ExpiredFixtureSanity(t *testing.T) {
	cert := mustLoadCert(t, "sample_piv_expired.pem")
	roots := mustPool(t, "sample_piv_root.pem")
	v := piv.NewTrustValidator(roots, nil)
	piv.WithFixedClock(v, fixedClock("2026-01-01T00:00:00Z"))
	err := v.ValidateChain(cert, nil)
	if !errors.Is(err, piv.ErrChainExpired) {
		t.Fatalf("frozen-clock expiry: err = %v, want ErrChainExpired", err)
	}
}
