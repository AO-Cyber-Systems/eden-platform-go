//go:build cgo

// Package pkcs11 implements platform/kms.KMSSigner over a PKCS#11 module
// (typically SoftHSMv2 in dev, vendor HSM in prod). It wraps
// github.com/ThalesGroup/crypto11.
//
// URI form:
//
//	pkcs11:///etc/aoid/pkcs11.conf?label=<cka-label>
//
// The opaque path is a crypto11 module config file (TOML). The label query
// parameter names the key by its CKA_LABEL attribute.
//
// crypto11 returns ASN.1-DER for ECDSA signatures, so consumers expecting
// JWS-raw r||s must convert via signature.ECDSAJWSFromDER. HealthCheck does
// this conversion internally.
package pkcs11

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

	"github.com/ThalesGroup/crypto11"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

func init() {
	kms.Register("pkcs11", func(ctx context.Context, u *url.URL) (kms.KMSSigner, error) {
		return New(ctx, u)
	})
}

// Signer is the PKCS#11-backed implementation of kms.KMSSigner.
type Signer struct {
	underlying crypto11.Signer
	label      string
	alg        string
}

// New constructs a Signer from a parsed pkcs11:// URI.
func New(_ context.Context, u *url.URL) (*Signer, error) {
	cfgPath := u.Path
	if cfgPath == "" {
		return nil, errors.New("pkcs11: URI is missing the config path (want pkcs11:///<config-path>?label=<key-label>)")
	}
	label := u.Query().Get("label")
	if label == "" {
		return nil, errors.New("pkcs11: URI is missing the ?label= query parameter")
	}

	ctx2, err := crypto11.ConfigureFromFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("pkcs11: configure %q: %w", cfgPath, err)
	}

	keypair, err := ctx2.FindKeyPair(nil, []byte(label))
	if err != nil {
		return nil, fmt.Errorf("pkcs11: find key %q: %w", label, err)
	}
	if keypair == nil {
		return nil, fmt.Errorf("pkcs11: key with label %q not found in token", label)
	}

	alg, err := detectAlgorithm(keypair.Public())
	if err != nil {
		return nil, fmt.Errorf("pkcs11: detect algorithm: %w", err)
	}

	return &Signer{underlying: keypair, label: label, alg: alg}, nil
}

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

// Public returns the cached public key from the token.
func (s *Signer) Public() crypto.PublicKey { return s.underlying.Public() }

// Sign forwards to the underlying crypto11 signer. The output is ASN.1-DER
// for ECDSA keys; callers expecting raw r||s (e.g. golang-jwt) must convert
// via signature.ECDSAJWSFromDER.
func (s *Signer) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.underlying.Sign(rand, digest, opts)
}

// KeyID returns the CKA_LABEL.
func (s *Signer) KeyID() string { return s.label }

// SigningAlgorithm returns the JWS alg detected at construction.
func (s *Signer) SigningAlgorithm() string { return s.alg }

// HealthCheck signs+verifies kms.HealthCheckPayload against the key.
func (s *Signer) HealthCheck(_ context.Context) error {
	digest := sha256.Sum256(kms.HealthCheckPayload)
	sig, err := s.underlying.Sign(nil, digest[:], crypto.SHA256)
	if err != nil {
		return fmt.Errorf("pkcs11: healthcheck sign: %w", err)
	}
	switch s.alg {
	case "ES256":
		epub, ok := s.underlying.Public().(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("pkcs11: public key not ECDSA (%T)", s.underlying.Public())
		}
		// crypto11 returns DER for ECDSA, so VerifyASN1 is the right call.
		if !ecdsa.VerifyASN1(epub, digest[:], sig) {
			return errors.New("pkcs11: signature did not verify")
		}
		return nil
	case "RS256":
		rpub, ok := s.underlying.Public().(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("pkcs11: public key not RSA (%T)", s.underlying.Public())
		}
		return rsa.VerifyPKCS1v15(rpub, crypto.SHA256, digest[:], sig)
	default:
		return fmt.Errorf("pkcs11: verify path missing for algorithm %q", s.alg)
	}
}
