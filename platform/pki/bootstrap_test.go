package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newP256Signer creates an in-process ECDSA P-256 signer. Used as a stand-in
// for KMS-backed crypto.Signer in unit tests — the production CA uses a
// platform/kms.KMSSigner here.
func newP256Signer(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return k
}

func TestBootstrapRoot_SelfSignedAndParseable(t *testing.T) {
	signer := newP256Signer(t)
	cert, der, err := BootstrapRoot(signer, pkix.Name{CommonName: "Test Root"}, 20*365*24*time.Hour)
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.NotEmpty(t, der)
	require.True(t, cert.IsCA, "IsCA must be true")
	require.True(t, cert.BasicConstraintsValid, "BasicConstraintsValid must be true")
	require.Equal(t, 1, cert.MaxPathLen, "root MaxPathLen must be 1 (allows one intermediate)")
	require.NotZero(t, cert.SerialNumber)

	// Parse round-trip: stdlib should accept what stdlib produced, and the
	// CA flags must survive marshal/unmarshal.
	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	require.True(t, parsed.IsCA, "parsed IsCA must be true")
	require.True(t, parsed.BasicConstraintsValid, "parsed BasicConstraintsValid must be true")

	// Backdated by ~5 minutes to absorb clock skew.
	require.WithinDuration(t, time.Now().Add(-5*time.Minute), cert.NotBefore, 30*time.Second)
	// NotAfter is roughly validity offset from now.
	require.WithinDuration(t, time.Now().Add(20*365*24*time.Hour), cert.NotAfter, 1*time.Minute)

	// CRITICAL: KeyUsage MUST include CertSign + CRLSign for a CA.
	require.NotZero(t, cert.KeyUsage&x509.KeyUsageCertSign, "KeyUsageCertSign must be set")
	require.NotZero(t, cert.KeyUsage&x509.KeyUsageCRLSign, "KeyUsageCRLSign must be set")
}

func TestBootstrapRoot_SignatureAlgorithmECDSA(t *testing.T) {
	signer := newP256Signer(t)
	cert, _, err := BootstrapRoot(signer, pkix.Name{CommonName: "ECDSA Root"}, 1*time.Hour)
	require.NoError(t, err)
	require.Equal(t, x509.ECDSAWithSHA256, cert.SignatureAlgorithm)
}

func TestBootstrapRoot_SerialIs160Bit(t *testing.T) {
	signer := newP256Signer(t)
	cert, _, err := BootstrapRoot(signer, pkix.Name{CommonName: "S"}, 1*time.Hour)
	require.NoError(t, err)
	// 159-bit random < 2^159, so BitLen <= 159 (with overwhelming probability >150).
	require.LessOrEqual(t, cert.SerialNumber.BitLen(), 159)
}

func TestBootstrapIntermediate_SignedByRoot(t *testing.T) {
	rootSigner := newP256Signer(t)
	rootCert, _, err := BootstrapRoot(rootSigner, pkix.Name{CommonName: "R"}, 20*365*24*time.Hour)
	require.NoError(t, err)

	interSigner := newP256Signer(t)
	interCert, interDER, err := BootstrapIntermediate(
		rootSigner, rootCert, &interSigner.PublicKey,
		pkix.Name{CommonName: "I"}, 10*365*24*time.Hour,
	)
	require.NoError(t, err)
	require.NotEmpty(t, interDER)
	require.True(t, interCert.IsCA, "intermediate IsCA must be true")
	require.True(t, interCert.BasicConstraintsValid, "intermediate BasicConstraintsValid must be true")
	require.True(t, interCert.MaxPathLenZero, "intermediate MaxPathLenZero must be true")
	require.NotZero(t, interCert.KeyUsage&x509.KeyUsageCertSign, "intermediate KeyUsageCertSign required")
	require.NotZero(t, interCert.KeyUsage&x509.KeyUsageCRLSign, "intermediate KeyUsageCRLSign required")

	// Chain.Verify against the root pool: proves the BasicConstraints +
	// IsCA pitfall is fixed.
	pool := x509.NewCertPool()
	pool.AddCert(rootCert)
	parsed, err := x509.ParseCertificate(interDER)
	require.NoError(t, err)
	_, err = parsed.Verify(x509.VerifyOptions{Roots: pool})
	require.NoError(t, err, "intermediate must chain-validate against root")
}

func TestBootstrapIntermediate_RejectsNonCARoot(t *testing.T) {
	rootSigner := newP256Signer(t)
	// Construct a non-CA "root" — a regular leaf cert. Self-sign it via
	// stdlib directly so we don't depend on the pki package mutating
	// fields.
	tmpl := &x509.Certificate{
		SerialNumber: bigIntOne(),
		Subject:      pkix.Name{CommonName: "Non-CA Root"},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(1 * time.Hour),
		IsCA:         false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &rootSigner.PublicKey, rootSigner)
	require.NoError(t, err)
	rootCert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	require.False(t, rootCert.IsCA)

	interSigner := newP256Signer(t)
	_, _, err = BootstrapIntermediate(
		rootSigner, rootCert, &interSigner.PublicKey,
		pkix.Name{CommonName: "I"}, 1*time.Hour,
	)
	require.ErrorIs(t, err, ErrNonCARoot)
}
