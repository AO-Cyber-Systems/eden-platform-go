package softkey

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/aocybersystems/eden-platform-go/platform/auth/jwks"
	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// GenerateAndWrap is the helper AOID's aoidkey CLI calls to mint a fresh
// software signing key, wrap the private half, and return material suitable
// for INSERTing into aoid.jwks_keys + publishing in /.well-known/jwks.json.
//
// Algorithms:
//
//   - "ES256" → P-256 ECDSA via crypto/ecdsa.GenerateKey + elliptic.P256
//   - "RS256" → 2048-bit RSA via crypto/rsa.GenerateKey
//   - anything else → error "softkey: unsupported alg" — caller MUST not
//     fall through (the aoidkey CLI surfaces the operator-actionable message)
//
// The wrapped blob is the cipher's opaque ciphertext over the PKCS#8 marshal
// of the private key. Callers MUST persist the ciphertext byte-for-byte; the
// cipher's Decrypt reads its own wrap format.
//
// The returned public JWK carries the kid set to "softkey://aoid/keys/<uuid>"
// — the canonical URI form softkey.New expects at construction time. AOID's
// boot code rebuilds the URI from the DB row UUID + the constant prefix, so
// the JWK kid is a convenience for tools that consume the JWK directly.
//
// The uuid return value is the bare UUID v4 (no scheme prefix). AOID's CLI
// uses it as the primary-key value when INSERTing the row; the canonical URI
// is reconstructed at boot time from that same UUID.
//
// Public-key conversion uses platform/auth/jwks.MarshalECPublic and
// MarshalRSAPublic — already validated to produce RFC 7517/7518-compliant
// output (see jwk_test.go covering 28/28 cases including the RFC 7638 §3.4
// thumbprint example).
func GenerateAndWrap(ctx context.Context, alg string, cipher kms.KMSCipher) (uuidStr string, wrappedPKCS8 []byte, publicJWK jwks.JWK, err error) {
	if cipher == nil {
		return "", nil, jwks.JWK{}, errors.New("softkey: cipher required")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return "", nil, jwks.JWK{}, fmt.Errorf("softkey: uuid: %w", err)
	}
	kid := "softkey://aoid/keys/" + id.String()
	var priv crypto.Signer
	var pubJWK jwks.JWK
	switch alg {
	case "ES256":
		k, gerr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if gerr != nil {
			return "", nil, jwks.JWK{}, fmt.Errorf("softkey: ecdsa generate: %w", gerr)
		}
		jwk, jerr := jwks.MarshalECPublic(&k.PublicKey, kid, "ES256", "sig")
		if jerr != nil {
			return "", nil, jwks.JWK{}, fmt.Errorf("softkey: marshal ec public: %w", jerr)
		}
		priv = k
		pubJWK = jwk
	case "RS256":
		k, gerr := rsa.GenerateKey(rand.Reader, 2048)
		if gerr != nil {
			return "", nil, jwks.JWK{}, fmt.Errorf("softkey: rsa generate: %w", gerr)
		}
		jwk, jerr := jwks.MarshalRSAPublic(&k.PublicKey, kid, "RS256", "sig")
		if jerr != nil {
			return "", nil, jwks.JWK{}, fmt.Errorf("softkey: marshal rsa public: %w", jerr)
		}
		priv = k
		pubJWK = jwk
	default:
		return "", nil, jwks.JWK{}, fmt.Errorf("softkey: unsupported alg %q (want ES256 or RS256)", alg)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", nil, jwks.JWK{}, fmt.Errorf("softkey: marshal pkcs8: %w", err)
	}
	wrapped, err := cipher.Encrypt(ctx, pkcs8)
	if err != nil {
		return "", nil, jwks.JWK{}, fmt.Errorf("softkey: wrap: %w", err)
	}
	return id.String(), wrapped, pubJWK, nil
}
