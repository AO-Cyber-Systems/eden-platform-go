package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Algo identifies a password-hashing algorithm.
type Algo int

const (
	// AlgoArgon2id is the OWASP-recommended Argon2id algorithm. Default for
	// non-FIPS deployments because of its memory hardness.
	AlgoArgon2id Algo = iota
	// AlgoPBKDF2SHA256 is PBKDF2 with SHA-256 — NIST SP 800-63B Rev 4 §5.1.1.2
	// approved for FIPS-validated contexts (FIPS 186-2 / FIPS 198-1).
	AlgoPBKDF2SHA256
)

// Sentinel errors. Consumers can use errors.Is for stable identity.
var (
	// ErrMalformedHash is returned when an encoded hash string cannot be
	// parsed (missing leading $, wrong part count, undecodable base64,
	// non-numeric parameters, out-of-range iteration counts).
	ErrMalformedHash = errors.New("auth: malformed password hash")
	// ErrUnsupportedAlgorithm is returned when the algorithm prefix is not
	// one of "argon2id" or "pbkdf2-sha256". Future algorithms must be added
	// to the dispatch table in Verify before this error stops triggering.
	ErrUnsupportedAlgorithm = errors.New("auth: unsupported hash algorithm")
)

// PasswordHasher hashes and verifies passwords. The active algorithm
// (Argon2id default, PBKDF2-SHA256 in FIPS mode) is selected at construction;
// Verify auto-detects from the encoded prefix so a single hasher instance can
// verify hashes produced by either algorithm. This lets a single user-table
// column hold rows hashed under either algorithm — the algorithm tag travels
// inside the encoded string itself.
type PasswordHasher struct {
	algo Algo

	// Argon2id parameters (used when algo == AlgoArgon2id).
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen uint32

	// PBKDF2 parameters (used when algo == AlgoPBKDF2SHA256).
	// keyLen and saltLen above are reused.
	iters int
}

// NewPasswordHasher returns the OWASP-recommended Argon2id hasher
// (m=47104 KiB, t=1, p=1, salt=16B, key=32B).
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		algo:    AlgoArgon2id,
		time:    1,
		memory:  47104, // 46 MiB
		threads: 1,
		keyLen:  32,
		saltLen: 16,
	}
}

// Hash hashes the given password with the hasher's configured algorithm.
//
// Returns an encoded string in one of two formats depending on the hasher's
// algorithm:
//
//	Argon2id:       $argon2id$v=19$m=47104,t=1,p=1$<b64salt>$<b64hash>
//	PBKDF2-SHA256:  $pbkdf2-sha256$i=600000$<b64salt>$<b64hash>
//
// The encoded format carries the algorithm tag so Verify can dispatch
// without knowing the hasher's construction.
func (h *PasswordHasher) Hash(password string) (string, error) {
	switch h.algo {
	case AlgoArgon2id:
		return h.hashArgon2id(password)
	case AlgoPBKDF2SHA256:
		return h.hashPBKDF2(password)
	default:
		return "", fmt.Errorf("%w: algo=%d", ErrUnsupportedAlgorithm, int(h.algo))
	}
}

// Verify checks whether the provided password matches the encoded hash.
//
// Verify auto-detects the encoded algorithm by inspecting the leading
// "$<algo>$..." prefix and dispatches to the right verifier. This means a
// PasswordHasher constructed for one algorithm can still verify hashes
// produced by the other — a deployment that flips its hashing policy
// continues to verify pre-existing rows without re-hashing.
//
// Returns (false, nil) for a correctly-formatted hash with the wrong
// password (this is a normal authentication-failure outcome and must not be
// logged as an error). Returns (false, err) wrapping ErrMalformedHash or
// ErrUnsupportedAlgorithm for parser-level failures.
//
// Uses constant-time comparison to prevent timing attacks.
func (h *PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	if encodedHash == "" || !strings.HasPrefix(encodedHash, "$") {
		return false, fmt.Errorf("%w: missing leading '$' or empty input", ErrMalformedHash)
	}
	// parts[0] is "" (leading $); parts[1] is the algorithm tag.
	parts := strings.Split(encodedHash, "$")
	if len(parts) < 2 {
		return false, fmt.Errorf("%w: too few parts", ErrMalformedHash)
	}
	switch parts[1] {
	case "argon2id":
		return h.verifyArgon2id(password, parts)
	case "pbkdf2-sha256":
		return h.verifyPBKDF2(password, parts)
	default:
		return false, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, parts[1])
	}
}

// hashArgon2id keeps the historical encoding format verbatim so existing
// rows hashed by the pre-TRD-03-01 code continue to verify byte-for-byte.
func (h *PasswordHasher) hashArgon2id(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, h.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.threads, b64Salt, b64Hash), nil
}

// verifyArgon2id parses an $argon2id$... encoded hash and constant-time
// compares the recomputed key against the stored key.
//
// Expected layout (6 parts including the leading empty token):
//
//	parts[0] = ""             (leading $)
//	parts[1] = "argon2id"
//	parts[2] = "v=19"
//	parts[3] = "m=<mem>,t=<time>,p=<threads>"
//	parts[4] = <b64 salt>
//	parts[5] = <b64 key>
func (h *PasswordHasher) verifyArgon2id(password string, parts []string) (bool, error) {
	if len(parts) != 6 {
		return false, fmt.Errorf("%w: argon2id expected 6 parts, got %d", ErrMalformedHash, len(parts))
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("%w: parse version: %w", ErrMalformedHash, err)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("%w: parse params: %w", ErrMalformedHash, err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("%w: decode salt: %w", ErrMalformedHash, err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("%w: decode hash: %w", ErrMalformedHash, err)
	}

	computedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1, nil
}

// hashPBKDF2 / verifyPBKDF2 are implemented in password_fips.go (TRD 03-01
// Task 2). Until Task 2 lands, the dispatch returns ErrUnsupportedAlgorithm
// for pbkdf2-sha256 inputs via these placeholders. Task 2 removes them.
//
// NOTE: these placeholders are intentionally minimal and will be deleted
// when the real PBKDF2 hasher is added — the TRD requires that the PBKDF2
// code path live in password_fips.go, not here.
func (h *PasswordHasher) hashPBKDF2(string) (string, error) {
	return "", fmt.Errorf("%w: pbkdf2-sha256 not yet implemented (TRD 03-01 Task 2)", ErrUnsupportedAlgorithm)
}

func (h *PasswordHasher) verifyPBKDF2(string, []string) (bool, error) {
	return false, fmt.Errorf("%w: pbkdf2-sha256 not yet implemented (TRD 03-01 Task 2)", ErrUnsupportedAlgorithm)
}

