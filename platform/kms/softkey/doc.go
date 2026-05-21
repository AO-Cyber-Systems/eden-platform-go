// Package softkey implements platform/kms.KMSSigner over a process-resident
// private key that the caller has previously wrapped with a KMSCipher
// (typically AES-256-GCM with an operator-held wrap key).
//
// URI form:
//
//	softkey://aoid/keys/<uuid-v4>
//
// The opaque path UUID is resolved against an Options.Resolver callback that
// the caller wires to its own storage (typically a Postgres row from
// aoid.jwks_keys). softkey itself owns no persistence, no wrap secret, and no
// AOID-specific config — Options is the only contract.
//
// ES256 Sign() returns ASN.1 DER (matching the pkcs11 provider's wire format).
// The platform/auth/kmssigner JWS adapter at platform/auth/kmssigner/method.go
// converts DER → raw r||s on the JWS layer; if softkey returned raw r||s the
// conversion would double-run and produce a garbled signature. RS256 Sign()
// returns the PKCS#1 v1.5 output verbatim (already the JWS wire format).
//
// FIPS posture: softkey delegates all cryptography to crypto/ecdsa,
// crypto/rsa, crypto/aes, and crypto/x509 — every primitive in Go's 1.24+
// FIPS 140-3 module. When the binary is built with GOFIPS140=v1.0.0 and run
// with GODEBUG=fips140=on, all softkey operations route through the FIPS
// module. softkey itself does NOT assert FIPS mode; callers (e.g. AOID's
// internal/crypto FIPS gate) MUST call platform/fipsmode.MustRequire().
//
// Key generation is provided by GenerateAndWrap, which produces a fresh
// ES256 (P-256) or RS256 (2048-bit) keypair, PKCS#8-marshals the private
// half, wraps via a caller-supplied KMSCipher, and returns the wrapped blob
// plus a public JWK. AOID's aoidkey CLI calls this helper.
//
// Registration: softkey registers TWO factory entries in platform/kms:
//
//   - kms.Register("softkey", ...) returns an error directing callers to
//     OpenWithOptions. Bare kms.Open is unsupported because the URI alone
//     does not carry the Resolver + WrapCipher dependencies.
//   - kms.RegisterOptions("softkey", ...) dispatches to softkey.New after
//     type-asserting the opts argument to softkey.Options.
//
// AOID's boot can therefore call kms.OpenWithOptions("softkey://...", opts)
// uniformly with the existing kms.Open path for awskms/azkeys/pkcs11.
package softkey
