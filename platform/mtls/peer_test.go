package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func stateWithLeaf(leaf *x509.Certificate) *tls.ConnectionState {
	return &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{leaf}},
	}
}

func TestExtractPeerSPIFFEID_ReturnsFirstSPIFFEURI(t *testing.T) {
	leaf := &x509.Certificate{
		URIs: []*url.URL{
			mustURL(t, "https://aoid.test/other"),
			mustURL(t, "spiffe://aoid.local/sa/aoid"),
			mustURL(t, "https://second.test"),
		},
	}
	got, err := ExtractPeerSPIFFEID(stateWithLeaf(leaf))
	require.NoError(t, err)
	require.Equal(t, "spiffe://aoid.local/sa/aoid", got)
}

func TestExtractPeerSPIFFEID_ErrNoSPIFFEWhenOnlyHTTPSURIs(t *testing.T) {
	leaf := &x509.Certificate{URIs: []*url.URL{mustURL(t, "https://aoid.test/only")}}
	_, err := ExtractPeerSPIFFEID(stateWithLeaf(leaf))
	require.ErrorIs(t, err, ErrNoSPIFFEID)
}

func TestExtractPeerSPIFFEID_ErrNoChainWhenStateNil(t *testing.T) {
	_, err := ExtractPeerSPIFFEID(nil)
	require.ErrorIs(t, err, ErrNoVerifiedChain)
}

func TestExtractPeerSPIFFEID_ErrNoChainWhenChainsEmpty(t *testing.T) {
	_, err := ExtractPeerSPIFFEID(&tls.ConnectionState{})
	require.ErrorIs(t, err, ErrNoVerifiedChain)
}

func TestExtractPeerCommonName_ReturnsSubjectCN(t *testing.T) {
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "aoedge.test.com"}}
	got, err := ExtractPeerCommonName(stateWithLeaf(leaf))
	require.NoError(t, err)
	require.Equal(t, "aoedge.test.com", got)
}

func TestExtractPeerCommonName_ErrNoChain(t *testing.T) {
	_, err := ExtractPeerCommonName(nil)
	require.ErrorIs(t, err, ErrNoVerifiedChain)
}
