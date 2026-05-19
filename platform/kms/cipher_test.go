package kms

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// inMemoryAESGCM is a hand-built KMSCipher implementation used purely as a
// contract-test fake. It uses AES-256-GCM with a fixed key. Not for
// production use.
type inMemoryAESGCM struct {
	key []byte
}

func newInMemoryAESGCM() *inMemoryAESGCM {
	// 32-byte hard-coded fixture key (NOT random — deterministic test
	// fixture). Hand-built per "no LLM-generated test data".
	return &inMemoryAESGCM{key: []byte("0123456789ABCDEF0123456789ABCDEF")}
}

func (c *inMemoryAESGCM) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := aead.Seal(nil, nonce, plaintext, nil)
	return append(nonce, out...), nil
}

func (c *inMemoryAESGCM) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, body, nil)
}

// unsupportedCipher always returns ErrUnsupported. Mirrors a KMS provider
// whose key type can sign but cannot encrypt (e.g., an ECDSA-only KMS key).
type unsupportedCipher struct{}

func (unsupportedCipher) Encrypt(_ context.Context, _ []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
func (unsupportedCipher) Decrypt(_ context.Context, _ []byte) ([]byte, error) {
	return nil, ErrUnsupported
}

// nilInputCipher rejects nil input — the documented contract expectation for
// providers.
type nilInputCipher struct{ inner KMSCipher }

func (n nilInputCipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, errors.New("kms: nil plaintext")
	}
	return n.inner.Encrypt(ctx, plaintext)
}
func (n nilInputCipher) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if ciphertext == nil {
		return nil, errors.New("kms: nil ciphertext")
	}
	return n.inner.Decrypt(ctx, ciphertext)
}

func TestKMSCipher_RoundTrip(t *testing.T) {
	// Cover with each fake the contract requires producers to honor.
	cases := []struct {
		name   string
		cipher KMSCipher
	}{
		{"in_memory_aes_gcm", newInMemoryAESGCM()},
		{"nil_input_guard_wrapper", nilInputCipher{inner: newInMemoryAESGCM()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			// PKCS#8-EC-private-key sized plaintext (~120 bytes — covers
			// the intermediate-CA-private-key wrapping use case).
			pt := bytes.Repeat([]byte{0xAB}, 120)
			ct, err := tc.cipher.Encrypt(ctx, pt)
			require.NoError(t, err)
			require.NotEqual(t, pt, ct, "ciphertext must differ from plaintext")
			out, err := tc.cipher.Decrypt(ctx, ct)
			require.NoError(t, err)
			require.Equal(t, pt, out)
		})
	}
}

func TestKMSCipher_ErrUnsupportedPropagates(t *testing.T) {
	ctx := context.Background()
	var c KMSCipher = unsupportedCipher{}
	_, err := c.Encrypt(ctx, []byte("hi"))
	require.ErrorIs(t, err, ErrUnsupported)
	_, err = c.Decrypt(ctx, []byte("hi"))
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestKMSCipher_NilInputRejected(t *testing.T) {
	ctx := context.Background()
	c := nilInputCipher{inner: newInMemoryAESGCM()}
	_, err := c.Encrypt(ctx, nil)
	require.Error(t, err)
	_, err = c.Decrypt(ctx, nil)
	require.Error(t, err)
}

func TestKMSCipher_120ByteCAKeyFitsUnderProviderLimits(t *testing.T) {
	// Production case: PKCS#8-encoded EC P-256 private key is ~120 bytes.
	// All target providers' max-plaintext limits exceed 120 bytes:
	//   - AWS KMS: 4096 bytes for symmetric keys
	//   - Azure Key Vault (azkeys): 512 bytes for symmetric wrapping
	//   - PKCS#11: module-dependent, but always >>120 bytes for symmetric keys
	// This test asserts the in-memory fake survives the production-sized
	// payload; provider-specific limit testing belongs in their own test
	// files.
	c := newInMemoryAESGCM()
	pt := bytes.Repeat([]byte{0x42}, 120)
	ct, err := c.Encrypt(context.Background(), pt)
	require.NoError(t, err)
	out, err := c.Decrypt(context.Background(), ct)
	require.NoError(t, err)
	require.Equal(t, pt, out)
}
