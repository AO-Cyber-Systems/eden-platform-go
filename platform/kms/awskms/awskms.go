// Package awskms implements the platform/kms.KMSSigner interface backed by
// AWS GovCloud KMS. It wraps go.step.sm/crypto/kms/awskms which already
// handles MessageType=DIGEST and (for ES256) returns JWS-raw r||s signatures.
//
// The AWS SDK auto-honors AWS_USE_FIPS_ENDPOINT=true; the AOID consumer is
// expected to set this in its container env so all KMS calls hit a FIPS
// endpoint.
//
// URI form:
//
//	awskms:///arn:aws-us-gov:kms:us-gov-west-1:123:key/abcd
//
// The opaque path after the third slash is the full AWS KMS key ARN.
package awskms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/aocybersystems/eden-platform-go/platform/kms/signature"
	sawskms "go.step.sm/crypto/kms/awskms"
	"go.step.sm/crypto/kms/apiv1"
)

func init() {
	kms.Register("awskms", func(ctx context.Context, u *url.URL) (kms.KMSSigner, error) {
		return New(ctx, u)
	})
}

// Signer is the AWS-KMS-backed implementation of kms.KMSSigner.
type Signer struct {
	underlying crypto.Signer
	keyARN     string
	alg        string
}

// New constructs a Signer from a parsed awskms:// URI. The opaque path is
// expected to be the full ARN (URL-encoded slashes inside the ARN are
// preserved by net/url.Parse since the opaque form keeps the path verbatim).
func New(ctx context.Context, u *url.URL) (*Signer, error) {
	keyARN := strings.TrimPrefix(u.Path, "/")
	if keyARN == "" {
		// Handle awskms://?arn=... fallback style; uncommon.
		keyARN = u.Query().Get("arn")
	}
	if keyARN == "" {
		return nil, errors.New("awskms: missing key ARN in URI; want awskms:///<arn>")
	}

	k, err := sawskms.New(ctx, apiv1.Options{URI: "awskms:///" + keyARN})
	if err != nil {
		return nil, fmt.Errorf("awskms: init: %w", err)
	}
	cs, err := k.CreateSigner(&apiv1.CreateSignerRequest{SigningKey: keyARN})
	if err != nil {
		return nil, fmt.Errorf("awskms: create signer: %w", err)
	}
	alg, err := detectAlgorithm(cs.Public())
	if err != nil {
		return nil, fmt.Errorf("awskms: detect algorithm: %w", err)
	}
	return &Signer{underlying: cs, keyARN: keyARN, alg: alg}, nil
}

// detectAlgorithm picks an Eden-supported JWS algorithm name from the public
// key shape. ECDSA P-256 → ES256, RSA → RS256. Other shapes are rejected so
// callers don't accidentally bind to ML-DSA or P-521 keys before the rest of
// the platform supports them.
func detectAlgorithm(pub crypto.PublicKey) (string, error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve.Params().Name != "P-256" {
			return "", fmt.Errorf("ecdsa curve %q not supported (want P-256)", k.Curve.Params().Name)
		}
		return "ES256", nil
	case *rsa.PublicKey:
		return "RS256", nil
	default:
		return "", fmt.Errorf("unsupported public key type %T", pub)
	}
}

// Public returns the AWS KMS public key.
func (s *Signer) Public() crypto.PublicKey { return s.underlying.Public() }

// Sign forwards to the underlying crypto.Signer. For ECDSA keys, step's
// awskms wrapper already converts AWS's DER output to JWS-raw r||s.
func (s *Signer) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.underlying.Sign(rand, digest, opts)
}

// KeyID returns the AWS KMS key ARN.
func (s *Signer) KeyID() string { return s.keyARN }

// SigningAlgorithm returns the JWS alg name detected at construction.
func (s *Signer) SigningAlgorithm() string { return s.alg }

// HealthCheck performs a real Sign+Verify round-trip on kms.HealthCheckPayload.
func (s *Signer) HealthCheck(ctx context.Context) error {
	digest := sha256.Sum256(kms.HealthCheckPayload)
	sig, err := s.underlying.Sign(nil, digest[:], crypto.SHA256)
	if err != nil {
		return fmt.Errorf("awskms: healthcheck sign: %w", err)
	}
	return verify(s.underlying.Public(), digest[:], sig, s.alg)
}

// verify confirms the signature is valid for the given digest+public key. The
// signature is expected to be in JWS-raw r||s form for ECDSA (step's awskms
// emits this); we convert to DER once for ecdsa.VerifyASN1.
func verify(pub crypto.PublicKey, digest, sig []byte, alg string) error {
	switch alg {
	case "ES256":
		der, err := signature.ECDSADERFromJWS(sig)
		if err != nil {
			return fmt.Errorf("awskms: convert sig: %w", err)
		}
		epub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("awskms: public key not ECDSA (%T)", pub)
		}
		if !ecdsa.VerifyASN1(epub, digest, der) {
			return errors.New("awskms: signature did not verify")
		}
		return nil
	case "RS256":
		rpub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("awskms: public key not RSA (%T)", pub)
		}
		return rsa.VerifyPKCS1v15(rpub, crypto.SHA256, digest, sig)
	default:
		return fmt.Errorf("awskms: verify path missing for algorithm %q", alg)
	}
}
