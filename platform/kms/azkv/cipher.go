package azkv

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// azkvCipherAPI is the narrow Azure Key Vault azkeys.Client subset used by
// Cipher; allows tests to substitute fakes.
type azkvCipherAPI interface {
	Encrypt(ctx context.Context, name, version string, params azkeys.KeyOperationParameters, opts *azkeys.EncryptOptions) (azkeys.EncryptResponse, error)
	Decrypt(ctx context.Context, name, version string, params azkeys.KeyOperationParameters, opts *azkeys.DecryptOptions) (azkeys.DecryptResponse, error)
}

// Cipher is the Azure-Key-Vault-backed implementation of kms.KMSCipher. It
// targets RSA-OAEP-256 wrapping (the only Eden-supported asymmetric encrypt
// algorithm in Managed HSM). Symmetric AES-KW is also valid for Managed HSM
// only — selected via the keyAlg field.
//
// For EC keys (sign-only), Azure returns an Unsupported algorithm error
// which we translate to kms.ErrUnsupported.
type Cipher struct {
	client  azkvCipherAPI
	name    string
	version string
	host    string
	keyAlg  azkeys.EncryptionAlgorithm
}

// NewCipher constructs a Cipher from a parsed azkeys.Client + key
// name/version. The keyAlg argument selects the encrypt algorithm; for v1
// callers should pass azkeys.EncryptionAlgorithmRSAOAEP256 (asymmetric
// wrapping) or one of the AES-KW variants when targeting an AES key in
// Managed HSM.
func NewCipher(client *azkeys.Client, host, name, version string, keyAlg azkeys.EncryptionAlgorithm) *Cipher {
	return &Cipher{
		client:  client,
		name:    name,
		version: version,
		host:    host,
		keyAlg:  keyAlg,
	}
}

// newCipherWithAPI is the test-only constructor.
func newCipherWithAPI(client azkvCipherAPI, host, name, version string, keyAlg azkeys.EncryptionAlgorithm) *Cipher {
	return &Cipher{
		client:  client,
		name:    name,
		version: version,
		host:    host,
		keyAlg:  keyAlg,
	}
}

// Encrypt wraps plaintext with the configured Azure key + algorithm. Returns
// the opaque Result bytes (callers must not parse them).
//
// nil plaintext is rejected. Unsupported algorithm errors from Azure are
// translated to kms.ErrUnsupported.
func (c *Cipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, errors.New("azkv: nil plaintext")
	}
	resp, err := c.client.Encrypt(ctx, c.name, c.version, azkeys.KeyOperationParameters{
		Algorithm: to.Ptr(c.keyAlg),
		Value:     plaintext,
	}, nil)
	if err != nil {
		return nil, mapAzureError("azkv encrypt", err)
	}
	if resp.Result == nil {
		return nil, errors.New("azkv: empty encrypt result")
	}
	return resp.Result, nil
}

// Decrypt unwraps ciphertext previously produced by Encrypt under the same
// (host, name, version, keyAlg) tuple.
func (c *Cipher) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if ciphertext == nil {
		return nil, errors.New("azkv: nil ciphertext")
	}
	resp, err := c.client.Decrypt(ctx, c.name, c.version, azkeys.KeyOperationParameters{
		Algorithm: to.Ptr(c.keyAlg),
		Value:     ciphertext,
	}, nil)
	if err != nil {
		return nil, mapAzureError("azkv decrypt", err)
	}
	if resp.Result == nil {
		return nil, errors.New("azkv: empty decrypt result")
	}
	return resp.Result, nil
}

// mapAzureError translates Azure SDK errors into eden sentinels. Azure's
// REST API returns Unsupported algorithm errors as 400 responses; we detect
// these via substring inspection (the azidentity SDK does not expose
// structured error types for this case).
func mapAzureError(op string, err error) error {
	msg := err.Error()
	// "is not supported" / "unsupported algorithm" / "ALGORITHM_NOT_SUPPORTED"
	// — Azure's wording varies by SKU; treat them as ErrUnsupported.
	if containsAnyFold(msg, []string{"unsupported algorithm", "is not supported", "algorithm_not_supported"}) {
		return fmt.Errorf("%s: %w", op, kms.ErrUnsupported)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// containsAnyFold returns true if any needle appears in haystack
// case-insensitively. Hand-rolled to avoid pulling in strings.EqualFold loops.
func containsAnyFold(haystack string, needles []string) bool {
	hLower := toLowerASCII(haystack)
	for _, n := range needles {
		if indexASCII(hLower, toLowerASCII(n)) >= 0 {
			return true
		}
	}
	return false
}

func toLowerASCII(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

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
