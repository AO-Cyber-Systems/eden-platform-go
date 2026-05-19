package secrethasher

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	edenfips "github.com/aocybersystems/eden-platform-go/platform/fipsmode"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// Algorithm parameters. These match OWASP 2024 + NIST SP 800-132 / FIPS 140-3
// IG D.B guidance respectively. They must NOT be tuned in-place; a parameter
// bump warrants a new TRD + a hash-format-version constant for forward
// migration. Verify reads parameters from the encoded string at call time so
// hashes minted under earlier parameter sets continue to verify byte-for-byte.
const (
	argon2idTime    uint32 = 1
	argon2idMemKiB  uint32 = 47104
	argon2idThreads uint8  = 1
	argon2idKeyLen  uint32 = 32

	pbkdf2Iter   = 600000
	pbkdf2KeyLen = 32

	saltLen = 16

	prefixArgon2id = "$argon2id$"
	prefixPBKDF2   = "$pbkdf2-sha256$"
)

// Sentinel errors. Consumers use errors.Is for stable identity.
var (
	// ErrUnknownAlgorithm is returned when the encoded string begins with a
	// well-formed $<algo>$ prefix that is not one of the two supported
	// algorithms (argon2id or pbkdf2-sha256). Future algorithms must be
	// added to Verify's dispatch table before this sentinel stops firing.
	ErrUnknownAlgorithm = errors.New("secrethasher: unknown algorithm")

	// ErrInvalidFormat is returned when the encoded string is malformed:
	// empty, missing the leading '$', wrong number of '$'-separated
	// fields, unparseable numeric parameters, or undecodable base64
	// salt/hash fields. Verify NEVER panics on malformed input.
	ErrInvalidFormat = errors.New("secrethasher: invalid encoded format")

	// ErrAlgorithmMismatch is returned by strict-mode verification (not
	// yet exposed in v1) when the encoded algorithm does not match the
	// current FIPS posture. Reserved for a future VerifyStrict API; the
	// default Verify is permissive so a FIPS-mode flip does not
	// invalidate already-stored hashes.
	ErrAlgorithmMismatch = errors.New("secrethasher: encoded algorithm does not match required FIPS posture")
)

// fipsMode is the runtime FIPS-mode accessor; it is overridable at package
// level so tests can flip the branch without rebuilding with build tags.
// Production code MUST NOT mutate this variable. The default value reads
// the real platform/fipsmode state.
var fipsMode = realFipsMode

func realFipsMode() bool { return edenfips.Enabled() }

// Hash returns an algorithm-tagged encoded string for the given secret.
//
// The active algorithm is selected at call time via fipsMode():
//
//	fipsMode() == false  -> Argon2id (OWASP 2024 defaults)
//	fipsMode() == true   -> PBKDF2-SHA256 (NIST SP 800-132 / FIPS 140-3 IG D.B)
//
// Encoded shapes:
//
//	$argon2id$v=19$m=47104,t=1,p=1$<b64salt>$<b64hash>
//	$pbkdf2-sha256$i=600000$<b64salt>$<b64hash>
//
// Salt is 16 random bytes from crypto/rand. A salt-read failure is the only
// non-deterministic error path and is treated as fatal by callers.
func Hash(secret string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("secrethasher: salt: %w", err)
	}

	encB64 := base64.RawStdEncoding.EncodeToString

	if fipsMode() {
		h := pbkdf2.Key([]byte(secret), salt, pbkdf2Iter, pbkdf2KeyLen, sha256.New)
		return fmt.Sprintf("$pbkdf2-sha256$i=%d$%s$%s",
			pbkdf2Iter, encB64(salt), encB64(h)), nil
	}

	h := argon2.IDKey([]byte(secret), salt,
		argon2idTime, argon2idMemKiB, argon2idThreads, argon2idKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2idMemKiB, argon2idTime, argon2idThreads,
		encB64(salt), encB64(h)), nil
}

// Verify reports whether `secret` produces the same hash as the one carried
// in `encoded`. The algorithm + parameters are read from `encoded` itself;
// the current fipsMode() value is NOT consulted on the Verify path. This
// "permissive" behavior is intentional: a deployment that flips
// non-FIPS -> FIPS (or back) continues to verify already-stored hashes
// without invalidating any row.
//
// Returns (false, nil) for a correctly-shaped encoded string whose stored
// hash does not match — that is a normal authentication failure and must
// not be logged as an error.
//
// Returns (false, ErrInvalidFormat) for shape/parse errors and
// (false, ErrUnknownAlgorithm) for a recognizable $<algo>$ prefix that is
// not one of the supported algorithms.
//
// The final byte comparison uses crypto/subtle.ConstantTimeCompare so the
// function does not leak timing information about how many bytes matched.
func Verify(secret, encoded string) (bool, error) {
	if encoded == "" || !strings.HasPrefix(encoded, "$") {
		return false, ErrInvalidFormat
	}

	switch {
	case strings.HasPrefix(encoded, prefixArgon2id):
		return verifyArgon2id(secret, encoded)
	case strings.HasPrefix(encoded, prefixPBKDF2):
		return verifyPBKDF2(secret, encoded)
	default:
		return false, ErrUnknownAlgorithm
	}
}

// verifyArgon2id parses an $argon2id$v=<v>$m=<m>,t=<t>,p=<p>$<salt>$<hash>
// string and constant-time-compares the recomputed Argon2id key.
//
// Expected layout (6 parts including the leading empty token):
//
//	parts[0] = ""             (leading '$')
//	parts[1] = "argon2id"
//	parts[2] = "v=<version>"
//	parts[3] = "m=<mem>,t=<time>,p=<threads>"
//	parts[4] = <b64 salt>
//	parts[5] = <b64 hash>
//
// Parameters m/t/p are taken from the encoded string (NOT the package
// constants) so older hashes minted under different parameters still
// verify. The keyLen used to re-derive is set to len(decoded hash) which
// preserves the original derivation length.
func verifyArgon2id(secret, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false, ErrInvalidFormat
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidFormat
	}

	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, ErrInvalidFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidFormat
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidFormat
	}

	computed := argon2.IDKey([]byte(secret), salt, t, m, p, uint32(len(expected)))
	return subtle.ConstantTimeCompare(computed, expected) == 1, nil
}

// verifyPBKDF2 parses a $pbkdf2-sha256$i=<iter>$<salt>$<hash> string and
// constant-time-compares the recomputed PBKDF2-SHA256 key.
//
// Expected layout (5 parts including the leading empty token):
//
//	parts[0] = ""               (leading '$')
//	parts[1] = "pbkdf2-sha256"
//	parts[2] = "i=<iter>"
//	parts[3] = <b64 salt>
//	parts[4] = <b64 hash>
//
// Iteration count is taken from the encoded string (NOT the package
// constant) so older hashes minted at a lower count still verify. The
// keyLen used to re-derive equals len(decoded hash).
func verifyPBKDF2(secret, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false, ErrInvalidFormat
	}

	var iter int
	if _, err := fmt.Sscanf(parts[2], "i=%d", &iter); err != nil {
		return false, ErrInvalidFormat
	}
	if iter <= 0 {
		return false, ErrInvalidFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, ErrInvalidFormat
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidFormat
	}

	computed := pbkdf2.Key([]byte(secret), salt, iter, len(expected), sha256.New)
	return subtle.ConstantTimeCompare(computed, expected) == 1, nil
}
