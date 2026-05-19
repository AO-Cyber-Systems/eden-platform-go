package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
)

// JWK is an RFC 7517 §4 + §3.2 / §3.3 JSON Web Key for ES256 P-256 and RS256
// RSA signing keys. Private-key fields (d, p, q, dp, dq, qi) are intentionally
// omitted from this struct — this package marshals PUBLIC keys only.
//
// Field ordering in JSON output follows Go's struct-declaration order; for the
// canonical lexicographic ordering required by RFC 7638 thumbprints see
// RFC7638Thumbprint, which hand-builds the canonical JSON rather than reusing
// encoding/json.
type JWK struct {
	// Kty is the JWK key type. "EC" for ECDSA P-256, "RSA" for RSA.
	Kty string `json:"kty"`
	// Crv is the EC curve identifier. Present for EC keys only. "P-256" is
	// the only curve this package emits.
	Crv string `json:"crv,omitempty"`
	// Alg is the JWS signing algorithm. "ES256" for EC, "RS256" for RSA.
	// AOID's JWKS publish always carries alg so verifiers don't have to
	// guess; RFC 7517 marks alg OPTIONAL but we treat it as required.
	Alg string `json:"alg"`
	// Use is the public key use. "sig" for signing keys (the only use this
	// package emits today).
	Use string `json:"use"`
	// Kid is the caller-assigned key identifier. RFC 7517 requires kid
	// uniqueness within a JWK Set; the Set itself does not enforce this at
	// AddSigningKey time — it is the caller's responsibility.
	Kid string `json:"kid"`
	// X is the EC X-coordinate, 32-byte big-endian base64url-no-pad (P-256).
	// Per RFC 7518 §6.2.1.2, the octet string MUST be the full coordinate
	// width — 32 bytes for P-256 — left-padded with zeros if the natural
	// big.Int representation is shorter.
	X string `json:"x,omitempty"`
	// Y is the EC Y-coordinate, 32-byte big-endian base64url-no-pad (P-256).
	Y string `json:"y,omitempty"`
	// N is the RSA modulus, minimal-length big-endian base64url-no-pad
	// (RFC 7518 §6.3.1.1).
	N string `json:"n,omitempty"`
	// E is the RSA public exponent, minimal-length big-endian
	// base64url-no-pad. For the common e=65537 case this is "AQAB".
	E string `json:"e,omitempty"`
}

// p256CoordBytes is the per-coordinate width for P-256 ECDSA keys: 32 bytes.
const p256CoordBytes = 32

// MarshalECPublic returns a JWK for a P-256 ECDSA public key. Per
// RFC 7518 §6.2.1, the X and Y coordinates are encoded as 32-byte
// big-endian base64url-no-pad octets — left-padded with zeros when the
// natural big.Int representation is shorter than 32 bytes. This is achieved
// with big.Int.FillBytes; do NOT use big.Int.Bytes() (which drops leading
// zeros and produces variable-length output).
//
// alg should be "ES256". use should be "sig". kid is the caller-assigned key
// identifier. Returns an error if pub is nil or pub.Curve != elliptic.P256().
//
// Future expansion: P-384 / P-521 would add ES384 / ES512 with 48 / 66 byte
// coordinate widths. This package rejects them today rather than silently
// emitting wrong-sized X/Y bytes.
func MarshalECPublic(pub *ecdsa.PublicKey, kid, alg, use string) (JWK, error) {
	if pub == nil {
		return JWK{}, errors.New("jwks: nil public key")
	}
	if pub.Curve != elliptic.P256() {
		curveName := "unknown"
		if pub.Curve != nil && pub.Curve.Params() != nil {
			curveName = pub.Curve.Params().Name
		}
		return JWK{}, fmt.Errorf("jwks: unsupported curve %s, only P-256 supported", curveName)
	}
	if pub.X == nil || pub.Y == nil {
		return JWK{}, errors.New("jwks: EC public key missing X or Y")
	}
	x := make([]byte, p256CoordBytes)
	y := make([]byte, p256CoordBytes)
	pub.X.FillBytes(x) // preserves length — exactly 32 bytes
	pub.Y.FillBytes(y)
	return JWK{
		Kty: "EC",
		Crv: "P-256",
		Alg: alg,
		Use: use,
		Kid: kid,
		X:   base64.RawURLEncoding.EncodeToString(x),
		Y:   base64.RawURLEncoding.EncodeToString(y),
	}, nil
}

// MarshalRSAPublic returns a JWK for an RSA public key per RFC 7518 §6.3.1.
// N is the minimal-length big-endian base64url-no-pad encoding of the
// modulus; E is the minimal-length big-endian encoding of the public
// exponent (typically "AQAB" for 65537).
//
// alg should be "RS256". use should be "sig". kid is the caller-assigned key
// identifier. Returns an error if pub is nil.
//
// Moduli smaller than 2048 bits are accepted (RFC 7518 sets no minimum) but
// emit a slog.Warn — production callers must not publish 1024-bit RSA keys.
func MarshalRSAPublic(pub *rsa.PublicKey, kid, alg, use string) (JWK, error) {
	if pub == nil {
		return JWK{}, errors.New("jwks: nil public key")
	}
	if pub.N == nil {
		return JWK{}, errors.New("jwks: RSA public key missing N")
	}
	if bits := pub.N.BitLen(); bits < 2048 {
		slog.Warn("jwks: RSA key smaller than 2048 bits (operationally unsafe)", "bits", bits, "kid", kid)
	}
	// N — minimal-length big-endian; pub.N.Bytes() returns exactly this.
	n := pub.N.Bytes()
	// E — write as 8 bytes big-endian (RSA public exponent fits in int,
	// which on Go is at least 32 bits and usually 64). Strip leading zeros
	// so the encoded form is minimal-length per RFC 7518 §6.3.1.2.
	if pub.E <= 0 {
		return JWK{}, fmt.Errorf("jwks: RSA public exponent must be positive, got %d", pub.E)
	}
	eBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(eBuf, uint64(pub.E))
	// Strip leading zero bytes — for the common e=65537 this leaves
	// {0x01, 0x00, 0x01} which base64url-encodes to "AQAB".
	for len(eBuf) > 1 && eBuf[0] == 0 {
		eBuf = eBuf[1:]
	}
	return JWK{
		Kty: "RSA",
		Alg: alg,
		Use: use,
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(n),
		E:   base64.RawURLEncoding.EncodeToString(eBuf),
	}, nil
}
