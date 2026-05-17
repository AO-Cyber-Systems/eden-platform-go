//go:build cgo

package pkcs11

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net/url"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/stretchr/testify/require"
)

// These tests cover URI parsing and algorithm detection only — they do NOT
// touch crypto11 / SoftHSMv2. A live SoftHSM integration test lives in a
// separate build-tag-gated file (softhsm.go / softhsm_test.go) that consumers
// can run manually with `-tags softhsm`. We keep the default test surface
// hermetic so CI doesn't depend on SoftHSMv2 binary availability.

func TestNew_RejectsMissingConfigPath(t *testing.T) {
	t.Run("empty_uri_path", func(t *testing.T) {
		u, err := url.Parse("pkcs11://")
		require.NoError(t, err)
		_, err = New(context.Background(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing the config path")
	})
}

func TestNew_RejectsMissingLabel(t *testing.T) {
	t.Run("config_path_but_no_label", func(t *testing.T) {
		u := &url.URL{Scheme: "pkcs11", Path: "/etc/aoid/pkcs11.conf"}
		_, err := New(context.Background(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "?label=")
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

	t.Run("rejects_p384", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		require.NoError(t, err)
		_, err = detectAlgorithm(&priv.PublicKey)
		require.Error(t, err)
	})

	t.Run("rsa_returns_rs256", func(t *testing.T) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		alg, err := detectAlgorithm(&priv.PublicKey)
		require.NoError(t, err)
		require.Equal(t, "RS256", alg)
	})
}

func TestInit_RegistersScheme(t *testing.T) {
	t.Run("pkcs11_scheme_registered", func(t *testing.T) {
		schemes := kms.RegisteredSchemes()
		require.Contains(t, schemes, "pkcs11")
	})
}
