package telephony

import "github.com/aocybersystems/eden-platform-go/platform/encryption"

// FieldEncrypter implements Encrypter using platform/encryption's AES-256-GCM
// FieldEncryptor. This is the thin production adapter over the PLATFORM
// encryption helper -- it vendors no crypto of its own.
//
// platform/encryption.New takes two 32-byte keys: an encryption key and a
// blind-index (HMAC) key. Telephony credentials are write-only and never
// blind-indexed (nothing searches ciphertext by value), so the same 32-byte
// key is reused for both arguments; the blind index is simply unused here.
//
// platform/encryption produces nonce||ciphertext bytes with the random nonce
// embedded, so two encryptions of the same plaintext differ, and Decrypt
// consumes the embedded nonce -- callers pass the platform/encryption output
// bytes straight through to storage and back.
type FieldEncrypter struct{ fe *encryption.FieldEncryptor }

// NewFieldEncrypter constructs a FieldEncrypter from a 32-byte key. It
// returns an error (surfaced from platform/encryption.New) when key is not
// exactly 32 bytes; callers fall back to NopEncrypter in that case (e.g. dev
// mode with no configured data key).
func NewFieldEncrypter(key []byte) (*FieldEncrypter, error) {
	fe, err := encryption.New(key, key)
	if err != nil {
		return nil, err
	}
	return &FieldEncrypter{fe: fe}, nil
}

// Encrypt implements Encrypter.
func (a *FieldEncrypter) Encrypt(plaintext string) ([]byte, error) {
	return a.fe.Encrypt([]byte(plaintext))
}

// Decrypt implements Encrypter.
func (a *FieldEncrypter) Decrypt(ciphertext []byte) (string, error) {
	b, err := a.fe.Decrypt(ciphertext)
	return string(b), err
}
