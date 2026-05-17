package awskms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"io"
	"net/url"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/aocybersystems/eden-platform-go/platform/kms/signature"
	"github.com/stretchr/testify/require"
)

// fakeSigner is a hand-rolled crypto.Signer used in tests so we don't need
// live AWS credentials. It exposes the same minimal surface step's awskms
// would: Public() + Sign(). Sign returns JWS-raw r||s for ECDSA P-256.
type fakeSigner struct {
	priv *ecdsa.PrivateKey
}

func newFakeECDSASigner(t *testing.T) *fakeSigner {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return &fakeSigner{priv: priv}
}

func (f *fakeSigner) Public() crypto.PublicKey { return &f.priv.PublicKey }

func (f *fakeSigner) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	der, err := ecdsa.SignASN1(rand.Reader, f.priv, digest)
	if err != nil {
		return nil, err
	}
	return signature.ECDSAJWSFromDER(der, 32)
}

func TestNew_RejectsEmptyPath(t *testing.T) {
	t.Run("empty_uri_path", func(t *testing.T) {
		u, err := url.Parse("awskms://")
		require.NoError(t, err)
		_, err = New(context.Background(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing key ARN")
	})
}

// TestHealthCheck_RoundTripWithFakeSigner exercises the verify path against a
// fake ECDSA signer, replicating what step.sm's awskms does on real AWS
// hardware (returns JWS-raw r||s).
func TestHealthCheck_RoundTripWithFakeSigner(t *testing.T) {
	t.Run("ecdsa_p256_round_trip", func(t *testing.T) {
		fs := newFakeECDSASigner(t)
		s := &Signer{underlying: fs, keyARN: "arn:aws:kms:test:0:key/fake", alg: "ES256"}
		require.NoError(t, s.HealthCheck(context.Background()))
		require.Equal(t, "ES256", s.SigningAlgorithm())
		require.Equal(t, "arn:aws:kms:test:0:key/fake", s.KeyID())
	})
}

func TestVerify_PathSelection(t *testing.T) {
	t.Run("es256_with_valid_sig", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		digest := sha256.Sum256([]byte("hello"))
		der, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
		require.NoError(t, err)
		raw, err := signature.ECDSAJWSFromDER(der, 32)
		require.NoError(t, err)
		require.NoError(t, verify(&priv.PublicKey, digest[:], raw, "ES256"))
	})

	t.Run("rs256_pubkey_type_mismatch", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		err = verify(&priv.PublicKey, make([]byte, 32), make([]byte, 256), "RS256")
		require.Error(t, err)
	})

	t.Run("unsupported_alg", func(t *testing.T) {
		err := verify(nil, nil, nil, "ML-DSA")
		require.Error(t, err)
		require.Contains(t, err.Error(), "ML-DSA")
	})
}

func TestDetectAlgorithm(t *testing.T) {
	t.Run("ecdsa_p256_returns_es256", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		alg, err := detectAlgorithm(&priv.PublicKey)
		require.NoError(t, err)
		require.Equal(t, "ES256", alg)
	})

	t.Run("ecdsa_p384_rejected", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		require.NoError(t, err)
		_, err = detectAlgorithm(&priv.PublicKey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "P-256")
	})

	t.Run("rsa_returns_rs256", func(t *testing.T) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		alg, err := detectAlgorithm(&priv.PublicKey)
		require.NoError(t, err)
		require.Equal(t, "RS256", alg)
	})

	t.Run("unsupported_type", func(t *testing.T) {
		_, err := detectAlgorithm("not a key")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported public key type")
	})
}

// Sanity that the import-side init() registered the awskms scheme. We can't
// easily call kms.Open without a real ARN, but the registry should list it.
func TestInit_RegistersScheme(t *testing.T) {
	t.Run("awskms_scheme_registered", func(t *testing.T) {
		schemes := kms.RegisteredSchemes()
		require.Contains(t, schemes, "awskms")
	})
}
