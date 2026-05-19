package secrethasher

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// Hand-built secret fixtures. NO LLM-generated test data — these are
// chosen literals only.
const (
	secretA = "correct horse battery staple"
	secretB = "Tr0ub4dor&3"
	secretC = "" // empty-secret edge case
)

// argon2idRegex matches the canonical Argon2id encoded shape:
//
//	$argon2id$v=19$m=47104,t=1,p=1$<22-char b64 salt (16B raw)>$<43-char b64 hash (32B raw)>
//
// 16 raw bytes → ceil(16*4/3) = 22 b64 chars (no padding); 32 raw bytes → 43 b64 chars.
var argon2idRegex = regexp.MustCompile(`^\$argon2id\$v=19\$m=47104,t=1,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)

// pbkdf2Regex matches the canonical PBKDF2-SHA256 encoded shape:
//
//	$pbkdf2-sha256$i=600000$<22-char b64 salt>$<43-char b64 hash>
var pbkdf2Regex = regexp.MustCompile(`^\$pbkdf2-sha256\$i=600000\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)

// withFipsMode swaps the package-level fipsMode hook for the duration of a
// test. The returned function restores the original value and MUST be
// deferred immediately.
func withFipsMode(t *testing.T, enabled bool) func() {
	t.Helper()
	prev := fipsMode
	fipsMode = func() bool { return enabled }
	return func() { fipsMode = prev }
}

// ---------- Argon2id round-trip ----------

func TestHashArgon2idRoundTrip(t *testing.T) {
	defer withFipsMode(t, false)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("expected $argon2id$ prefix, got: %q", encoded)
	}

	ok, err := Verify(secretA, encoded)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned false for correct secret")
	}
}

func TestArgon2idVerifyRejectsWrongSecret(t *testing.T) {
	defer withFipsMode(t, false)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	ok, err := Verify(secretB, encoded)
	if err != nil {
		t.Fatalf("Verify returned error for wrong-secret case (should be nil): %v", err)
	}
	if ok {
		t.Fatal("Verify returned true for wrong secret")
	}
}

// ---------- PBKDF2-SHA256 round-trip ----------

func TestHashPBKDF2RoundTrip(t *testing.T) {
	defer withFipsMode(t, true)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(encoded, "$pbkdf2-sha256$i=600000$") {
		t.Fatalf("expected $pbkdf2-sha256$i=600000$ prefix, got: %q", encoded)
	}

	ok, err := Verify(secretA, encoded)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned false for correct secret")
	}
}

func TestPBKDF2VerifyRejectsWrongSecret(t *testing.T) {
	defer withFipsMode(t, true)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	ok, err := Verify(secretB, encoded)
	if err != nil {
		t.Fatalf("Verify returned error for wrong-secret case (should be nil): %v", err)
	}
	if ok {
		t.Fatal("Verify returned true for wrong secret")
	}
}

// ---------- Cross-mode verify (FIPS-flip semantics) ----------

func TestCrossModeVerifyArgon2idUnderFIPS(t *testing.T) {
	// Mint under non-FIPS.
	restore := withFipsMode(t, false)
	encoded, err := Hash(secretA)
	restore()
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("expected $argon2id$ prefix on non-FIPS hash, got: %q", encoded)
	}

	// Verify under FIPS-on — permissive mode should succeed.
	defer withFipsMode(t, true)()
	ok, err := Verify(secretA, encoded)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !ok {
		t.Fatal("argon2id hash should still verify under FIPS-on (permissive mode)")
	}
}

func TestCrossModeVerifyPBKDF2UnderNonFIPS(t *testing.T) {
	// Mint under FIPS.
	restore := withFipsMode(t, true)
	encoded, err := Hash(secretA)
	restore()
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(encoded, "$pbkdf2-sha256$") {
		t.Fatalf("expected $pbkdf2-sha256$ prefix on FIPS hash, got: %q", encoded)
	}

	// Verify under FIPS-off — permissive mode should succeed.
	defer withFipsMode(t, false)()
	ok, err := Verify(secretA, encoded)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !ok {
		t.Fatal("pbkdf2 hash should still verify under FIPS-off (permissive mode)")
	}
}

// ---------- Error sentinels ----------

func TestErrUnknownAlgorithm(t *testing.T) {
	// $bcrypt$... has a recognizable $-prefix but is not one of our algorithms.
	ok, err := Verify(secretA, "$bcrypt$10$abcdefghijklmnopqrstuv")
	if ok {
		t.Fatal("Verify returned true for unknown algorithm")
	}
	if !errors.Is(err, ErrUnknownAlgorithm) {
		t.Fatalf("expected ErrUnknownAlgorithm, got: %v", err)
	}
}

func TestErrInvalidFormatNoLeadingDollar(t *testing.T) {
	ok, err := Verify(secretA, "not a hash")
	if ok {
		t.Fatal("Verify returned true for malformed input")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatEmptyString(t *testing.T) {
	ok, err := Verify(secretA, "")
	if ok {
		t.Fatal("Verify returned true for empty encoded string")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatArgon2idWrongPartCount(t *testing.T) {
	// Argon2id with too few `$`-separated fields.
	ok, err := Verify(secretA, "$argon2id$wrong$count$")
	if ok {
		t.Fatal("Verify returned true for malformed Argon2id input")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatArgon2idBadVersion(t *testing.T) {
	// Properly-shaped 6 parts but version field is unparseable.
	ok, err := Verify(secretA, "$argon2id$notaversion$m=47104,t=1,p=1$YWFhYWFhYWFhYWFhYWFhYQ$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh")
	if ok {
		t.Fatal("Verify returned true for malformed version")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatArgon2idBadParams(t *testing.T) {
	// Properly-shaped 6 parts but params field is unparseable.
	ok, err := Verify(secretA, "$argon2id$v=19$notparams$YWFhYWFhYWFhYWFhYWFhYQ$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh")
	if ok {
		t.Fatal("Verify returned true for malformed params")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatArgon2idBadSaltB64(t *testing.T) {
	// Properly-shaped 6 parts but salt field is not valid base64.
	ok, err := Verify(secretA, "$argon2id$v=19$m=47104,t=1,p=1$!!!notb64!!!$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh")
	if ok {
		t.Fatal("Verify returned true for bad base64 salt")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatPBKDF2WrongPartCount(t *testing.T) {
	// Too few `$`-separated fields for pbkdf2-sha256.
	ok, err := Verify(secretA, "$pbkdf2-sha256$i=600000$only2parts")
	if ok {
		t.Fatal("Verify returned true for malformed PBKDF2 input")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatPBKDF2BadIter(t *testing.T) {
	// Iter field unparseable.
	ok, err := Verify(secretA, "$pbkdf2-sha256$i=notanumber$YWFhYWFhYWFhYWFhYWFhYQ$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh")
	if ok {
		t.Fatal("Verify returned true for bad iter")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestErrInvalidFormatPBKDF2NonPositiveIter(t *testing.T) {
	// Zero iter must be rejected as malformed.
	ok, err := Verify(secretA, "$pbkdf2-sha256$i=0$YWFhYWFhYWFhYWFhYWFhYQ$YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh")
	if ok {
		t.Fatal("Verify returned true for non-positive iter")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got: %v", err)
	}
}

// ---------- Encoded format assertions ----------

func TestArgon2idEncodedFormat(t *testing.T) {
	defer withFipsMode(t, false)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !argon2idRegex.MatchString(encoded) {
		t.Fatalf("Argon2id encoded format mismatch:\n  got: %q\n  want regex: %s", encoded, argon2idRegex)
	}
	// Reject base64 padding.
	if strings.Contains(encoded, "=") {
		t.Fatalf("encoded string must not contain '=' padding: %q", encoded)
	}
}

func TestPBKDF2EncodedFormat(t *testing.T) {
	defer withFipsMode(t, true)()

	encoded, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !pbkdf2Regex.MatchString(encoded) {
		t.Fatalf("PBKDF2 encoded format mismatch:\n  got: %q\n  want regex: %s", encoded, pbkdf2Regex)
	}
	if strings.Contains(encoded, "=") {
		t.Fatalf("encoded string must not contain '=' padding: %q", encoded)
	}
}

// ---------- Salt randomness ----------

func TestArgon2idSaltUniqueness(t *testing.T) {
	defer withFipsMode(t, false)()

	h1, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash#1 returned error: %v", err)
	}
	h2, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash#2 returned error: %v", err)
	}
	if h1 == h2 {
		t.Fatalf("Hash returned identical encoded strings for the same secret — salts collided:\n  %q", h1)
	}
}

func TestPBKDF2SaltUniqueness(t *testing.T) {
	defer withFipsMode(t, true)()

	h1, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash#1 returned error: %v", err)
	}
	h2, err := Hash(secretA)
	if err != nil {
		t.Fatalf("Hash#2 returned error: %v", err)
	}
	if h1 == h2 {
		t.Fatalf("Hash returned identical encoded strings for the same secret — salts collided:\n  %q", h1)
	}
}

// ---------- Empty-secret round-trip ----------

func TestEmptySecretRoundTripArgon2id(t *testing.T) {
	defer withFipsMode(t, false)()

	encoded, err := Hash(secretC)
	if err != nil {
		t.Fatalf("Hash empty-secret returned error: %v", err)
	}
	ok, err := Verify(secretC, encoded)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !ok {
		t.Fatal("empty-secret round-trip failed")
	}
	ok, err = Verify(secretA, encoded)
	if err != nil {
		t.Fatalf("Verify returned error for wrong-secret case: %v", err)
	}
	if ok {
		t.Fatal("Verify returned true comparing non-empty secret to empty-secret hash")
	}
}

// ---------- Benchmarks ----------

func BenchmarkHashArgon2id(b *testing.B) {
	prev := fipsMode
	fipsMode = func() bool { return false }
	defer func() { fipsMode = prev }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Hash(secretA); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashPBKDF2(b *testing.B) {
	prev := fipsMode
	fipsMode = func() bool { return true }
	defer func() { fipsMode = prev }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Hash(secretA); err != nil {
			b.Fatal(err)
		}
	}
}
