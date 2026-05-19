package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// pbkdf2IterFloor is the sanity-check minimum iteration count Verify will
// accept on parse. NIST SP 800-63B Rev 4 §5.1.1.2 requires ≥600,000 for
// new hashes; the floor here is intentionally permissive (1,000) so that
// older, pre-policy-bump rows still verify while still rejecting absurdly
// low values that signal a malformed or attacker-controlled input.
const pbkdf2IterFloor = 1000

// NewFIPSPasswordHasher returns a PasswordHasher configured for PBKDF2-SHA256
// per NIST SP 800-63B Rev 4 §5.1.1.2: 600,000 iterations, salt = 16 bytes,
// derived key = 32 bytes.
//
// This is the FIPS-validated path. Consumers running in FIPS mode
// (GOFIPS140 build setting AND fips140=on GODEBUG, see platform/fipsmode)
// should construct this hasher instead of the Argon2id default returned by
// NewPasswordHasher.
//
// The returned PasswordHasher's Verify method auto-detects encoded-hash
// algorithm, so it can still verify Argon2id-formatted hashes produced by
// a non-FIPS deployment. That property is what lets the user table be
// shared across deployment modes without re-hashing.
func NewFIPSPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		algo:    AlgoPBKDF2SHA256,
		iters:   600_000,
		keyLen:  32,
		saltLen: 16,
	}
}

// hashPBKDF2 produces a $pbkdf2-sha256$i=<iters>$<b64salt>$<b64hash> string.
// Salt comes from crypto/rand; failure to read the OS RNG is surfaced as an
// error (consumers should treat this as fatal — there is no recovery from
// a missing-randomness condition).
func (h *PasswordHasher) hashPBKDF2(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := pbkdf2.Key([]byte(password), salt, h.iters, int(h.keyLen), sha256.New)

	return fmt.Sprintf("$pbkdf2-sha256$i=%d$%s$%s",
		h.iters,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// verifyPBKDF2 parses a $pbkdf2-sha256$... encoded string and constant-time
// compares the recomputed key against the stored key.
//
// Expected layout (5 parts including the leading empty token):
//
//	parts[0] = ""               (leading $)
//	parts[1] = "pbkdf2-sha256"
//	parts[2] = "i=<iters>"
//	parts[3] = <b64 salt>
//	parts[4] = <b64 key>
//
// Returns (false, nil) for a correctly-formatted hash that does not match
// the password (normal authentication failure). Returns (false, err)
// wrapping ErrMalformedHash for parser errors.
func (h *PasswordHasher) verifyPBKDF2(password string, parts []string) (bool, error) {
	if len(parts) != 5 {
		return false, fmt.Errorf("%w: pbkdf2-sha256 expected 5 parts, got %d", ErrMalformedHash, len(parts))
	}

	if !strings.HasPrefix(parts[2], "i=") {
		return false, fmt.Errorf("%w: missing iter param prefix", ErrMalformedHash)
	}
	var iters int
	if _, err := fmt.Sscanf(parts[2], "i=%d", &iters); err != nil {
		return false, fmt.Errorf("%w: parse iters: %w", ErrMalformedHash, err)
	}
	if iters < pbkdf2IterFloor {
		return false, fmt.Errorf("%w: iterations too low: %d (floor=%d)", ErrMalformedHash, iters, pbkdf2IterFloor)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, fmt.Errorf("%w: decode salt: %w", ErrMalformedHash, err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("%w: decode hash: %w", ErrMalformedHash, err)
	}

	computed := pbkdf2.Key([]byte(password), salt, iters, len(expected), sha256.New)
	return subtle.ConstantTimeCompare(computed, expected) == 1, nil
}
