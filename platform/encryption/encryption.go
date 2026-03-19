package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// FieldEncryptor provides AES-GCM field-level encryption with HMAC-SHA256 blind indexing.
type FieldEncryptor struct {
	encKey   []byte // 32-byte AES-256 key
	indexKey []byte // 32-byte HMAC key for blind indexes
}

// New creates a new FieldEncryptor with the given keys.
func New(encryptionKey, blindIndexKey []byte) (*FieldEncryptor, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes")
	}
	if len(blindIndexKey) != 32 {
		return nil, fmt.Errorf("blind index key must be 32 bytes")
	}
	return &FieldEncryptor{encKey: encryptionKey, indexKey: blindIndexKey}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *FieldEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.encKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts AES-256-GCM ciphertext.
func (e *FieldEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.encKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// BlindIndex generates an HMAC-SHA256 blind index for searchable encrypted fields.
func (e *FieldEncryptor) BlindIndex(plaintext string) string {
	mac := hmac.New(sha256.New, e.indexKey)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateKey generates a cryptographically secure 32-byte key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return key, nil
}
