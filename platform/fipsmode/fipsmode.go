package fipsmode

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/fips140"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// buildSettingGOFIPS140 is the runtime/debug.BuildInfo setting key the Go
// toolchain stamps into the binary when GOFIPS140 is supplied at build time.
const buildSettingGOFIPS140 = "GOFIPS140"

// MustRequire reports whether this binary is running in FIPS 140-3 mode and
// returns a descriptive error if not. All three of the following must hold:
//
//  1. The binary was compiled with GOFIPS140 set to a supported version (we
//     check the build setting via runtime/debug.ReadBuildInfo).
//  2. One of the supported fips140v* build tags fired (buildTagPresent==true).
//  3. crypto/fips140.Enabled() reports true at runtime (GODEBUG=fips140=on|only).
//
// Call MustRequire as the FIRST step of service boot, before any cryptographic
// operation, signer construction, or KMS handshake. The function intentionally
// does NOT panic — it returns an error so the caller decides whether to log,
// wrap, or abort. There is no init() side-effect; gating must be explicit.
func MustRequire() error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("fipsmode: runtime/debug.ReadBuildInfo unavailable; cannot verify GOFIPS140 build setting")
	}

	gofips := readBuildSetting(info.Settings, buildSettingGOFIPS140)
	if gofips == "" || gofips == "off" {
		return fmt.Errorf("fipsmode: GOFIPS140 build setting required (got %q); rebuild with GOFIPS140=v1.0.0 or v1.26.0", gofips)
	}

	if !buildTagPresent {
		return errors.New("fipsmode: FIPS build tag not detected; binary was not built with GOFIPS140")
	}

	if !fips140.Enabled() {
		return errors.New("fipsmode: runtime FIPS mode not active; set GODEBUG=fips140=on or =only")
	}

	return nil
}

// readBuildSetting returns the value of the named build setting, or "" if the
// setting is absent. The function is defensive against an empty Settings slice
// (some unusual build paths produce an empty BuildInfo).
func readBuildSetting(settings []debug.BuildSetting, key string) string {
	if len(settings) == 0 {
		return ""
	}
	for _, s := range settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// SelfTest exercises the FIPS-approved ECDSA P-256 sign+verify and AES-256-GCM
// seal+open round-trips using stdlib-only paths. It is intentionally narrow:
// the validated FIPS module already runs CMVP-mandated power-on tests in its
// own init(); SelfTest is the service-level "the crypto primitives I need to
// issue and validate tokens actually work in this process" check.
//
// SelfTest succeeds in any process, regardless of whether GOFIPS140 was set —
// the stdlib paths used here work both inside and outside the FIPS module.
// That is intentional: SelfTest is a binary-level wiring check, not a FIPS
// gate. The FIPS gate lives in MustRequire.
func SelfTest() error {
	if err := selfTestECDSA(); err != nil {
		return fmt.Errorf("fipsmode: selftest: ecdsa: %w", err)
	}
	if err := selfTestAESGCM(); err != nil {
		return fmt.Errorf("fipsmode: selftest: aes-gcm: %w", err)
	}
	slog.Info("fipsmode: self-test passed", "module_version", Version())
	return nil
}

func selfTestECDSA() error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	msg := []byte("fipsmode self-test payload")
	digest := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if !ecdsa.VerifyASN1(&priv.PublicKey, digest[:], sig) {
		return errors.New("verify failed")
	}
	return nil
}

func selfTestAESGCM() error {
	key := make([]byte, 32) // AES-256
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("rand key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("rand nonce: %w", err)
	}
	plaintext := []byte("fipsmode self-test payload")
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	decrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if string(decrypted) != string(plaintext) {
		return errors.New("decrypted plaintext does not match")
	}
	return nil
}

// Version returns the active FIPS module version string as reported by
// crypto/fips140.Version(). Returns "" if FIPS is not active in this process.
func Version() string {
	if !fips140.Enabled() {
		return ""
	}
	return fips140.Version()
}

// Enabled reports whether crypto/fips140 considers FIPS mode active in this
// process. It is a thin wrapper so consumers can read state without importing
// crypto/fips140 directly.
func Enabled() bool {
	return fips140.Enabled()
}
