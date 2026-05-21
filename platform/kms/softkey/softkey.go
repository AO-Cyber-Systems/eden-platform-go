package softkey

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/aocybersystems/eden-platform-go/platform/kms"
)

// Resolver is the caller-supplied callback that turns a softkey URI into a
// (algorithm, wrapped-PKCS#8-blob) tuple. AOID wires this to a Postgres query
// against aoid.jwks_keys; tests use an in-memory map.
//
// keyID is the canonical URI form, e.g. "softkey://aoid/keys/<uuid-v4>".
// Returning a non-nil error with no further annotations is fine — softkey
// wraps the error with the softkey: prefix before surfacing it.
type Resolver func(ctx context.Context, keyID string) (alg string, wrappedPKCS8 []byte, err error)

// Options is the dependency bundle softkey.New requires. Both fields are
// mandatory; either being nil yields a descriptive construction error. The
// Resolver determines where the wrapped private key lives; the WrapCipher
// determines how it is unwrapped. Eden owns neither — both are pluggable so
// AOID can supply its AES-256-GCM wrap key and Postgres lookup without
// leaking AOID-specific symbols into Eden.
type Options struct {
	Resolver   Resolver
	WrapCipher kms.KMSCipher
}

// Signer is the kms.KMSSigner implementation backed by a process-resident
// private key. After New unwraps the PKCS#8 blob, the private key lives in
// the Go heap; there is no separate "session" or "connection" lifecycle to
// manage. Concurrent Sign calls are safe — Go's crypto/ecdsa and crypto/rsa
// signing primitives are goroutine-safe under documented usage.
type Signer struct {
	priv  crypto.Signer
	keyID string
	alg   string
}

// New constructs a Signer by resolving the URI through opts.Resolver and
// unwrapping the returned blob through opts.WrapCipher. Returns:
//
//   - an error when u is nil or its scheme is not "softkey"
//   - an error when Options.Resolver or Options.WrapCipher is nil
//   - the resolver's error (wrapped with the softkey: prefix) when lookup fails
//   - the cipher's error when Decrypt fails (typically wrap-key mismatch or
//     ciphertext tampering)
//   - "softkey: parse pkcs8" when the unwrapped blob is not a valid PKCS#8
//   - "softkey: alg mismatch" when the row's alg disagrees with the key type
//   - "softkey: unsupported key type %T" for non-ECDSA/RSA keys
//
// On success, the returned *Signer's KeyID is the URI exactly as constructed
// (u.String()), so log lines and JWKS kid fields can use it without further
// transformation.
func New(ctx context.Context, u *url.URL, opts Options) (*Signer, error) {
	if u == nil {
		return nil, errors.New("softkey: nil URI")
	}
	if u.Scheme != "softkey" {
		return nil, fmt.Errorf("softkey: invalid URI scheme %q (want softkey)", u.Scheme)
	}
	if opts.Resolver == nil || opts.WrapCipher == nil {
		return nil, errors.New("softkey: Options.Resolver and Options.WrapCipher are required")
	}
	keyID := u.String()
	alg, wrapped, err := opts.Resolver(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("softkey: resolver: %w", err)
	}
	if len(wrapped) == 0 {
		return nil, errors.New("softkey: resolver returned empty wrapped blob")
	}
	pkcs8, err := opts.WrapCipher.Decrypt(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("softkey: unwrap: %w", err)
	}
	anyKey, err := x509.ParsePKCS8PrivateKey(pkcs8)
	if err != nil {
		return nil, fmt.Errorf("softkey: parse pkcs8: %w", err)
	}
	var priv crypto.Signer
	switch k := anyKey.(type) {
	case *ecdsa.PrivateKey:
		if alg != "ES256" {
			return nil, fmt.Errorf("softkey: alg mismatch: row says %s but key is ECDSA (want ES256)", alg)
		}
		priv = k
	case *rsa.PrivateKey:
		if alg != "RS256" {
			return nil, fmt.Errorf("softkey: alg mismatch: row says %s but key is RSA (want RS256)", alg)
		}
		priv = k
	default:
		return nil, fmt.Errorf("softkey: unsupported key type %T", anyKey)
	}
	return &Signer{priv: priv, keyID: keyID, alg: alg}, nil
}

// Sign produces a signature over digest using the configured algorithm:
//
//   - ES256: ecdsa.SignASN1 — returns ASN.1 DER. The platform/auth/kmssigner
//     JWS adapter converts to raw r||s; callers building JWS bytes by hand
//     must do the same conversion.
//   - RS256: rsa.SignPKCS1v15 — returns the JWS wire format directly.
//
// The opts argument is the standard crypto.SignerOpts; callers passing
// crypto.SHA256 align with what the digest was produced with (the typical
// path). Implementations of crypto/ecdsa and crypto/rsa do not consume opts
// beyond the hash family, so passing nil is also accepted at the Go API
// level — but callers MUST hash with SHA-256 before invoking Sign because
// both algorithms ride on that digest size.
func (s *Signer) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	switch p := s.priv.(type) {
	case *ecdsa.PrivateKey:
		return ecdsa.SignASN1(rand, p, digest)
	case *rsa.PrivateKey:
		// Hash family resolution: prefer the caller-supplied opts.HashFunc()
		// but fall back to SHA-256 (the only hash this provider claims to
		// support today via RS256). crypto/rsa.SignPKCS1v15 requires the
		// hash family to match what produced the digest.
		hash := crypto.SHA256
		if opts != nil {
			hash = opts.HashFunc()
		}
		return rsa.SignPKCS1v15(rand, p, hash, digest)
	}
	return nil, fmt.Errorf("softkey: impossible alg %s (priv type %T)", s.alg, s.priv)
}

// Public returns the cached public key associated with the private signing
// key. Useful for callers who need to assemble a JWK on the fly without
// re-reading the row.
func (s *Signer) Public() crypto.PublicKey { return s.priv.Public() }

// KeyID returns the canonical softkey URI (e.g.
// "softkey://aoid/keys/<uuid>"). AOID uses this verbatim as the JWS "kid"
// header and as the JWKS "kid" field, so verifiers can correlate by exact
// match.
func (s *Signer) KeyID() string { return s.keyID }

// SigningAlgorithm returns "ES256" or "RS256", matching what the resolver
// asserted at New time.
func (s *Signer) SigningAlgorithm() string { return s.alg }

// HealthCheck performs a real in-process Sign+Verify round-trip over
// kms.HealthCheckPayload. This catches:
//
//   - alg mismatch between the row metadata and the actual key material
//   - corrupted public key (private key sign produces output that cannot
//     verify against the stored public)
//   - a downstream library swap that breaks DER encoding
//
// Returns nil on success; wrapped errors naming the failed step on failure.
func (s *Signer) HealthCheck(_ context.Context) error {
	digest := sha256.Sum256(kms.HealthCheckPayload)
	sig, err := s.Sign(nil, digest[:], crypto.SHA256)
	if err != nil {
		return fmt.Errorf("softkey: healthcheck sign (%s): %w", s.alg, err)
	}
	switch s.alg {
	case "ES256":
		epub, ok := s.priv.Public().(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("softkey: healthcheck verify: public key is %T, expected *ecdsa.PublicKey", s.priv.Public())
		}
		if !ecdsa.VerifyASN1(epub, digest[:], sig) {
			return errors.New("softkey: healthcheck verify (ES256): signature did not verify")
		}
		return nil
	case "RS256":
		rpub, ok := s.priv.Public().(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("softkey: healthcheck verify: public key is %T, expected *rsa.PublicKey", s.priv.Public())
		}
		if err := rsa.VerifyPKCS1v15(rpub, crypto.SHA256, digest[:], sig); err != nil {
			return fmt.Errorf("softkey: healthcheck verify (RS256): %w", err)
		}
		return nil
	}
	return fmt.Errorf("softkey: healthcheck: unknown alg %q", s.alg)
}

// init registers the softkey scheme with both kms registries:
//
//   - The bare provider factory returns an error directing callers to
//     OpenWithOptions. This is intentional: a softkey:// URI alone cannot
//     produce a signer because the Resolver + WrapCipher dependencies live
//     outside the URI namespace.
//   - The options factory dispatches to softkey.New after type-asserting
//     opts to softkey.Options. A wrong type yields a descriptive error
//     (NOT a panic).
//
// AOID's boot calls kms.OpenWithOptions("softkey://...", softkey.Options{...})
// uniformly with the awskms/azkeys/pkcs11 paths.
func init() {
	kms.Register("softkey", func(ctx context.Context, u *url.URL) (kms.KMSSigner, error) {
		return nil, errors.New("softkey: bare kms.Open is not supported — call kms.OpenWithOptions(ctx, uri, softkey.Options{Resolver, WrapCipher}) instead")
	})
	kms.RegisterOptions("softkey", func(ctx context.Context, u *url.URL, opts any) (kms.KMSSigner, error) {
		so, ok := opts.(Options)
		if !ok {
			return nil, fmt.Errorf("softkey: opts must be softkey.Options, got %T", opts)
		}
		return New(ctx, u, so)
	})
}
