package encryption

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// envelopeV1Prefix tags ciphertexts produced via EncryptStringV1.
const envelopeV1Prefix = "v1:"

// EncryptString encrypts the plaintext and returns a base64-encoded ciphertext
// suitable for storage in a string column. Backward-compatible with consumers
// that store raw nonce+ciphertext bytes — callers reading existing data should
// keep using Decrypt(rawBytes); callers that move forward to string storage
// should use this method paired with DecryptString.
func (e *FieldEncryptor) EncryptString(plaintext string) (string, error) {
	ct, err := e.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// EncryptStringV1 encrypts and returns a versioned envelope:
//
//	v1:<base64-of-nonce+ciphertext>
//
// The version prefix allows future key rotations / algorithm bumps without
// breaking storage compatibility.
func (e *FieldEncryptor) EncryptStringV1(plaintext string) (string, error) {
	ct, err := e.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return envelopeV1Prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptString decrypts a ciphertext produced by EncryptString or
// EncryptStringV1. Both forms are accepted so data written before the
// envelope was introduced remains decryptable.
func (e *FieldEncryptor) DecryptString(ciphertext string) (string, error) {
	encoded := strings.TrimPrefix(ciphertext, envelopeV1Prefix)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("encryption: DecryptString: base64 decode: %w", err)
	}
	pt, err := e.Decrypt(raw)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// BlindIndexLower computes a case-insensitive HMAC-SHA256 blind index by
// lower-casing the input before hashing. Use for searchable encrypted columns
// where lookups should be case-insensitive (emails, usernames). Pair with
// `BlindIndexLower` on insert AND lookup so both write and read sides use the
// same canonicalization.
func (e *FieldEncryptor) BlindIndexLower(plaintext string) string {
	mac := hmac.New(sha256.New, e.indexKey)
	mac.Write([]byte(strings.ToLower(plaintext)))
	return hex.EncodeToString(mac.Sum(nil))
}
