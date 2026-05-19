//go:build cgo

package pkcs11

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/ThalesGroup/crypto11"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// Cipher is the PKCS#11-backed implementation of kms.KMSCipher. It targets a
// symmetric AES key referenced by CKA_LABEL inside the HSM token. The HSM
// holds the key; we drive Encrypt/Decrypt via C_Encrypt / C_Decrypt
// (delegated to crypto11's SecretKey).
//
// Algorithm: AES-GCM (CKM_AES_GCM). 96-bit nonce, 128-bit auth tag, prepended
// nonce in the ciphertext envelope. The nonce IS NOT derived from the
// plaintext — it's freshly random for every call (NIST SP 800-38D §8.2.1).
//
// If the token contains an asymmetric key under the given label, Cipher
// returns kms.ErrUnsupported from Encrypt/Decrypt (the underlying
// SecretKey() lookup will fail at construction time).
//
// Note: PKCS#11 module behavior is module-dependent. SoftHSMv2 and most
// hardware vendors support CKM_AES_GCM. Modules that don't will surface
// an error at HealthCheck (recommended bootstrap pattern) rather than at
// first Encrypt call.
type Cipher struct {
	secret *crypto11.SecretKey
	label  string
}

// NewCipher constructs a Cipher from a parsed pkcs11 URI (config path +
// ?label=<cka-label>). Returns kms.ErrUnsupported wrapped in an error if the
// label resolves to an asymmetric key (no symmetric encrypt path available).
func NewCipher(cfgPath, label string) (*Cipher, error) {
	if cfgPath == "" {
		return nil, errors.New("pkcs11: NewCipher: missing config path")
	}
	if label == "" {
		return nil, errors.New("pkcs11: NewCipher: missing label")
	}
	ctx, err := crypto11.ConfigureFromFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("pkcs11: configure %q: %w", cfgPath, err)
	}
	secret, err := ctx.FindKey(nil, []byte(label))
	if err != nil {
		return nil, fmt.Errorf("pkcs11: find symmetric key %q: %w", label, err)
	}
	if secret == nil {
		return nil, fmt.Errorf("pkcs11: symmetric key %q not found in token: %w", label, kms.ErrUnsupported)
	}
	return &Cipher{secret: secret, label: label}, nil
}

// newCipherWithSecret is the test-only constructor for hand-built fakes.
func newCipherWithSecret(secret *crypto11.SecretKey, label string) *Cipher {
	return &Cipher{secret: secret, label: label}
}

// Encrypt seals plaintext with AES-256-GCM using the HSM-held symmetric key.
// Returns nonce(12) || ciphertext_with_tag.
//
// nil plaintext is rejected. If the HSM module rejects the AES-GCM mechanism,
// the error is propagated wrapped — at boot time, callers SHOULD HealthCheck
// by performing a small round trip before going live.
func (c *Cipher) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, errors.New("pkcs11: nil plaintext")
	}
	aead, err := c.secret.NewGCM()
	if err != nil {
		// CKR_MECHANISM_INVALID or similar — treat as unsupported.
		return nil, fmt.Errorf("pkcs11: GCM init: %w", wrapUnsupportedIfMechanismError(err))
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pkcs11: nonce: %w", err)
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// Decrypt unwraps ciphertext produced by Encrypt under the SAME HSM key.
// Expects nonce(12) || ciphertext_with_tag.
func (c *Cipher) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	if ciphertext == nil {
		return nil, errors.New("pkcs11: nil ciphertext")
	}
	if len(ciphertext) < 12+16 { // nonce + min GCM auth tag
		return nil, errors.New("pkcs11: ciphertext too short")
	}
	aead, err := c.secret.NewGCM()
	if err != nil {
		return nil, fmt.Errorf("pkcs11: GCM init: %w", wrapUnsupportedIfMechanismError(err))
	}
	nonce := ciphertext[:aead.NonceSize()]
	body := ciphertext[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("pkcs11: GCM open: %w", err)
	}
	return pt, nil
}

// wrapUnsupportedIfMechanismError checks for the PKCS#11 mechanism-rejection
// signature and converts to ErrUnsupported. We use a small substring scan
// because crypto11 doesn't expose structured error types for these cases.
func wrapUnsupportedIfMechanismError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, needle := range []string{"CKR_MECHANISM_INVALID", "CKR_KEY_TYPE_INCONSISTENT", "mechanism", "not supported"} {
		if indexASCII(msg, needle) >= 0 {
			return fmt.Errorf("%w: %v", kms.ErrUnsupported, err)
		}
	}
	return err
}

// indexASCII is a small substring search; pulled into this file so the
// pkcs11 build doesn't depend on azkv's helpers.
func indexASCII(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Compile-time: confirm the in-package AES primitives stay reachable so
// linters don't strip them if a future refactor inlines secret.NewGCM.
var _ = aes.BlockSize
var _ cipher.AEAD = (*emptyAEAD)(nil)

type emptyAEAD struct{}

func (emptyAEAD) NonceSize() int                              { return 0 }
func (emptyAEAD) Overhead() int                               { return 0 }
func (emptyAEAD) Seal(_, _, _, _ []byte) []byte               { return nil }
func (emptyAEAD) Open(_, _, _, _ []byte) ([]byte, error)      { return nil, nil }
