package mtls

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures — hand-built CAs, server certs, and client certs using only
// crypto/x509 + crypto/ecdsa. No third-party cert factory libraries.
// ---------------------------------------------------------------------------

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certDER []byte
	certPEM []byte
}

func newTestCA(t *testing.T, cn string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &testCA{cert: cert, key: key, certDER: der, certPEM: pemBytes}
}

type testCert struct {
	certPEM []byte
	keyPEM  []byte
	leaf    *x509.Certificate
	key     *ecdsa.PrivateKey
}

func newServerCert(t *testing.T, ca *testCA, dnsName string, uris ...*url.URL) *testCert {
	t.Helper()
	return newCert(t, ca, dnsName, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, uris)
}

func newClientCert(t *testing.T, ca *testCA, cn string, uris ...*url.URL) *testCert {
	t.Helper()
	return newCert(t, ca, cn, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, uris)
}

func newCert(t *testing.T, ca *testCA, cn string, ekus []x509.ExtKeyUsage, uris []*url.URL) *testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  ekus,
		DNSNames:     []string{cn, "localhost"},
		IPAddresses:  nil,
		URIs:         uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return &testCert{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		leaf:    leaf,
		key:     key,
	}
}

func writePEM(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, b, 0o600))
	return p
}

// ---------------------------------------------------------------------------
// BuildServerTLSConfig validation tests
// ---------------------------------------------------------------------------

func TestBuildServerTLSConfig_ClientAuthIsRequireAndVerify(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	certFile := writePEM(t, dir, "server.crt", srv.certPEM)
	keyFile := writePEM(t, dir, "server.key", srv.keyPEM)
	caFile := writePEM(t, dir, "ca.pem", ca.certPEM)

	cfg, err := BuildServerTLSConfig(Config{
		ServerCertFile:   certFile,
		ServerKeyFile:    keyFile,
		TrustAnchorsFile: caFile,
	})
	require.NoError(t, err)
	require.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
}

func TestBuildServerTLSConfig_MinVersionFloorIsTLS13(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	cfg, err := BuildServerTLSConfig(Config{
		ServerCertFile:   writePEM(t, dir, "s.crt", srv.certPEM),
		ServerKeyFile:    writePEM(t, dir, "s.key", srv.keyPEM),
		TrustAnchorsFile: writePEM(t, dir, "ca.pem", ca.certPEM),
		MinTLSVersion:    tls.VersionTLS12, // attempt to weaken
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, cfg.MinVersion, uint16(tls.VersionTLS13),
		"MinVersion must be raised to 1.3 even if caller asked for 1.2")
}

func TestBuildServerTLSConfig_RejectsMissingTrustPool(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	_, err := BuildServerTLSConfig(Config{
		ServerCertFile: writePEM(t, dir, "s.crt", srv.certPEM),
		ServerKeyFile:  writePEM(t, dir, "s.key", srv.keyPEM),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TrustAnchorsFile required")
}

func TestBuildServerTLSConfig_RejectsBothFileAndKMSModes(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	_, err = BuildServerTLSConfig(Config{
		ServerCertFile:   writePEM(t, dir, "s.crt", srv.certPEM),
		ServerKeyFile:    writePEM(t, dir, "s.key", srv.keyPEM),
		KMSSigner:        signer,
		ServerCertChain:  srv.certPEM,
		TrustAnchorsFile: writePEM(t, dir, "ca.pem", ca.certPEM),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly one of")
}

func TestBuildServerTLSConfig_RejectsMalformedTrustPool(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	bad := writePEM(t, dir, "bad.pem", []byte("not a pem"))
	_, err := BuildServerTLSConfig(Config{
		ServerCertFile:   writePEM(t, dir, "s.crt", srv.certPEM),
		ServerKeyFile:    writePEM(t, dir, "s.key", srv.keyPEM),
		TrustAnchorsFile: bad,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "malformed PEM")
}

// KMS mode acceptance — uses an ECDSA private key as a crypto.Signer stand-in.
func TestBuildServerTLSConfig_AcceptsKMSMode(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "test-ca")
	srv := newServerCert(t, ca, "localhost")
	cfg, err := BuildServerTLSConfig(Config{
		KMSSigner:        srv.key, // local crypto.Signer used as stand-in
		ServerCertChain:  srv.certPEM,
		TrustAnchorsFile: writePEM(t, dir, "ca.pem", ca.certPEM),
	})
	require.NoError(t, err)
	require.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	require.NotNil(t, cfg.Certificates[0].PrivateKey)
}

// ---------------------------------------------------------------------------
// End-to-end TLS handshake tests
// ---------------------------------------------------------------------------

// withMTLSServer spins up an httptest server using BuildServerTLSConfig and
// returns the server + the CA root pool callers can use to verify the server
// cert from a client.
func withMTLSServer(t *testing.T, ca *testCA, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	srv := newServerCert(t, ca, "localhost")
	cfg, err := BuildServerTLSConfig(Config{
		ServerCertFile:   writePEM(t, dir, "s.crt", srv.certPEM),
		ServerKeyFile:    writePEM(t, dir, "s.key", srv.keyPEM),
		TrustAnchorsFile: writePEM(t, dir, "ca.pem", ca.certPEM),
	})
	require.NoError(t, err)

	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = cfg
	ts.StartTLS()
	t.Cleanup(ts.Close)

	rootPool := x509.NewCertPool()
	require.True(t, rootPool.AppendCertsFromPEM(ca.certPEM))
	return ts, rootPool
}

func TestE2E_RejectsClientWithoutCert(t *testing.T) {
	ca := newTestCA(t, "e2e-ca")
	ts, rootPool := withMTLSServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Client presents NO client cert.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    rootPool,
			ServerName: "localhost",
			MinVersion: tls.VersionTLS13,
		},
	}}
	resp, err := client.Get(ts.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS handshake error, got success")
	}
	require.Error(t, err)
}

func TestE2E_AcceptsClientWithVerifyingCert(t *testing.T) {
	ca := newTestCA(t, "e2e-ca")
	ts, rootPool := withMTLSServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cli := newClientCert(t, ca, "test-client")
	clientTLSCert, err := tls.X509KeyPair(cli.certPEM, cli.keyPEM)
	require.NoError(t, err)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:      rootPool,
			Certificates: []tls.Certificate{clientTLSCert},
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		},
	}}
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestE2E_RejectsClientWithUnknownCACert(t *testing.T) {
	serverCA := newTestCA(t, "server-ca")
	ts, rootPool := withMTLSServer(t, serverCA, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Client cert issued by a DIFFERENT CA not in the server's trust pool.
	otherCA := newTestCA(t, "other-ca")
	cli := newClientCert(t, otherCA, "rogue-client")
	clientTLSCert, err := tls.X509KeyPair(cli.certPEM, cli.keyPEM)
	require.NoError(t, err)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:      rootPool,
			Certificates: []tls.Certificate{clientTLSCert},
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		},
	}}
	resp, err := client.Get(ts.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS handshake error for unknown-CA client cert")
	}
	require.Error(t, err)
}

// Confirm the package's static crypto.Signer type assertion (interface
// satisfaction) holds for the local ecdsa key path used by the KMS-mode test.
func TestStaticSignerAssertion(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	var _ crypto.Signer = priv // compile-time check
}
