package kms

import (
	"context"
	"crypto"
	"fmt"
	"net/url"
)

// HealthCheckPayload is the fixed canary the HealthCheck implementations should
// sign+verify. Exported so provider tests can mirror the production payload.
var HealthCheckPayload = []byte("aoid.kms.health-check.v1")

// KMSSigner is the canonical signing-key surface for Eden services.
//
// It embeds crypto.Signer so values plug directly into:
//   - github.com/golang-jwt/jwt/v5 (any SigningMethodES256/RS256 path)
//   - crypto/tls server config (Certificate.PrivateKey)
//   - crypto/x509.CreateCertificate (priv parameter)
//
// Implementations MUST be safe for concurrent use by multiple goroutines.
type KMSSigner interface {
	crypto.Signer

	// KeyID returns a stable, log-safe identifier for the underlying key
	// (AWS ARN, Azure key URL, PKCS#11 CKA_LABEL). It does NOT include
	// credentials and is safe to log at INFO level.
	KeyID() string

	// SigningAlgorithm returns the JWS algorithm name implemented by this
	// signer ("ES256", "RS256", etc.). Used to populate JWT `alg` headers
	// and JWKS `alg` fields.
	SigningAlgorithm() string

	// HealthCheck performs a real Sign+Verify round-trip on
	// HealthCheckPayload. It catches IAM/role configurations that permit
	// GetPublicKey but deny Sign, and it confirms the algorithm advertised
	// by SigningAlgorithm() matches what the underlying provider actually
	// performs.
	HealthCheck(ctx context.Context) error
}

// providerFactory is the New(ctx, *url.URL) signature every provider exposes.
// Providers register themselves with Open via Register so that kms.go does not
// import provider subpackages directly — preventing a circular import and
// keeping the public surface independent of provider build constraints (e.g.
// PKCS#11 needs cgo).
type providerFactory func(ctx context.Context, u *url.URL) (KMSSigner, error)

// providerOptionsFactory is the registry entry for providers that need
// caller-supplied options beyond the URI (currently only softkey). The opts
// argument is type-asserted by the provider to its own Options struct; passing
// a wrong type yields a descriptive error from the provider, NOT a panic.
type providerOptionsFactory func(ctx context.Context, u *url.URL, opts any) (KMSSigner, error)

// registry is populated by provider subpackages in their init() functions.
// Reads/writes happen at package init time only, so no synchronization is
// needed here.
var registry = map[string]providerFactory{}

// optionsRegistry parallels registry for providers requiring Options. Schemes
// may appear in BOTH maps: the bare Open path returns an error directing the
// caller to OpenWithOptions; the OpenWithOptions path dispatches here.
var optionsRegistry = map[string]providerOptionsFactory{}

// Register associates a URI scheme with a provider factory. Called from each
// provider subpackage's init(); panics if the scheme is registered twice
// (programmer error). Public so out-of-tree providers (e.g. a customer-supplied
// HSM driver) can plug in.
func Register(scheme string, f providerFactory) {
	if _, dup := registry[scheme]; dup {
		panic(fmt.Sprintf("kms: scheme %q registered twice", scheme))
	}
	registry[scheme] = f
}

// RegisterOptions associates a URI scheme with a providerOptionsFactory.
// Called by providers (currently only softkey) whose construction requires
// out-of-band data (a Resolver callback + KMSCipher) that does not fit in the
// URI. Panics if the scheme is registered twice via RegisterOptions.
//
// A scheme MAY be registered with BOTH Register and RegisterOptions — the bare
// Register entry is used to return a descriptive "use OpenWithOptions" error
// when callers reach Open with a scheme that requires options.
func RegisterOptions(scheme string, f providerOptionsFactory) {
	if _, dup := optionsRegistry[scheme]; dup {
		panic(fmt.Sprintf("kms: scheme %q registered twice in optionsRegistry", scheme))
	}
	optionsRegistry[scheme] = f
}

// Open parses providerURI and dispatches to the registered provider for the
// URI scheme. Returns a descriptive error when:
//
//   - providerURI fails to parse (net/url.Parse error)
//   - no provider is registered for the scheme
//   - the provider's New() returns an error (auth failure, missing key, etc.)
//
// Open does NOT log the URI — Azure URIs in particular include tenant
// identifiers and Managed HSM names that we treat as sensitive.
func Open(ctx context.Context, providerURI string) (KMSSigner, error) {
	u, err := url.Parse(providerURI)
	if err != nil {
		return nil, fmt.Errorf("kms: parse provider URI: %w", err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("kms: provider URI is missing a scheme (want awskms, azkeys, or pkcs11)")
	}
	factory, ok := registry[u.Scheme]
	if !ok {
		return nil, fmt.Errorf("kms: unsupported scheme %q (want awskms, azkeys, or pkcs11)", u.Scheme)
	}
	return factory(ctx, u)
}

// OpenWithOptions parses providerURI and dispatches to a providerOptionsFactory
// when one is registered for the scheme. When no options-factory is registered,
// OpenWithOptions falls through to the bare Open path — preserving back-compat
// for awskms/azkv/pkcs11 callers that may switch to OpenWithOptions wholesale
// without breaking.
//
// The opts argument is provider-opaque: each provider type-asserts to its own
// Options struct (e.g. softkey.Options). Passing nil to a scheme that requires
// options surfaces an error from the provider.
func OpenWithOptions(ctx context.Context, providerURI string, opts any) (KMSSigner, error) {
	u, err := url.Parse(providerURI)
	if err != nil {
		return nil, fmt.Errorf("kms: parse provider URI: %w", err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("kms: provider URI is missing a scheme (want awskms, azkeys, pkcs11, or softkey)")
	}
	if of, ok := optionsRegistry[u.Scheme]; ok {
		return of(ctx, u, opts)
	}
	// Fall through to bare Open — preserves back-compat for providers that
	// don't need Options (awskms, azkv, pkcs11).
	factory, ok := registry[u.Scheme]
	if !ok {
		return nil, fmt.Errorf("kms: unsupported scheme %q (want awskms, azkeys, pkcs11, or softkey)", u.Scheme)
	}
	return factory(ctx, u)
}

// RegisteredSchemes returns the list of registered URI schemes. Useful for
// diagnostic output during boot when a misconfigured URI is rejected.
func RegisteredSchemes() []string {
	seen := make(map[string]struct{}, len(registry)+len(optionsRegistry))
	for s := range registry {
		seen[s] = struct{}{}
	}
	for s := range optionsRegistry {
		seen[s] = struct{}{}
	}
	schemes := make([]string, 0, len(seen))
	for s := range seen {
		schemes = append(schemes, s)
	}
	return schemes
}
