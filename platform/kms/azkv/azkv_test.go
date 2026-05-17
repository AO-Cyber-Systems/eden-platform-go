package azkv

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"
	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/stretchr/testify/require"
)

func TestNew_RejectsMissingHost(t *testing.T) {
	t.Run("scheme_only_uri", func(t *testing.T) {
		// Build a *url.URL with empty Host directly — net/url.Parse of
		// "azkeys:///keys/x/y" sets Host="" and Path="/keys/x/y" so this is
		// the realistic edge case.
		u := &url.URL{Scheme: "azkeys", Path: "/keys/x/y"}
		_, err := New(t.Context(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing the Managed HSM host")
	})
}

func TestNew_RejectsMalformedPath(t *testing.T) {
	t.Run("missing_keys_prefix", func(t *testing.T) {
		u := &url.URL{Scheme: "azkeys", Host: "hsm.example.com", Path: "/foo/bar/baz"}
		_, err := New(t.Context(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "/keys/")
	})

	t.Run("missing_version", func(t *testing.T) {
		u := &url.URL{Scheme: "azkeys", Host: "hsm.example.com", Path: "/keys/aoid"}
		_, err := New(t.Context(), u)
		require.Error(t, err)
		require.Contains(t, err.Error(), "/keys/<name>/<version>")
	})
}

func TestParsePublicKey(t *testing.T) {
	t.Run("ec_p256_returns_es256", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		jwk := &azkeys.JSONWebKey{
			Kty: to.Ptr(azkeys.KeyTypeEC),
			Crv: to.Ptr(azkeys.CurveNameP256),
			X:   priv.X.Bytes(),
			Y:   priv.Y.Bytes(),
		}
		pub, alg, err := parsePublicKey(jwk)
		require.NoError(t, err)
		require.Equal(t, "ES256", alg)
		ecpub, ok := pub.(*ecdsa.PublicKey)
		require.True(t, ok)
		require.Equal(t, priv.X, ecpub.X)
		require.Equal(t, priv.Y, ecpub.Y)
	})

	t.Run("rejects_nil_jwk", func(t *testing.T) {
		_, _, err := parsePublicKey(nil)
		require.Error(t, err)
	})

	t.Run("rejects_unsupported_curve", func(t *testing.T) {
		jwk := &azkeys.JSONWebKey{
			Kty: to.Ptr(azkeys.KeyTypeEC),
			Crv: to.Ptr(azkeys.CurveNameP384),
			X:   []byte{0x01},
			Y:   []byte{0x02},
		}
		_, _, err := parsePublicKey(jwk)
		require.Error(t, err)
		require.Contains(t, err.Error(), "P-256")
	})

	t.Run("rejects_unsupported_kty", func(t *testing.T) {
		jwk := &azkeys.JSONWebKey{Kty: to.Ptr(azkeys.KeyTypeRSA)}
		_, _, err := parsePublicKey(jwk)
		require.Error(t, err)
		require.Contains(t, err.Error(), "RSA")
	})
}

func TestInit_RegistersScheme(t *testing.T) {
	t.Run("azkeys_scheme_registered", func(t *testing.T) {
		schemes := kms.RegisteredSchemes()
		require.Contains(t, schemes, "azkeys")
	})
}
