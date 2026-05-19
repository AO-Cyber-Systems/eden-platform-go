// Test list
//   - TestSentinelErrors_Breach — errors.Is identity for ErrScreenerUnavailable + ErrInvalidPrefix
//   - TestScreener_InterfaceCompliance — compile-time assertion that all three implementations satisfy Screener
//   - TestDisabledScreener_RoundTrip — DisabledScreener.Check never reports compromised, never errors
package breach

import (
	"context"
	"errors"
	"testing"
)

// TestSentinelErrors_Breach verifies sentinel errors satisfy errors.Is identity.
func TestSentinelErrors_Breach(t *testing.T) {
	if !errors.Is(ErrScreenerUnavailable, ErrScreenerUnavailable) {
		t.Fatal("ErrScreenerUnavailable is not identical to itself under errors.Is")
	}
	if !errors.Is(ErrInvalidPrefix, ErrInvalidPrefix) {
		t.Fatal("ErrInvalidPrefix is not identical to itself under errors.Is")
	}
	if errors.Is(ErrScreenerUnavailable, ErrInvalidPrefix) {
		t.Fatal("ErrScreenerUnavailable should not be identical to ErrInvalidPrefix")
	}
}

// TestScreener_InterfaceCompliance is a compile-time + run-time check that all
// three implementations satisfy the Screener interface.
func TestScreener_InterfaceCompliance(t *testing.T) {
	var _ Screener = (*HIBPScreener)(nil)
	var _ Screener = (*LocalListScreener)(nil)
	var _ Screener = (*DisabledScreener)(nil)
}

// TestDisabledScreener_RoundTrip — Check returns (false, 0, nil) for
// every input including known-compromised passwords and empty strings.
// This is the documented no-op contract for FedRAMP-High deployments
// where breach screening is intentionally not enforced.
func TestDisabledScreener_RoundTrip(t *testing.T) {
	s := NewDisabledScreener()
	for _, p := range []string{"", "password", "123456", "correct horse battery staple"} {
		compromised, count, err := s.Check(context.Background(), p)
		if err != nil {
			t.Fatalf("Check(%q) returned err: %v", p, err)
		}
		if compromised {
			t.Fatalf("Check(%q) reported compromised=true on disabled screener", p)
		}
		if count != 0 {
			t.Fatalf("Check(%q) reported count=%d on disabled screener", p, count)
		}
	}
}
