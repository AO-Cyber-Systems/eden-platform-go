// Package kmssigner registers golang-jwt/v5 SigningMethod implementations for
// ES256 and RS256 that delegate signing to a platform/kms.KMSSigner-shaped key.
//
// Verification uses the standard library (crypto/ecdsa.Verify,
// crypto/rsa.VerifyPKCS1v15) so the hot validation path never round-trips to
// the HSM/KMS — callers cache the public key (typically via a JWKS document).
//
// The canonical integration pattern follows the golang-jwt/v5 MIGRATION_GUIDE,
// "Implementing a Custom Signing Method", with two AOCyber-specific
// adaptations:
//
//  1. ES256 KMS providers return ASN.1-DER ECDSA signatures, but JWS expects
//     the fixed-width raw r||s form. We use
//     platform/kms/signature.ECDSAJWSFromDER to perform the lossless
//     conversion (handles short-r / short-s padding correctly across all
//     signatures, including those with high-bit-unset components).
//
//  2. The Signer interface is intentionally a strict subset of the eden
//     platform/kms.KMSSigner interface so that test fakes can satisfy it
//     without implementing HealthCheck and KeyID. Production code passes a
//     KMSSigner (or any wrapper that embeds one) directly.
//
// # Example — issuing a JWT with a KMS-backed key
//
//	import (
//	    "github.com/aocybersystems/eden-platform-go/platform/auth/kmssigner"
//	    "github.com/golang-jwt/jwt/v5"
//	)
//
//	// One-time registration, at process boot. Idempotent.
//	kmssigner.RegisterAll()
//
//	// activeSigner is a platform/kms.KMSSigner (AWS KMS, Azure Managed HSM,
//	// PKCS#11) or any wrapper that satisfies kmssigner.Signer.
//	claims := jwt.MapClaims{
//	    "iss": "https://aoid.example.com",
//	    "sub": "user-123",
//	    "aud": "edge.example.com",
//	}
//	token := jwt.NewWithClaims(&kmssigner.ES256SigningMethod{}, claims)
//	token.Header["kid"] = activeSigner.KeyID()
//	signed, err := token.SignedString(activeSigner)
//	if err != nil {
//	    return err
//	}
//	_ = signed // hand to the caller / store / wire
//
// After RegisterAll, jwt.GetSigningMethod("ES256") returns *ES256SigningMethod
// as well, so jwt.Parse with WithValidMethods routes through the same
// implementation. Verification uses stdlib and does NOT hit KMS — callers
// fetch the public key from JWKS and pass it directly:
//
//	parsed, err := jwt.Parse(signed, func(t *jwt.Token) (interface{}, error) {
//	    // resolve t.Header["kid"] against your JWKS cache:
//	    return pubKey, nil // *ecdsa.PublicKey
//	}, jwt.WithValidMethods([]string{"ES256", "RS256"}))
//
// # Threading
//
// SigningMethod values are stateless and safe for concurrent use. The Signer
// interface MUST be safe for concurrent use (all eden KMSSigner implementations
// already are).
//
// # Security notes (RFC 8725)
//
// alg="none" tokens are rejected by golang-jwt's default Parse path provided
// callers pass jwt.WithValidMethods. The kmssigner package does NOT register a
// "none" method, and the SigningMethod types reject signer/method alg
// mismatches before any KMS round-trip.
package kmssigner
