package pki

import (
	"crypto/x509"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildCRL_RoundTrip(t *testing.T) {
	_, ca, _ := newTestCA(t)
	now := time.Now().UTC().Truncate(time.Second)
	revoked := []RevokedCert{
		{SerialNumber: big.NewInt(101), RevokedAt: now.Add(-10 * time.Minute), Reason: 1 /* keyCompromise */},
		{SerialNumber: big.NewInt(102), RevokedAt: now.Add(-5 * time.Minute), Reason: 4 /* superseded */},
		{SerialNumber: big.NewInt(103), RevokedAt: now.Add(-1 * time.Minute), Reason: 5 /* cessationOfOperation */},
	}
	crlDER, err := BuildCRL(ca.Intermediate(), ca.Signer(), revoked, big.NewInt(7), now, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, crlDER)

	parsed, err := x509.ParseRevocationList(crlDER)
	require.NoError(t, err)
	require.Equal(t, int64(7), parsed.Number.Int64())
	require.True(t, parsed.ThisUpdate.Equal(now))
	require.True(t, parsed.NextUpdate.Equal(now.Add(24*time.Hour)))
	require.Len(t, parsed.RevokedCertificateEntries, 3)

	// Map serial → entry for order-independent assertions.
	bySerial := map[int64]x509.RevocationListEntry{}
	for _, e := range parsed.RevokedCertificateEntries {
		bySerial[e.SerialNumber.Int64()] = e
	}
	require.Contains(t, bySerial, int64(101))
	require.Contains(t, bySerial, int64(102))
	require.Contains(t, bySerial, int64(103))
	require.Equal(t, 1, bySerial[101].ReasonCode)
	require.Equal(t, 4, bySerial[102].ReasonCode)
	require.Equal(t, 5, bySerial[103].ReasonCode)
}

func TestBuildCRL_SignatureValidatesAgainstIntermediate(t *testing.T) {
	_, ca, _ := newTestCA(t)
	now := time.Now().UTC()
	crlDER, err := BuildCRL(ca.Intermediate(), ca.Signer(),
		[]RevokedCert{{SerialNumber: big.NewInt(1), RevokedAt: now, Reason: 0}},
		big.NewInt(1), now, now.Add(1*time.Hour),
	)
	require.NoError(t, err)
	parsed, err := x509.ParseRevocationList(crlDER)
	require.NoError(t, err)
	// CheckSignatureFrom verifies the CRL was signed by ca.Intermediate().
	require.NoError(t, parsed.CheckSignatureFrom(ca.Intermediate()))
}

func TestBuildCRL_EmptyRevocationList(t *testing.T) {
	_, ca, _ := newTestCA(t)
	now := time.Now().UTC()
	crlDER, err := BuildCRL(ca.Intermediate(), ca.Signer(), nil, big.NewInt(1), now, now.Add(1*time.Hour))
	require.NoError(t, err)
	parsed, err := x509.ParseRevocationList(crlDER)
	require.NoError(t, err)
	require.Empty(t, parsed.RevokedCertificateEntries)
}

func TestBuildCRL_ReasonCodesPersisted(t *testing.T) {
	_, ca, _ := newTestCA(t)
	now := time.Now().UTC()
	// RFC 5280 §5.3.1 reason codes.
	cases := []struct {
		serial int64
		reason int
	}{
		{1, 0}, // unspecified
		{2, 1}, // keyCompromise
		{3, 2}, // cACompromise
		{4, 3}, // affiliationChanged
		{5, 4}, // superseded
		{6, 5}, // cessationOfOperation
	}
	revoked := make([]RevokedCert, 0, len(cases))
	for _, c := range cases {
		revoked = append(revoked, RevokedCert{SerialNumber: big.NewInt(c.serial), RevokedAt: now, Reason: c.reason})
	}
	crlDER, err := BuildCRL(ca.Intermediate(), ca.Signer(), revoked, big.NewInt(2), now, now.Add(1*time.Hour))
	require.NoError(t, err)
	parsed, err := x509.ParseRevocationList(crlDER)
	require.NoError(t, err)

	bySerial := map[int64]int{}
	for _, e := range parsed.RevokedCertificateEntries {
		bySerial[e.SerialNumber.Int64()] = e.ReasonCode
	}
	for _, c := range cases {
		require.Equal(t, c.reason, bySerial[c.serial], "reason for serial %d", c.serial)
	}
}
