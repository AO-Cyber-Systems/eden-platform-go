package jwks

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
)

// Signer is the minimal interface AddSigningKey needs from a signing-key
// provider: a public key and a signing algorithm string. AOID's
// *crypto.ActiveSigner (from internal/crypto/kms) satisfies this verbatim;
// tests use a hand-built fake to avoid an import cycle.
//
// SigningAlgorithm() MUST return a JWS "alg" Header Parameter value per
// RFC 7518 §3.1 — currently this package recognizes "ES256" and "RS256".
type Signer interface {
	// Public returns the public half of the signing key. The concrete type
	// MUST be *ecdsa.PublicKey for ES256 or *rsa.PublicKey for RS256.
	Public() crypto.PublicKey
	// SigningAlgorithm returns the JWS alg string (e.g. "ES256", "RS256").
	SigningAlgorithm() string
}

// Set is an RFC 7517 §5 JWK Set: an ordered list of JSON Web Keys.
// MarshalJSON renders the canonical {"keys":[...]} shape; insertion order is
// preserved so callers can deterministically express "ACTIVE key first, then
// retiring keys in retirement order".
//
// Set is not safe for concurrent mutation. Build the set once at startup or
// during a key rotation, then serve the marshaled bytes (which are immutable)
// from /.well-known/jwks.json.
type Set struct {
	Keys []JWK
}

// AddSigningKey appends a signing-key JWK derived from signer's public key.
// It selects MarshalECPublic or MarshalRSAPublic based on signer's algorithm
// and uses use="sig". kid is REQUIRED — RFC 7517 §4.5 makes it optional, but
// AOID's contract is that every published key carry a stable identifier so
// JWT verifiers can pick the right one via the "kid" JWS Header Parameter.
//
// Returns an error if the signer is nil, the kid is empty, the algorithm is
// unrecognized, or the signer's public-key type doesn't match the declared
// algorithm (e.g. RSA public key but alg="ES256").
//
// Caller is responsible for kid uniqueness within the Set — this method does
// not enforce it.
func (s *Set) AddSigningKey(signer Signer, kid string) error {
	if signer == nil {
		return errors.New("jwks: nil signer")
	}
	if kid == "" {
		return errors.New("jwks: empty kid")
	}
	alg := signer.SigningAlgorithm()
	switch alg {
	case "ES256":
		pub, ok := signer.Public().(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("jwks: %s signer has non-ecdsa public key %T", alg, signer.Public())
		}
		jwk, err := MarshalECPublic(pub, kid, alg, "sig")
		if err != nil {
			return fmt.Errorf("jwks: marshal EC public for kid=%q: %w", kid, err)
		}
		s.Keys = append(s.Keys, jwk)
		return nil
	case "RS256":
		pub, ok := signer.Public().(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("jwks: %s signer has non-rsa public key %T", alg, signer.Public())
		}
		jwk, err := MarshalRSAPublic(pub, kid, alg, "sig")
		if err != nil {
			return fmt.Errorf("jwks: marshal RSA public for kid=%q: %w", kid, err)
		}
		s.Keys = append(s.Keys, jwk)
		return nil
	default:
		return fmt.Errorf("jwks: unsupported signing algorithm %q (want ES256 or RS256)", alg)
	}
}

// MarshalJSON renders the canonical RFC 7517 §5 JWK Set JSON. The "keys"
// field is ALWAYS present — even when the set is empty, the output is
// `{"keys":[]}` (RFC 7517 §5 makes "keys" REQUIRED).
//
// Insertion order is preserved: encoding/json renders []JWK in slice order,
// which is the same order AddSigningKey appended them. Same input → same
// bytes; this is asserted by TestSet_MarshalJSON_DeterministicOrder.
func (s *Set) MarshalJSON() ([]byte, error) {
	// Promote nil keys to an empty array so the canonical empty-set
	// representation is {"keys":[]} rather than {"keys":null}.
	keys := s.Keys
	if keys == nil {
		keys = []JWK{}
	}
	return json.Marshal(struct {
		Keys []JWK `json:"keys"`
	}{Keys: keys})
}
