package piv

import (
	"crypto/x509"
	"errors"
	"regexp"
	"sync"
)

// ErrNoEDIPI indicates the certificate has no parseable UPN OtherName,
// so no EDIPI can be derived. Returned by ExtractEDIPI when ExtractUPN
// itself returns ErrNoUPN (including the nil-cert case).
var ErrNoEDIPI = errors.New("piv: no EDIPI in cert (UPN absent)")

// ErrInvalidEDIPI indicates the certificate's UPN OtherName was present
// but did not match the DoD CAC convention of "<10-digit-EDIPI>@mil".
// It is also returned for the well-known test EDIPI "0000000001" when
// RejectTestEDIPI is true (the default).
var ErrInvalidEDIPI = errors.New("piv: UPN not in EDIPI@mil format")

// edipiPattern matches DoD-CAC UPN values of the form "1234567890@mil"
// (exactly 10 decimal digits followed by "@mil"). The single capture
// group yields the EDIPI prefix.
var edipiPattern = regexp.MustCompile(`^([0-9]{10})@mil$`)

// testEDIPI is the DoD-published JITC test EDIPI. Test certificates
// issued for joint-interoperability testing carry this value; we refuse
// it by default to keep test creds out of production sessions.
const testEDIPI = "0000000001"

var (
	rejectTestMu    sync.RWMutex
	rejectTestEDIPI = true
)

// TestingT is the subset of *testing.T that SetRejectTestEDIPIForTest
// requires. Declared as an interface so the package does not import
// testing in production builds.
type TestingT interface {
	Cleanup(func())
}

// SetRejectTestEDIPIForTest toggles the test-EDIPI rejection flag for the
// duration of a single test. The previous value is restored via
// t.Cleanup. This is the only sanctioned way to mutate the flag — a
// production caller has no path to disable the safeguard.
func SetRejectTestEDIPIForTest(t TestingT, val bool) {
	rejectTestMu.Lock()
	prev := rejectTestEDIPI
	rejectTestEDIPI = val
	rejectTestMu.Unlock()
	t.Cleanup(func() {
		rejectTestMu.Lock()
		rejectTestEDIPI = prev
		rejectTestMu.Unlock()
	})
}

// ExtractEDIPI returns the 10-digit DoD EDIPI from the UPN OtherName SAN
// (Microsoft UPN OID 1.3.6.1.4.1.311.20.2.3). DoD CAC cards encode the
// EDIPI as the literal "<10-digit>@mil" in the UPN field, so EDIPI
// extraction is a thin parser on top of ExtractUPN.
//
// Returns ErrNoEDIPI if the UPN is absent (including nil cert).
// Returns ErrInvalidEDIPI if the UPN does not match ^[0-9]{10}@mil$.
// Returns ErrInvalidEDIPI for the test EDIPI "0000000001" when
// RejectTestEDIPI is true (the default).
//
// ExtractEDIPI does not consult FASC-N. Callers that need a FASC-N
// fallback (e.g., PIV-Auth certs whose UPN is missing) should call
// ExtractFASC_N separately.
func ExtractEDIPI(cert *x509.Certificate) (string, error) {
	upn, err := ExtractUPN(cert)
	if err != nil {
		// Any failure to obtain a UPN — missing SAN, missing OtherName,
		// nil cert, or a wrapped parse error — collapses to ErrNoEDIPI
		// for the caller. The UPN-layer error is intentionally not
		// surfaced; CAC federation handlers branch on EDIPI-level
		// sentinels only.
		return "", ErrNoEDIPI
	}
	m := edipiPattern.FindStringSubmatch(upn)
	if m == nil {
		return "", ErrInvalidEDIPI
	}
	edipi := m[1]

	rejectTestMu.RLock()
	reject := rejectTestEDIPI
	rejectTestMu.RUnlock()
	if reject && edipi == testEDIPI {
		return "", ErrInvalidEDIPI
	}
	return edipi, nil
}
