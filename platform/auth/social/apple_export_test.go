// Package social_test provides external-package accessibility checks for
// exported symbols in platform/auth/social. This file is package social_test
// (note the _test suffix) so that the compile-time assignment below verifies
// the symbol is reachable from an EXTERNAL import path — the exact guarantee
// TRD 14-01 requires before TRD 14-05 can depend on it.
package social_test

import (
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth/social"
)

// TestGenerateAppleClientSecret_ExternalCallable verifies that
// GenerateAppleClientSecret is exported (capital G) and callable from an
// external package.  The function returns an error for a bad PEM, which is
// enough to confirm reachability without a real Apple .p8 key.
func TestGenerateAppleClientSecret_ExternalCallable(t *testing.T) {
	_, err := social.GenerateAppleClientSecret("not-a-pem", "TEAM", "SERVICES", "KEY")
	if err == nil {
		t.Fatal("expected error for malformed PEM, got nil — confirms function is callable")
	}
}
