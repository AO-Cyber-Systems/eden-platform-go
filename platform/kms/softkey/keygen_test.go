package softkey

import (
	"context"
	"crypto/x509"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateAndWrap_ES256 exercises the canonical happy path the aoidkey
// CLI uses to mint its first software key: generate P-256, PKCS#8-marshal,
// wrap with the AOID-supplied cipher, return uuid + ciphertext + JWK.
func TestGenerateAndWrap_ES256(t *testing.T) {
	t.Run("returns_uuid_ciphertext_and_EC_jwk", func(t *testing.T) {
		ctx := context.Background()
		id, wrapped, jwk, err := GenerateAndWrap(ctx, "ES256", testCipher{})
		require.NoError(t, err)
		require.NotEmpty(t, id)
		require.NotEmpty(t, wrapped)
		require.Equal(t, "EC", jwk.Kty)
		require.Equal(t, "P-256", jwk.Crv)
		require.Equal(t, "ES256", jwk.Alg)
		require.Equal(t, "sig", jwk.Use)
		require.Equal(t, "softkey://aoid/keys/"+id, jwk.Kid)
		require.NotEmpty(t, jwk.X)
		require.NotEmpty(t, jwk.Y)

		// Round-trip: decrypt + parse the PKCS#8 to confirm the wrapped blob
		// is a valid ECDSA private key.
		pkcs8, err := testCipher{}.Decrypt(ctx, wrapped)
		require.NoError(t, err)
		_, err = x509.ParsePKCS8PrivateKey(pkcs8)
		require.NoError(t, err)
	})
}

func TestGenerateAndWrap_RS256(t *testing.T) {
	t.Run("returns_uuid_ciphertext_and_RSA_jwk", func(t *testing.T) {
		ctx := context.Background()
		id, wrapped, jwk, err := GenerateAndWrap(ctx, "RS256", testCipher{})
		require.NoError(t, err)
		require.NotEmpty(t, id)
		require.NotEmpty(t, wrapped)
		require.Equal(t, "RSA", jwk.Kty)
		require.Equal(t, "RS256", jwk.Alg)
		require.Equal(t, "sig", jwk.Use)
		require.NotEmpty(t, jwk.N)
		require.Equal(t, "AQAB", jwk.E, "RSA e=65537 → base64url 'AQAB'")

		// The key must be a valid RSA private key after unwrap.
		pkcs8, err := testCipher{}.Decrypt(ctx, wrapped)
		require.NoError(t, err)
		_, err = x509.ParsePKCS8PrivateKey(pkcs8)
		require.NoError(t, err)
	})
}

func TestGenerateAndWrap_UUIDsAreDistinct(t *testing.T) {
	t.Run("two_calls_produce_different_uuids", func(t *testing.T) {
		ctx := context.Background()
		id1, _, _, err := GenerateAndWrap(ctx, "ES256", testCipher{})
		require.NoError(t, err)
		id2, _, _, err := GenerateAndWrap(ctx, "ES256", testCipher{})
		require.NoError(t, err)
		require.NotEqual(t, id1, id2)
	})
}

func TestGenerateAndWrap_RejectsUnsupportedAlg(t *testing.T) {
	t.Run("HS256_is_rejected", func(t *testing.T) {
		_, _, _, err := GenerateAndWrap(context.Background(), "HS256", testCipher{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported alg")
	})
	t.Run("empty_string_is_rejected", func(t *testing.T) {
		_, _, _, err := GenerateAndWrap(context.Background(), "", testCipher{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported alg")
	})
}

func TestGenerateAndWrap_RejectsNilCipher(t *testing.T) {
	t.Run("nil_cipher_is_rejected", func(t *testing.T) {
		_, _, _, err := GenerateAndWrap(context.Background(), "ES256", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cipher")
	})
}

// TestGenerateAndWrap_RoundTripThroughNew is the cross-helper integration
// test: GenerateAndWrap mints + wraps a key, then softkey.New unwraps it
// through the same cipher, and the resulting signer can produce a valid
// signature. This is the load-bearing CLI-to-runtime contract.
func TestGenerateAndWrap_RoundTripThroughNew(t *testing.T) {
	t.Run("mint_then_load_through_New_ES256", func(t *testing.T) {
		ctx := context.Background()
		id, wrapped, jwk, err := GenerateAndWrap(ctx, "ES256", testCipher{})
		require.NoError(t, err)

		uri := "softkey://aoid/keys/" + id
		require.True(t, strings.HasPrefix(jwk.Kid, "softkey://aoid/keys/"))

		m := map[string]rowT{uri: {alg: "ES256", wrapped: wrapped}}
		u, _ := url.Parse(uri)
		signer, err := New(ctx, u, Options{Resolver: resolverFromMap(m), WrapCipher: testCipher{}})
		require.NoError(t, err)
		require.NoError(t, signer.HealthCheck(ctx))
	})
}
