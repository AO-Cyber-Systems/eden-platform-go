// Package azkv implements platform/kms.KMSSigner against Azure Managed HSM
// (azkeys SDK). Azure returns ECDSA signatures already in JWS-raw r||s form
// for the *Sign* endpoint (the *VerifyDigest* endpoint expects the same), so
// no DER conversion is required for golang-jwt consumption.
//
// URI form:
//
//	azkeys://<hsm-name>.managedhsm.usgovcloudapi.net/keys/<key-name>/<key-version>
//
// The credential is acquired via azidentity.NewDefaultAzureCredential — the
// deploy-time role required is "Managed HSM Crypto User" on the target HSM.
package azkv

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
	"github.com/aocybersystems/eden-platform-go/platform/kms/signature"
)

func init() {
	kms.Register("azkeys", func(ctx context.Context, u *url.URL) (kms.KMSSigner, error) {
		return New(ctx, u)
	})
}

// Signer is the Azure-Managed-HSM-backed implementation of kms.KMSSigner.
type Signer struct {
	client  *azkeys.Client
	name    string
	version string
	host    string
	pub     crypto.PublicKey
	alg     string
}

// New constructs a Signer from a parsed azkeys:// URI.
func New(ctx context.Context, u *url.URL) (*Signer, error) {
	if u.Host == "" {
		return nil, errors.New("azkv: URI is missing the Managed HSM host")
	}
	pathTrimmed := strings.TrimPrefix(u.Path, "/keys/")
	if pathTrimmed == u.Path {
		return nil, errors.New("azkv: URI path must begin with /keys/")
	}
	parts := strings.SplitN(pathTrimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("azkv: URI must include /keys/<name>/<version>")
	}
	name, version := parts[0], parts[1]

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azkv: credential: %w", err)
	}
	client, err := azkeys.NewClient("https://"+u.Host, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("azkv: client: %w", err)
	}

	resp, err := client.GetKey(ctx, name, version, nil)
	if err != nil {
		return nil, fmt.Errorf("azkv: get key %s/%s: %w", name, version, err)
	}
	pub, alg, err := parsePublicKey(resp.Key)
	if err != nil {
		return nil, fmt.Errorf("azkv: parse public key: %w", err)
	}

	return &Signer{
		client:  client,
		name:    name,
		version: version,
		host:    u.Host,
		pub:     pub,
		alg:     alg,
	}, nil
}

// parsePublicKey converts an azkeys.JSONWebKey into a Go crypto.PublicKey plus
// a JWS algorithm name. Only ECDSA P-256 (ES256) and RSA (RS256) are
// supported in v1 of this objective.
func parsePublicKey(jwk *azkeys.JSONWebKey) (crypto.PublicKey, string, error) {
	if jwk == nil {
		return nil, "", errors.New("nil JSONWebKey")
	}
	if jwk.Kty == nil {
		return nil, "", errors.New("kty missing")
	}
	switch *jwk.Kty {
	case azkeys.KeyTypeECHSM, azkeys.KeyTypeEC:
		if jwk.Crv == nil {
			return nil, "", errors.New("ec curve missing")
		}
		if *jwk.Crv != azkeys.CurveNameP256 {
			return nil, "", fmt.Errorf("ec curve %q not supported (want P-256)", *jwk.Crv)
		}
		x := new(big.Int).SetBytes(jwk.X)
		y := new(big.Int).SetBytes(jwk.Y)
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, "ES256", nil
	default:
		return nil, "", fmt.Errorf("unsupported key type %q (want EC-HSM/EC)", *jwk.Kty)
	}
}

// Public returns the cached public key fetched at construction.
func (s *Signer) Public() crypto.PublicKey { return s.pub }

// Sign performs an Azure Managed HSM Sign operation. The digest must match
// the algorithm; for ES256 a 32-byte SHA-256 digest is expected.
func (s *Signer) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
	algName := azkeys.SignatureAlgorithmES256
	resp, err := s.client.Sign(context.Background(), s.name, s.version, azkeys.SignParameters{
		Algorithm: to.Ptr(algName),
		Value:     digest,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("azkv: sign: %w", err)
	}
	if resp.Result == nil {
		return nil, errors.New("azkv: empty signature result")
	}
	// Azure Managed HSM returns raw r||s for ES256 (per Microsoft docs):
	// https://learn.microsoft.com/en-us/rest/api/keyvault/keys/sign/sign
	// Caller is golang-jwt which expects exactly this shape.
	return resp.Result, nil
}

// KeyID returns the full Azure key URL (host + /keys/name/version) — safe to
// log at INFO level.
func (s *Signer) KeyID() string {
	return fmt.Sprintf("https://%s/keys/%s/%s", s.host, s.name, s.version)
}

// SigningAlgorithm returns the JWS alg detected at construction.
func (s *Signer) SigningAlgorithm() string { return s.alg }

// HealthCheck signs and verifies kms.HealthCheckPayload against the key. For
// ES256 the raw signature converts back through ECDSADERFromJWS so the verify
// step uses ecdsa.VerifyASN1.
func (s *Signer) HealthCheck(ctx context.Context) error {
	digest := sha256.Sum256(kms.HealthCheckPayload)
	sig, err := s.Sign(nil, digest[:], crypto.SHA256)
	if err != nil {
		return fmt.Errorf("azkv: healthcheck sign: %w", err)
	}
	switch s.alg {
	case "ES256":
		epub, ok := s.pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("azkv: public key not ECDSA (%T)", s.pub)
		}
		der, err := signature.ECDSADERFromJWS(sig)
		if err != nil {
			return fmt.Errorf("azkv: convert sig: %w", err)
		}
		if !ecdsa.VerifyASN1(epub, digest[:], der) {
			return errors.New("azkv: signature did not verify")
		}
		return nil
	default:
		return fmt.Errorf("azkv: verify path missing for algorithm %q", s.alg)
	}
}
