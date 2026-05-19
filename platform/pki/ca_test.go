package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// bigIntOne is a tiny helper used across files; centralized here to keep test
// fixtures readable.
func bigIntOne() *big.Int { return big.NewInt(1) }

// newTestCA constructs an in-memory root + intermediate CA + parsed CA
// instance ready for issuance. Returned root cert is the trust anchor for
// chain.Verify assertions.
func newTestCA(t *testing.T) (root *x509.Certificate, ca *CA, interSigner *ecdsa.PrivateKey) {
	t.Helper()
	rootSigner := newP256Signer(t)
	rootCert, _, err := BootstrapRoot(rootSigner, pkix.Name{CommonName: "R"}, 20*365*24*time.Hour)
	require.NoError(t, err)

	interSigner = newP256Signer(t)
	interCert, _, err := BootstrapIntermediate(
		rootSigner, rootCert, &interSigner.PublicKey,
		pkix.Name{CommonName: "I"}, 10*365*24*time.Hour,
	)
	require.NoError(t, err)
	ca = NewCA(interCert, interSigner)
	return rootCert, ca, interSigner
}

// newCSR builds a CSR DER for the given keypair + subject. Hand-built; no LLM
// fixtures. The keypair is the *requester's* key — separate from the CA.
func newCSR(t *testing.T, key *ecdsa.PrivateKey, subject pkix.Name, dnsNames []string, uris []*url.URL) []byte {
	t.Helper()
	tmpl := &x509.CertificateRequest{
		Subject:  subject,
		DNSNames: dnsNames,
		URIs:     uris,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	require.NoError(t, err)
	return der
}

func TestIssueLeafFromCSR_ValidEC_P256_Roundtrip(t *testing.T) {
	rootCert, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	csrDER := newCSR(t, leafKey, pkix.Name{CommonName: "client.example"}, nil, nil)

	template := &x509.Certificate{
		NotBefore:   time.Now().Add(-1 * time.Minute),
		NotAfter:    time.Now().Add(1 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	leafDER, leafCert, err := ca.IssueLeafFromCSR(context.Background(), csrDER, template)
	require.NoError(t, err)
	require.NotEmpty(t, leafDER)
	require.NotNil(t, leafCert)

	parsed, err := x509.ParseCertificate(leafDER)
	require.NoError(t, err)
	require.Equal(t, "client.example", parsed.Subject.CommonName)
	require.False(t, parsed.IsCA, "leaf must not be a CA")

	// Verify leaf → intermediate → root chains correctly.
	rootPool := x509.NewCertPool()
	rootPool.AddCert(rootCert)
	interPool := x509.NewCertPool()
	interPool.AddCert(ca.Intermediate())
	_, err = parsed.Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: interPool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	require.NoError(t, err, "leaf must chain-validate via intermediate to root")
}

func TestIssueLeafFromCSR_RejectsCSRWithInvalidSignature(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	csrDER := newCSR(t, leafKey, pkix.Name{CommonName: "x"}, nil, nil)

	// Flip a byte in the last 8 bytes (signature region) of the DER.
	corrupt := make([]byte, len(csrDER))
	copy(corrupt, csrDER)
	corrupt[len(corrupt)-4] ^= 0x55

	template := &x509.Certificate{
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}
	_, _, err := ca.IssueLeafFromCSR(context.Background(), corrupt, template)
	require.ErrorIs(t, err, ErrCSRInvalid)
}

func TestIssueLeafFromCSR_RejectsMalformedCSR(t *testing.T) {
	_, ca, _ := newTestCA(t)
	garbage := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	template := &x509.Certificate{
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}
	_, _, err := ca.IssueLeafFromCSR(context.Background(), garbage, template)
	require.ErrorIs(t, err, ErrCSRInvalid)
}

func TestIssueLeafFromCSR_TemplateSubjectOverridesCSR(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	// CSR carries Subject CN=requester; admin template overrides to CN=enforced.
	csrDER := newCSR(t, leafKey, pkix.Name{CommonName: "requester"}, nil, nil)

	template := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "enforced"},
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}
	_, leafCert, err := ca.IssueLeafFromCSR(context.Background(), csrDER, template)
	require.NoError(t, err)
	require.Equal(t, "enforced", leafCert.Subject.CommonName)
}

func TestIssueLeafFromCSR_TemplateSANsOverrideCSR(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	csrDER := newCSR(t, leafKey, pkix.Name{}, []string{"requester.example"}, nil)

	template := &x509.Certificate{
		DNSNames:  []string{"enforced.example"},
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}
	_, leafCert, err := ca.IssueLeafFromCSR(context.Background(), csrDER, template)
	require.NoError(t, err)
	require.Equal(t, []string{"enforced.example"}, leafCert.DNSNames)
}

func TestIssueLeafFromCSR_FillsFromCSRWhenTemplateEmpty(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	csrDER := newCSR(t, leafKey, pkix.Name{CommonName: "from-csr"}, []string{"san.from.csr"}, nil)

	// Template subject + DNSNames intentionally left empty — CSR values
	// should fill in.
	template := &x509.Certificate{
		NotBefore: time.Now().Add(-1 * time.Minute),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}
	_, leafCert, err := ca.IssueLeafFromCSR(context.Background(), csrDER, template)
	require.NoError(t, err)
	require.Equal(t, "from-csr", leafCert.Subject.CommonName)
	require.Equal(t, []string{"san.from.csr"}, leafCert.DNSNames)
}

func TestIssueLeaf_DirectPubKey(t *testing.T) {
	rootCert, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)

	template := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "svid.example"},
		NotBefore:   time.Now().Add(-1 * time.Minute),
		NotAfter:    time.Now().Add(1 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, leafCert, err := ca.IssueLeaf(context.Background(), template, &leafKey.PublicKey)
	require.NoError(t, err)
	require.NotEmpty(t, leafDER)
	require.NotNil(t, leafCert)
	require.Equal(t, "svid.example", leafCert.Subject.CommonName)

	rootPool := x509.NewCertPool()
	rootPool.AddCert(rootCert)
	interPool := x509.NewCertPool()
	interPool.AddCert(ca.Intermediate())
	parsed, err := x509.ParseCertificate(leafDER)
	require.NoError(t, err)
	_, err = parsed.Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: interPool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	require.NoError(t, err)
}

func TestIssueLeaf_RejectsNilTemplate(t *testing.T) {
	_, ca, _ := newTestCA(t)
	_, _, err := ca.IssueLeaf(context.Background(), nil, &newP256Signer(t).PublicKey)
	require.ErrorIs(t, err, ErrInvalidTemplate)
}

func TestIssueLeafFromCSR_RejectsNilTemplate(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	csrDER := newCSR(t, leafKey, pkix.Name{CommonName: "x"}, nil, nil)
	_, _, err := ca.IssueLeafFromCSR(context.Background(), csrDER, nil)
	require.ErrorIs(t, err, ErrInvalidTemplate)
}

func TestCA_SignerAndIntermediateAccessors(t *testing.T) {
	_, ca, interSigner := newTestCA(t)
	require.NotNil(t, ca.Signer())
	require.NotNil(t, ca.Intermediate())
	// Signer should be the intermediate signer (used by CRL/OCSP).
	pub1 := ca.Signer().Public().(*ecdsa.PublicKey)
	require.True(t, pub1.Equal(&interSigner.PublicKey))
}

func TestCASerialGeneratorUniqueness(t *testing.T) {
	_, ca, _ := newTestCA(t)
	leafKey := newP256Signer(t)
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		template := &x509.Certificate{
			Subject:   pkix.Name{CommonName: "leaf"},
			NotBefore: time.Now().Add(-1 * time.Minute),
			NotAfter:  time.Now().Add(1 * time.Hour),
		}
		_, leafCert, err := ca.IssueLeaf(context.Background(), template, &leafKey.PublicKey)
		require.NoError(t, err)
		serial := leafCert.SerialNumber.String()
		_, dup := seen[serial]
		require.False(t, dup, "serial %s appeared twice across 100 leaves", serial)
		seen[serial] = struct{}{}
	}
}

func TestCA_WithSerialGen_OverrideRespected(t *testing.T) {
	_, baseCA, _ := newTestCA(t)
	// Build a NEW CA that reuses base CA's intermediate + signer but with a
	// deterministic serial generator. We do this by accessing the
	// accessors and constructing a fresh CA via NewCA.
	counter := big.NewInt(42)
	ca := NewCA(baseCA.Intermediate(), baseCA.Signer(), WithSerialGen(func() (*big.Int, error) {
		// Return ascending serial numbers 42, 43, 44...
		out := new(big.Int).Set(counter)
		counter = counter.Add(counter, big.NewInt(1))
		return out, nil
	}))

	leafKey := newP256Signer(t)
	for i, want := range []int64{42, 43, 44} {
		template := &x509.Certificate{
			Subject:   pkix.Name{CommonName: "leaf"},
			NotBefore: time.Now().Add(-1 * time.Minute),
			NotAfter:  time.Now().Add(1 * time.Hour),
		}
		_, leafCert, err := ca.IssueLeaf(context.Background(), template, &leafKey.PublicKey)
		require.NoError(t, err, "iteration %d", i)
		require.Equal(t, want, leafCert.SerialNumber.Int64(), "iteration %d serial mismatch", i)
	}
	// Silence unused warnings on elliptic — kept for documentation that
	// new ECDSA tests should target P-256.
	_ = elliptic.P256
}
