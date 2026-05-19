package kms

import (
	"context"
	"errors"
)

// ErrUnsupported is the sentinel returned by KMSCipher implementations when
// the underlying key type cannot perform the requested operation. For example,
// AWS KMS keys with key-spec ECC_NIST_P256 can sign but cannot encrypt — a
// caller invoking Encrypt on such a key receives ErrUnsupported.
//
// Consumers that bootstrap an intermediate-CA wrapping key (AOID TRD 05-06)
// should check for ErrUnsupported and produce an operator-actionable error
// instructing them to provision a symmetric (or RSA-wrap) KMS key.
var ErrUnsupported = errors.New("kms: encrypt/decrypt not supported by this key")

// KMSCipher is a small, provider-agnostic envelope-encryption surface for
// wrapping short blobs (e.g., PKCS#8-encoded EC private keys) with a KMS-held
// key.
//
// Operational shape:
//
//   - Ciphertext format is opaque to the caller — providers may use
//     authenticated encryption (AES-GCM), KMS-native envelope formats, or
//     PKCS#11 mechanism-wrappers. Callers MUST NOT inspect the ciphertext
//     bytes.
//   - All operations must be safe for concurrent use by multiple goroutines.
//   - Implementations should reject nil plaintext / ciphertext with a
//     descriptive error (NOT ErrUnsupported — that sentinel is for key-type
//     mismatches).
//   - Maximum plaintext size is provider-dependent. AWS KMS symmetric:
//     4096 bytes. Azure Key Vault symmetric: ~512 bytes. PKCS#11:
//     module-dependent. All providers comfortably exceed the ~120 bytes
//     needed for a PKCS#8 EC P-256 private key.
//
// This interface is a peer to KMSSigner: a single deployment may use the
// same provider (and even the same key URI) to obtain both interfaces.
type KMSCipher interface {
	// Encrypt seals plaintext with the underlying KMS key. The returned
	// ciphertext is provider-opaque (callers must not parse it). On
	// unsupported key types, returns ErrUnsupported.
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)

	// Decrypt unwraps ciphertext previously produced by the SAME KMS
	// key's Encrypt. Cross-key or cross-provider ciphertext is not
	// supported (each provider's wrap format is distinct). On unsupported
	// key types, returns ErrUnsupported.
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}
