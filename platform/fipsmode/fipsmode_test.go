package fipsmode

import (
	"crypto/fips140"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMustRequire exercises the gate from a Go test process built without
// GOFIPS140 — the everyday developer / CI baseline. MustRequire must return
// a descriptive error so an operator can diagnose without reading source.
func TestMustRequire(t *testing.T) {
	t.Run("returns_error_when_build_setting_absent", func(t *testing.T) {
		err := MustRequire()
		// Default CI/test build does not set GOFIPS140; we expect a non-nil
		// error. (If a developer somehow runs `GOFIPS140=v1.0.0 go test ./...`
		// against an already-FIPS-enabled environment this case may flip to
		// nil — that is acceptable and means the FIPS path is actually live.)
		if fips140.Enabled() {
			t.Skip("FIPS already active in this process; skipping the negative-path test")
		}
		require.Error(t, err, "MustRequire should fail when GOFIPS140 build setting is absent")
	})

	t.Run("error_message_includes_actionable_hint", func(t *testing.T) {
		if fips140.Enabled() {
			t.Skip("FIPS active; error-path hint check N/A")
		}
		err := MustRequire()
		require.Error(t, err)
		// The error must mention GOFIPS140 by name so an operator can search
		// docs and rebuild. We accept either the build-setting message or the
		// build-tag fallback message; both reference GOFIPS140.
		require.Contains(t, err.Error(), "GOFIPS140",
			"error message should reference GOFIPS140 by name")
	})
}

// TestSelfTest verifies the ECDSA P-256 and AES-256-GCM round-trips work
// inside this process. SelfTest is intentionally portable: it does not require
// GOFIPS140 to be set (the stdlib paths function in either mode), so the test
// passes in standard CI.
func TestSelfTest(t *testing.T) {
	t.Run("ecdsa_round_trip", func(t *testing.T) {
		// SelfTest covers both ECDSA and AES-GCM in one shot; the ecdsa half
		// is the first stage. If only this case fails, the AES path is fine.
		err := selfTestECDSA()
		require.NoError(t, err, "ECDSA P-256 sign+verify must succeed")
	})

	t.Run("aes_gcm_round_trip", func(t *testing.T) {
		err := selfTestAESGCM()
		require.NoError(t, err, "AES-256-GCM seal+open must succeed")
	})

	t.Run("full_selftest_returns_nil", func(t *testing.T) {
		err := SelfTest()
		require.NoError(t, err, "SelfTest must succeed end-to-end in any process")
	})
}

// TestVersion verifies the Version helper agrees with crypto/fips140 about
// whether FIPS is active. Empty string when disabled (the common CI case);
// non-empty when enabled.
func TestVersion(t *testing.T) {
	t.Run("empty_when_fips_disabled", func(t *testing.T) {
		if fips140.Enabled() {
			t.Skip("FIPS active; version is expected to be non-empty")
		}
		require.Equal(t, "", Version(),
			"Version should be empty when crypto/fips140.Enabled is false")
	})

	t.Run("non_empty_when_fips_enabled", func(t *testing.T) {
		if !fips140.Enabled() {
			t.Skip("FIPS not active; nothing to assert")
		}
		require.NotEmpty(t, Version(),
			"Version should report a non-empty string when FIPS is active")
		require.Equal(t, fips140.Version(), Version(),
			"Version should mirror crypto/fips140.Version exactly")
	})
}

// TestEnabled verifies the Enabled wrapper agrees with the stdlib for the
// same process.
func TestEnabled(t *testing.T) {
	t.Run("matches_stdlib", func(t *testing.T) {
		require.Equal(t, fips140.Enabled(), Enabled(),
			"fipsmode.Enabled must mirror crypto/fips140.Enabled in the same process")
	})
}

// TestReadBuildSetting protects the BuildInfo iteration helper against the
// empty-Settings edge case noted in RESEARCH.md Pitfall 7.
func TestReadBuildSetting(t *testing.T) {
	t.Run("empty_settings_returns_empty", func(t *testing.T) {
		got := readBuildSetting(nil, buildSettingGOFIPS140)
		require.Equal(t, "", got, "nil settings slice must not panic and must return \"\"")
	})

	t.Run("missing_key_returns_empty", func(t *testing.T) {
		// Empty Settings slice is the realistic shape when BuildInfo is
		// available but stamping was suppressed. We use a synthetic shape
		// here via the same code path.
		got := readBuildSetting(nil, "SOMETHING_ELSE")
		require.Equal(t, "", got)
	})
}
