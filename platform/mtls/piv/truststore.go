package piv

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// ErrNoCertsInBundle indicates that LoadDoDTrustStore parsed the named
// file but found no PEM-encoded certificates inside it. AppendCertsFromPEM
// returned false — typically because the file is empty, contains only
// non-PEM data, or every PEM block failed to parse.
var ErrNoCertsInBundle = errors.New("piv: no PEM-encoded certs in bundle file")

// LoadDoDTrustStore reads a PEM bundle of DoD root + intermediate CAs
// from the given path and returns an *x509.CertPool suitable for use as
// VerifyOptions.Roots when validating CAC/PIV certificate chains. The
// returned pool is safe to pass to NewTrustValidator.
//
// The operator manages the bundle file out-of-band. DoD PKI publishes
// the All Certs Bundle as a PKCS#7 (.p7b) archive at
// https://dl.dod.cyber.mil/wp-content/uploads/pki-pke/zip/unclass-certificates_pkcs7_DoD.zip
// (typical rotation: annual). Convert PKCS#7 → PEM with:
//
//	openssl pkcs7 -print_certs -inform DER -in DoD_Certs.p7b -out dod-trust.pem
//
// and point AOID at the resulting path (configurable via the
// AOID_DOD_TRUSTSTORE_PATH env var per TRD 07-04).
//
// Hot reload is NOT supported in v1. The DoD rolls roots annually and
// the operator restarts the AOID process to pick up new anchors. A
// future v2 may add a SIGHUP handler.
//
// Returns ErrNoCertsInBundle if AppendCertsFromPEM fails to parse any
// certificates from the file content. Returns a wrapped os error if the
// file cannot be read (e.g., does not exist, permission denied) — wrap
// preserves errors.Is(err, os.ErrNotExist) so callers can branch on the
// underlying stdlib sentinel.
func LoadDoDTrustStore(path string) (*x509.CertPool, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("piv: read trust store at %q: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bs) {
		return nil, ErrNoCertsInBundle
	}
	return pool, nil
}
