package awskms

import (
	"context"
	"errors"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	awskmssvc "github.com/aws/aws-sdk-go-v2/service/kms"
)

// awsKMSCipherAPI is the narrow subset of *kms.Client used by Cipher; pulled
// out so tests can substitute fakes without spinning up an AWS SDK call.
type awsKMSCipherAPI interface {
	Encrypt(ctx context.Context, in *awskmssvc.EncryptInput, opts ...func(*awskmssvc.Options)) (*awskmssvc.EncryptOutput, error)
	Decrypt(ctx context.Context, in *awskmssvc.DecryptInput, opts ...func(*awskmssvc.Options)) (*awskmssvc.DecryptOutput, error)
}

// Cipher is the AWS-KMS-backed implementation of kms.KMSCipher. It wraps the
// standard AWS KMS Encrypt + Decrypt operations, suitable for symmetric KMS
// keys (key-spec SYMMETRIC_DEFAULT) or RSA wrap keys.
//
// For sign-only key specs (ECC_NIST_P256, ECC_NIST_P384, etc.) the AWS SDK
// returns an InvalidKeyUsage error; Cipher translates this into
// kms.ErrUnsupported so AOID's bootstrap can produce an operator-actionable
// message.
type Cipher struct {
	client awsKMSCipherAPI
	keyID  string
}

// NewCipher constructs a Cipher from an AWS KMS client + key identifier
// (ARN, alias, or key ID).
func NewCipher(client *awskmssvc.Client, keyID string) *Cipher {
	return &Cipher{client: client, keyID: keyID}
}

// newCipherWithAPI is the test-only constructor that accepts the narrow API
// interface for unit testing.
func newCipherWithAPI(client awsKMSCipherAPI, keyID string) *Cipher {
	return &Cipher{client: client, keyID: keyID}
}

// Encrypt calls AWS KMS Encrypt. The returned blob is the AWS-native opaque
// CiphertextBlob (callers must not parse it).
//
// nil plaintext is rejected. InvalidKeyUsage from AWS (sign-only key) is
// translated to kms.ErrUnsupported.
func (c *Cipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if plaintext == nil {
		return nil, errors.New("awskms: nil plaintext")
	}
	out, err := c.client.Encrypt(ctx, &awskmssvc.EncryptInput{
		KeyId:     &c.keyID,
		Plaintext: plaintext,
	})
	if err != nil {
		return nil, mapAWSKMSError("awskms encrypt", err)
	}
	return out.CiphertextBlob, nil
}

// Decrypt calls AWS KMS Decrypt. The KeyId is passed explicitly so AWS KMS
// returns a clear error if the ciphertext was sealed under a different key
// (defends against cross-tenant ciphertext-confusion attacks).
func (c *Cipher) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if ciphertext == nil {
		return nil, errors.New("awskms: nil ciphertext")
	}
	out, err := c.client.Decrypt(ctx, &awskmssvc.DecryptInput{
		KeyId:          &c.keyID,
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, mapAWSKMSError("awskms decrypt", err)
	}
	return out.Plaintext, nil
}

// mapAWSKMSError translates AWS SDK errors into eden sentinels where
// appropriate. InvalidKeyUsageException — returned when caller invokes
// Encrypt/Decrypt on an ECC key — collapses to kms.ErrUnsupported.
func mapAWSKMSError(op string, err error) error {
	var iku *types.InvalidKeyUsageException
	if errors.As(err, &iku) {
		return fmt.Errorf("%s: %w", op, kms.ErrUnsupported)
	}
	return fmt.Errorf("%s: %w", op, err)
}
