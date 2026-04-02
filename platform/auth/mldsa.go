package auth

import (
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/golang-jwt/jwt/v5"
)

// SigningMethodMLDSA65 implements jwt.SigningMethod for post-quantum ML-DSA-65
// (NIST FIPS 204) using cloudflare/circl.
type SigningMethodMLDSA65 struct{}

// signingMethodMLDSA65 is the singleton instance registered with golang-jwt.
var signingMethodMLDSA65 = &SigningMethodMLDSA65{}

func init() {
	jwt.RegisterSigningMethod("ML-DSA-65", func() jwt.SigningMethod {
		return signingMethodMLDSA65
	})
}

func (m *SigningMethodMLDSA65) Alg() string { return "ML-DSA-65" }

// Sign creates an ML-DSA-65 signature. key must be *mldsa65.PrivateKey.
func (m *SigningMethodMLDSA65) Sign(signingString string, key any) ([]byte, error) {
	sk, ok := key.(*mldsa65.PrivateKey)
	if !ok || sk == nil {
		return nil, jwt.ErrInvalidKeyType
	}
	sig := make([]byte, mldsa65.SignatureSize)
	// Randomized signing prevents signature correlation across identical payloads.
	if err := mldsa65.SignTo(sk, []byte(signingString), nil, true, sig); err != nil {
		return nil, err
	}
	return sig, nil
}

// Verify checks an ML-DSA-65 signature. key must be *mldsa65.PublicKey.
func (m *SigningMethodMLDSA65) Verify(signingString string, sig []byte, key any) error {
	pk, ok := key.(*mldsa65.PublicKey)
	if !ok || pk == nil {
		return jwt.ErrInvalidKeyType
	}
	if !mldsa65.Verify(pk, []byte(signingString), nil, sig) {
		return jwt.ErrSignatureInvalid
	}
	return nil
}
