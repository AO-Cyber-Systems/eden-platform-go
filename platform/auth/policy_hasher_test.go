//go:build !fips140v1.0 && !fips140v1.26

// Test list (TRD 03-01 Task 3 — default / non-FIPS build)
//
//   - TestNewPolicyHasher_NonFIPSReturnsArgon2id  — Hash() output starts with "$argon2id$"
//   - TestNewPolicyHasher_NonFIPSAlgoField        — returned hasher has algo == AlgoArgon2id
//   - TestNewPolicyHasher_VerifiesPBKDF2Hashes    — non-FIPS hasher still verifies FIPS-formatted hashes
//   - TestNewPolicyHasher_VerifiesArgon2idHashes  — round-trip with its own output
//
// The FIPS-build counterpart lives in policy_hasher_fips_test.go (selected
// by the fips140v* build tags). CI runs both: default build AND
// `GOFIPS140=v1.26.0 GODEBUG=fips140=on go test ./platform/auth/...`.

package auth

import (
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/fipsmode"
)

func TestNewPolicyHasher_NonFIPSReturnsArgon2id(t *testing.T) {
	// Sanity precondition: this test only makes sense if fipsmode reports off.
	if fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = true; this test file is gated for the non-FIPS build only")
	}
	h := NewPolicyHasher()
	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash err = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Errorf("non-FIPS NewPolicyHasher produced %q; want $argon2id$ prefix", encoded)
	}
}

func TestNewPolicyHasher_NonFIPSAlgoField(t *testing.T) {
	if fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = true; non-FIPS test")
	}
	h := NewPolicyHasher()
	if h.algo != AlgoArgon2id {
		t.Errorf("NewPolicyHasher().algo = %v; want AlgoArgon2id in non-FIPS build", h.algo)
	}
}

func TestNewPolicyHasher_VerifiesPBKDF2Hashes(t *testing.T) {
	// Cross-mode property: even when policy chose Argon2id, the returned
	// hasher must still verify PBKDF2-formatted rows produced by another
	// deployment running in FIPS mode. This is what makes a shared user
	// table portable across deployment modes.
	if fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = true; non-FIPS test")
	}
	policy := NewPolicyHasher()
	fips := NewFIPSPasswordHasher()

	pbkdf2Encoded, err := fips.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("fips.Hash err = %v", err)
	}
	ok, err := policy.Verify("correct horse battery staple", pbkdf2Encoded)
	if err != nil {
		t.Fatalf("policy.Verify err = %v", err)
	}
	if !ok {
		t.Errorf("non-FIPS policy hasher failed to verify a PBKDF2 hash")
	}
}

func TestNewPolicyHasher_VerifiesArgon2idHashes(t *testing.T) {
	if fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = true; non-FIPS test")
	}
	h := NewPolicyHasher()
	encoded, err := h.Hash("hunter2")
	if err != nil {
		t.Fatalf("Hash err = %v", err)
	}
	ok, err := h.Verify("hunter2", encoded)
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if !ok {
		t.Errorf("self round-trip failed")
	}
}
