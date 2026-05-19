// Package jwks marshals public signing keys to RFC 7517 JSON Web Keys (JWK)
// and aggregates them into RFC 7517 §5 JWK Sets ready for serving at
// /.well-known/jwks.json.
//
// Scope and intent:
//
//   - MarshalECPublic encodes *ecdsa.PublicKey (P-256 only) to a JWK per
//     RFC 7518 §6.2.1. X and Y are always 32-byte big-endian base64url-no-pad
//     encoded.
//   - MarshalRSAPublic encodes *rsa.PublicKey (2048-bit and larger) to a JWK
//     per RFC 7518 §6.3.1. N is the minimal-length big-endian base64url-no-pad
//     encoding of the modulus; E is the public exponent (typically "AQAB" for
//     65537).
//   - Set wraps []JWK with a stable JSON shape {"keys":[...]} that is
//     deterministic in insertion order — callers add the ACTIVE key first,
//     then retiring keys in retirement order.
//   - RFC7638Thumbprint computes the JWK thumbprint per RFC 7638 §3.1 (SHA-256
//     over canonical JSON of REQUIRED members in lexicographic order). Useful
//     as the default kid when callers don't supply one.
//
// This package marshals PUBLIC keys only. Private-key fields (d, p, q, dp, dq,
// qi for RSA; d for EC) are intentionally never emitted. The Marshal helpers
// take *ecdsa.PublicKey / *rsa.PublicKey, so there is no compile-time path to
// leak private material through this surface.
//
// Operational note: RSA moduli smaller than 2048 bits are accepted but emit a
// slog.Warn. Callers should not publish 1024-bit RSA keys in production JWKS.
//
// Example:
//
//	set := jwks.Set{}
//	_ = set.AddSigningKey(activeSigner, "key-2026-05-01")
//	_ = set.AddSigningKey(retiringSigner, "key-2026-02-01")
//	body, _ := json.Marshal(&set) // serve at /.well-known/jwks.json
//
// References: RFC 7517 (JWK), RFC 7518 §6.2/§6.3 (EC/RSA parameters),
// RFC 7638 §3.1 (thumbprint).
package jwks
