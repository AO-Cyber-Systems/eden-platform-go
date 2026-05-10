package encryption

import (
	"crypto/sha256"
)

// DeriveKeyFromPassphrase derives a deterministic 32-byte key from an
// arbitrary-length passphrase by taking SHA-256(passphrase). Suitable for
// bootstrapping AES-256 keys from human-supplied secrets in environments
// where rotating to a randomly generated key (see GenerateKey) is not yet
// feasible — for example AOSentry's `SECRET_KEY_BASE` legacy path.
//
// This helper deliberately uses a single round of SHA-256 with no salt so it
// is byte-compatible with the existing aosentry/pkg/crypto.DeriveKey output
// and any data already encrypted under it. It is NOT a credential-grade KDF.
//
// New systems should prefer `GenerateKey` plus `KeyFromHex` / `KeyFromBase64`
// / `KeyFromEnv`. Reach for `DeriveKeyFromPassphrase` only when migrating
// existing data or interoperating with an external service that requires the
// same derivation.
func DeriveKeyFromPassphrase(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}
