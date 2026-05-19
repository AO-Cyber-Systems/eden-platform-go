package kmssigner

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/aocybersystems/eden-platform-go/platform/kms/signature"
	"github.com/golang-jwt/jwt/v5"
)

// Signer is the minimum interface a key value must satisfy to be used with
// the kmssigner SigningMethod types.
//
// It is a strict subset of platform/kms.KMSSigner so test fakes can implement
// it without HealthCheck/KeyID. AOID's internal/crypto.ActiveSigner satisfies
// this interface verbatim.
type Signer interface {
	// Public returns the corresponding public key. For ES256 signers, this MUST
	// return *ecdsa.PublicKey; for RS256 signers, *rsa.PublicKey. This is
	// used by the Verify fallback when callers pass a Signer instead of the
	// raw public key.
	Public() crypto.PublicKey

	// Sign produces a signature over digest. opts.HashFunc() must be
	// crypto.SHA256. For ES256, returns ASN.1-DER ECDSA. For RS256, returns
	// PKCS1-v1_5 in the JWS-compatible shape (no conversion needed).
	Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error)

	// SigningAlgorithm reports the JWS algorithm name ("ES256", "RS256").
	// Used to sanity-check that callers paired the signer with the matching
	// SigningMethod.
	SigningAlgorithm() string
}

// ErrAlgMismatch is returned when a Signer's reported SigningAlgorithm does not
// match the SigningMethod's Alg().
var ErrAlgMismatch = errors.New("kmssigner: signer alg does not match method alg")

// ES256SigningMethod implements jwt.SigningMethod for ES256, delegating the
// actual sign operation to a kmssigner.Signer (typically a KMSSigner).
//
// Sign path:
//   raw := sha256.Sum256([]byte(signingString))
//   derSig, err := key.Sign(rand.Reader, raw[:], crypto.SHA256)
//   return signature.ECDSAJWSFromDER(derSig, 32)  // 32 = P-256 component width
//
// Verify path uses stdlib crypto/ecdsa.Verify (no KMS round-trip).
type ES256SigningMethod struct{}

// Alg returns "ES256".
func (m *ES256SigningMethod) Alg() string { return "ES256" }

// Sign delegates to the Signer and converts DER → JWS raw r||s.
//
// key MUST implement Signer. If key.SigningAlgorithm() != "ES256" the call
// fails fast (no KMS round-trip) with an ErrAlgMismatch-wrapped error.
func (m *ES256SigningMethod) Sign(signingString string, key interface{}) ([]byte, error) {
	signer, ok := key.(Signer)
	if !ok {
		return nil, fmt.Errorf("kmssigner: ES256 sign: %w: got %T", jwt.ErrInvalidKeyType, key)
	}
	if alg := signer.SigningAlgorithm(); alg != "ES256" {
		return nil, fmt.Errorf("%w: signer reports %q, method is ES256", ErrAlgMismatch, alg)
	}
	digest := sha256.Sum256([]byte(signingString))
	derSig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("kmssigner: ES256 sign: %w", err)
	}
	jwsSig, err := signature.ECDSAJWSFromDER(derSig, 32)
	if err != nil {
		return nil, fmt.Errorf("kmssigner: ES256 sign: DER conversion: %w", err)
	}
	return jwsSig, nil
}

// Verify checks an ES256 JWS signature using stdlib crypto/ecdsa.Verify.
//
// key may be:
//   - *ecdsa.PublicKey: used directly.
//   - Signer: Public() is called and asserted as *ecdsa.PublicKey.
//
// Returns jwt.ErrTokenSignatureInvalid on signature mismatch, or a wrapped
// error for malformed inputs (wrong length, non-ECDSA key, etc).
func (m *ES256SigningMethod) Verify(signingString string, sig []byte, key interface{}) error {
	pub, err := extractECDSAPublicKey(key)
	if err != nil {
		return fmt.Errorf("kmssigner: ES256 verify: %w", err)
	}
	if len(sig) != 64 {
		return fmt.Errorf("kmssigner: ES256 verify: signature length %d, want 64: %w",
			len(sig), jwt.ErrTokenSignatureInvalid)
	}
	digest := sha256.Sum256([]byte(signingString))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return jwt.ErrTokenSignatureInvalid
	}
	return nil
}

// RS256SigningMethod implements jwt.SigningMethod for RS256, delegating the
// actual sign operation to a kmssigner.Signer. RSA-PKCS1-v1_5 signatures from
// KMS providers match the JWS wire shape directly (no conversion needed).
type RS256SigningMethod struct{}

// Alg returns "RS256".
func (m *RS256SigningMethod) Alg() string { return "RS256" }

// Sign delegates to the Signer. KMS providers return PKCS1-v1_5 in the JWS
// shape directly, so no conversion is needed.
func (m *RS256SigningMethod) Sign(signingString string, key interface{}) ([]byte, error) {
	signer, ok := key.(Signer)
	if !ok {
		return nil, fmt.Errorf("kmssigner: RS256 sign: %w: got %T", jwt.ErrInvalidKeyType, key)
	}
	if alg := signer.SigningAlgorithm(); alg != "RS256" {
		return nil, fmt.Errorf("%w: signer reports %q, method is RS256", ErrAlgMismatch, alg)
	}
	digest := sha256.Sum256([]byte(signingString))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("kmssigner: RS256 sign: %w", err)
	}
	return sig, nil
}

// Verify checks an RS256 signature using stdlib crypto/rsa.VerifyPKCS1v15.
//
// key may be:
//   - *rsa.PublicKey: used directly.
//   - Signer: Public() is called and asserted as *rsa.PublicKey.
func (m *RS256SigningMethod) Verify(signingString string, sig []byte, key interface{}) error {
	pub, err := extractRSAPublicKey(key)
	if err != nil {
		return fmt.Errorf("kmssigner: RS256 verify: %w", err)
	}
	digest := sha256.Sum256([]byte(signingString))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		// Surface jwt.ErrTokenSignatureInvalid so consumers using errors.Is
		// can branch uniformly across signature methods.
		return fmt.Errorf("%w: %v", jwt.ErrTokenSignatureInvalid, err)
	}
	return nil
}

// extractECDSAPublicKey accepts a *ecdsa.PublicKey directly or a Signer whose
// Public() returns one.
func extractECDSAPublicKey(key interface{}) (*ecdsa.PublicKey, error) {
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		return k, nil
	case Signer:
		pub, ok := k.Public().(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("Signer.Public() returned %T, want *ecdsa.PublicKey: %w",
				k.Public(), jwt.ErrInvalidKeyType)
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("%w: got %T, want *ecdsa.PublicKey or Signer", jwt.ErrInvalidKeyType, key)
	}
}

// extractRSAPublicKey accepts a *rsa.PublicKey directly or a Signer whose
// Public() returns one.
func extractRSAPublicKey(key interface{}) (*rsa.PublicKey, error) {
	switch k := key.(type) {
	case *rsa.PublicKey:
		return k, nil
	case Signer:
		pub, ok := k.Public().(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("Signer.Public() returned %T, want *rsa.PublicKey: %w",
				k.Public(), jwt.ErrInvalidKeyType)
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("%w: got %T, want *rsa.PublicKey or Signer", jwt.ErrInvalidKeyType, key)
	}
}
