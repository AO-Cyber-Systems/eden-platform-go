package logingov

import (
	"crypto/rsa"
	"fmt"

	"github.com/hashicorp/cap/oidc/clientassertion"
)

// minRSAKeyBits is the floor on RP signing-key length per Login.gov's
// OIDC documentation. Login.gov rejects assertions signed with shorter
// keys at the token endpoint; we fail closed at construction time to
// surface the misconfiguration earlier and with a clearer error.
const minRSAKeyBits = 2048

// SignClientAssertion produces an RFC 7523 §2.2 client_assertion JWT
// signed with the caller-supplied RSA private key. The returned
// (assertion, assertionType) tuple is intended to be inserted into the
// form body of the Login.gov token-endpoint POST per the
// `private_key_jwt` authentication method.
//
// Parameters:
//
//   - clientID: the RP's Login.gov-registered client_id. Becomes both
//     `iss` and `sub` in the assertion claims (RFC 7523 §3).
//   - audience: the token endpoint URL exactly as published in Login.gov's
//     discovery document (e.g.,
//     https://idp.int.identitysandbox.gov/api/openid_connect/token).
//     MUST equal the token endpoint, NOT the issuer URL — Login.gov
//     verifies this exactly.
//   - key: the RP's RSA private key, ≥ 2048 bits. Shorter keys yield
//     ErrSigningKeyTooShort.
//   - kid: optional Key ID header value. When the RP has multiple keys
//     registered (key rotation window), kid tells Login.gov which
//     public key to verify against. Pass "" if only one key is on file.
//
// Lifetime: the underlying clientassertion library hardcodes a
// 5-minute exp claim per RFC 7523 §3's recommended ceiling. We do not
// shorten it here because Login.gov's reference docs accept the
// standard ceiling and operator clock skew in federal environments
// can be material.
//
// Returns:
//
//   - assertion: the serialized JWT (the value placed in the
//     `client_assertion` form field).
//   - assertionType: the constant
//     "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" (the
//     value placed in the `client_assertion_type` form field).
//   - err: ErrSigningKeyMissing when key is nil;
//     ErrSigningKeyTooShort when key.N.BitLen() < 2048; otherwise the
//     underlying clientassertion library error wrapped with %w for
//     errors.Is/As inspection.
func SignClientAssertion(clientID, audience string, key *rsa.PrivateKey, kid string) (assertion, assertionType string, err error) {
	if key == nil {
		return "", "", ErrSigningKeyMissing
	}
	if key.N.BitLen() < minRSAKeyBits {
		return "", "", ErrSigningKeyTooShort
	}

	opts := []clientassertion.Option{}
	if kid != "" {
		opts = append(opts, clientassertion.WithKeyID(kid))
	}

	j, err := clientassertion.NewJWTWithRSAKey(
		clientID,
		[]string{audience},
		clientassertion.RS256,
		key,
		opts...,
	)
	if err != nil {
		return "", "", fmt.Errorf("logingov: build assertion: %w", err)
	}
	serialized, err := j.Serialize()
	if err != nil {
		return "", "", fmt.Errorf("logingov: serialize assertion: %w", err)
	}
	return serialized, clientassertion.JWTTypeParam, nil
}
