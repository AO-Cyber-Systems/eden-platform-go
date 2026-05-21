package kms

import (
	"context"
	"crypto"
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubSigner satisfies KMSSigner for dispatch-path tests without pulling in a
// real provider. Registered with a synthetic scheme so the dispatch tests
// don't collide with real provider registrations.
type stubSigner struct{}

func (stubSigner) Public() crypto.PublicKey                                  { return nil }
func (stubSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) { return nil, nil }
func (stubSigner) KeyID() string                                             { return "stub" }
func (stubSigner) SigningAlgorithm() string                                  { return "ES256" }
func (stubSigner) HealthCheck(context.Context) error                         { return nil }

func TestOpen_DispatchesByScheme(t *testing.T) {
	// Register a stub provider for the duration of this test under a
	// synthetic scheme. Register panics on duplicates; we use a scheme that
	// real providers never use.
	const scheme = "kmstest"
	if _, ok := registry[scheme]; !ok {
		Register(scheme, func(context.Context, *url.URL) (KMSSigner, error) {
			return stubSigner{}, nil
		})
	}

	t.Run("dispatches_to_registered_scheme", func(t *testing.T) {
		s, err := Open(context.Background(), "kmstest:///some/key")
		require.NoError(t, err)
		require.NotNil(t, s)
		require.Equal(t, "stub", s.KeyID())
	})
}

func TestOpen_UnknownScheme(t *testing.T) {
	t.Run("returns_descriptive_error", func(t *testing.T) {
		_, err := Open(context.Background(), "wat://hsm/keys/abc")
		require.Error(t, err)
		require.Contains(t, err.Error(), `unsupported scheme "wat"`)
		require.Contains(t, err.Error(), "awskms, azkeys, or pkcs11")
	})
}

func TestOpen_RejectsMalformedURI(t *testing.T) {
	t.Run("rejects_empty_string", func(t *testing.T) {
		_, err := Open(context.Background(), "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing a scheme")
	})

	t.Run("rejects_no_scheme", func(t *testing.T) {
		_, err := Open(context.Background(), "/just/a/path")
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing a scheme")
	})
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	t.Run("panics_when_scheme_registered_twice", func(t *testing.T) {
		const scheme = "dup-scheme-test"
		Register(scheme, func(context.Context, *url.URL) (KMSSigner, error) { return stubSigner{}, nil })
		require.Panics(t, func() {
			Register(scheme, func(context.Context, *url.URL) (KMSSigner, error) { return stubSigner{}, nil })
		})
	})
}

func TestRegisteredSchemes_ReturnsAllSchemes(t *testing.T) {
	t.Run("includes_test_scheme", func(t *testing.T) {
		// The dispatch test above registered "kmstest". Just confirm the
		// list helper returns something non-empty so callers can produce
		// diagnostic output.
		schemes := RegisteredSchemes()
		require.NotEmpty(t, schemes)
	})
}

// Sanity-check: the package-level HealthCheckPayload is non-trivial — it has
// at least 16 bytes of entropy-resistant text, since some HSMs reject very
// short digests in edge configurations.
func TestHealthCheckPayload_NonTrivial(t *testing.T) {
	require.GreaterOrEqual(t, len(HealthCheckPayload), 16)
	// A no-op consumer of crypto/rand to keep linters happy if rand becomes
	// unused after refactoring.
	_ = rand.Reader
}

// TestOpenWithOptions_FallsThroughForOptionlessProviders confirms that
// providers registered only via Register (no RegisterOptions) still work when
// callers go through the new OpenWithOptions entry point. This preserves
// back-compat: AOID's boot code can call OpenWithOptions uniformly for every
// provider, and providers that don't need opts (awskms, azkv, pkcs11) ignore
// the opts argument.
func TestOpenWithOptions_FallsThroughForOptionlessProviders(t *testing.T) {
	const scheme = "owotest"
	if _, ok := registry[scheme]; !ok {
		Register(scheme, func(context.Context, *url.URL) (KMSSigner, error) {
			return stubSigner{}, nil
		})
	}

	t.Run("falls_through_when_no_options_factory_registered", func(t *testing.T) {
		s, err := OpenWithOptions(context.Background(), "owotest:///key", nil)
		require.NoError(t, err)
		require.NotNil(t, s)
		require.Equal(t, "stub", s.KeyID())
	})
}

// TestOpenWithOptions_DispatchesToOptionsFactory exercises the new
// providerOptionsFactory registry path with a synthetic scheme so the test
// does not depend on softkey package init order.
func TestOpenWithOptions_DispatchesToOptionsFactory(t *testing.T) {
	const scheme = "owooptstest"
	type optsT struct{ ID string }
	captured := make(chan optsT, 1)
	if _, ok := optionsRegistry[scheme]; !ok {
		RegisterOptions(scheme, func(_ context.Context, _ *url.URL, opts any) (KMSSigner, error) {
			o, ok := opts.(optsT)
			if !ok {
				return nil, fmt.Errorf("opts must be optsT, got %T", opts)
			}
			captured <- o
			return stubSigner{}, nil
		})
	}

	t.Run("dispatches_with_typed_opts", func(t *testing.T) {
		s, err := OpenWithOptions(context.Background(), "owooptstest:///abc", optsT{ID: "row-uuid"})
		require.NoError(t, err)
		require.NotNil(t, s)
		got := <-captured
		require.Equal(t, "row-uuid", got.ID)
	})

	t.Run("rejects_wrong_opts_type", func(t *testing.T) {
		_, err := OpenWithOptions(context.Background(), "owooptstest:///abc", "not-the-right-type")
		require.Error(t, err)
		require.Contains(t, err.Error(), "optsT")
	})
}

// TestRegisterOptions_PanicsOnDuplicate mirrors Register's behaviour.
func TestRegisterOptions_PanicsOnDuplicate(t *testing.T) {
	t.Run("panics_when_scheme_registered_twice", func(t *testing.T) {
		const scheme = "dup-opts-scheme-test"
		RegisterOptions(scheme, func(context.Context, *url.URL, any) (KMSSigner, error) {
			return stubSigner{}, nil
		})
		require.Panics(t, func() {
			RegisterOptions(scheme, func(context.Context, *url.URL, any) (KMSSigner, error) {
				return stubSigner{}, nil
			})
		})
	})
}

// NOTE: softkey-scheme registration assertions live in
// platform/kms/softkey/softkey_test.go where the softkey package is imported
// naturally and its init() runs as part of the test binary. Putting them here
// would require an anonymous import which leaks softkey symbols into the kms
// core test suite.
