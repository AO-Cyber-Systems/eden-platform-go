//go:build fips140v1.0 || fips140v1.26

// Test list (TRD 03-01 Task 3 — FIPS build counterpart)
//
//   - TestNewPolicyHasher_FIPSReturnsPBKDF2   — Hash() output starts with "$pbkdf2-sha256$" when fipsmode.Enabled() is true
//   - TestNewPolicyHasher_FIPSAlgoField       — returned hasher has algo == AlgoPBKDF2SHA256
//
// This file is built only when the Go toolchain stamps the fips140v* tag
// (i.e. when GOFIPS140 is set at build time). Use:
//
//   GOFIPS140=v1.26.0 GODEBUG=fips140=on go test ./platform/auth/...
//
// At runtime the test additionally skips itself if crypto/fips140.Enabled()
// reports false — this can happen when the build tag fired but GODEBUG was
// not set, in which case the FIPS path is dormant and the assertion would
// be incorrect.

package auth

import (
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/fipsmode"
)

func TestNewPolicyHasher_FIPSReturnsPBKDF2(t *testing.T) {
	if !fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = false; FIPS-tagged build but runtime gate not active (set GODEBUG=fips140=on)")
	}
	h := NewPolicyHasher()
	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash err = %v", err)
	}
	if !strings.HasPrefix(encoded, "$pbkdf2-sha256$") {
		t.Errorf("FIPS NewPolicyHasher produced %q; want $pbkdf2-sha256$ prefix", encoded)
	}
}

func TestNewPolicyHasher_FIPSAlgoField(t *testing.T) {
	if !fipsmode.Enabled() {
		t.Skipf("fipsmode.Enabled() = false; FIPS-tagged build but runtime gate not active")
	}
	h := NewPolicyHasher()
	if h.algo != AlgoPBKDF2SHA256 {
		t.Errorf("NewPolicyHasher().algo = %v; want AlgoPBKDF2SHA256 in FIPS build", h.algo)
	}
}
