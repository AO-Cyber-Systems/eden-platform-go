// Package jwks serves the JSON Web Key Set at /.well-known/jwks.json.
//
// AO ID signs tokens with ML-DSA-65 (post-quantum). The standard JWK
// type registry (RFC 7517) doesn't yet have an entry for ML-DSA, so we
// follow the IETF draft draft-ietf-cose-dilithium direction and use
// kty="AKP" (Algorithm Key Pair) with alg="ML-DSA-65" and a base64url-
// encoded `pub` field carrying the raw public-key bytes (1952 bytes for
// ML-DSA-65).
//
// Rotation: the JWKS response is regenerated on every request from the
// JWTManager's current public-key map. Callers wanting zero-downtime
// rotation push a new key into the manager and the next /jwks.json
// request reflects it. A short HTTP Cache-Control window (5 minutes)
// shields the endpoint from stampedes; production deployments will
// front-load with a CDN.
package jwks

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// Algorithm identifiers exposed by the JWKS document.
const (
	// AlgMLDSA65 is the JOSE alg name for ML-DSA-65 used by platform/auth.
	AlgMLDSA65 = "ML-DSA-65"
	// KtyAKP is the JWK kty value for Algorithm Key Pair (covers ML-DSA;
	// see draft-ietf-cose-dilithium).
	KtyAKP = "AKP"
)

// Key is a single JWK entry. We omit the standard EC / RSA fields and
// surface a single `pub` field carrying the raw public-key bytes; this
// matches the AKP draft and is what a future PQ-aware verifier will
// consume.
type Key struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Pub string `json:"pub"`
}

// Set is the JWK Set wire format (RFC 7517 §5).
type Set struct {
	Keys []Key `json:"keys"`
}

// Build returns a JWK Set populated from the JWTManager's public keys.
// Active key is listed first so straightforward consumers (those that
// pick `keys[0]`) get the signing key. All other registered kids follow
// in unspecified order.
func Build(jm *auth.JWTManager) Set {
	if jm == nil {
		return Set{Keys: []Key{}}
	}
	keys := jm.PublicKeys()
	active := jm.ActiveKID()

	out := Set{Keys: make([]Key, 0, len(keys))}
	if pk, ok := keys[active]; ok {
		out.Keys = append(out.Keys, encode(active, pk.Bytes()))
		delete(keys, active)
	}
	for kid, pk := range keys {
		out.Keys = append(out.Keys, encode(kid, pk.Bytes()))
	}
	return out
}

func encode(kid string, raw []byte) Key {
	return Key{
		Kty: KtyAKP,
		Use: "sig",
		Alg: AlgMLDSA65,
		Kid: kid,
		Pub: base64.RawURLEncoding.EncodeToString(raw),
	}
}

// Handler returns an http.HandlerFunc that emits the current JWK set.
// Cache-Control is set to public, max-age=300 with must-revalidate so
// rotations propagate within five minutes of a successful re-fetch.
func Handler(jm *auth.JWTManager) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		set := Build(jm)
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		_ = json.NewEncoder(w).Encode(set)
	}
}

// EmptyHandler is used when the JWTManager isn't wired yet — early-stage
// boot, server tests that exercise only health/discovery surface.
// Returns an empty JWK Set with the standard headers so probes get a
// well-formed response rather than 404.
func EmptyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
		_ = json.NewEncoder(w).Encode(Set{Keys: []Key{}})
	}
}
