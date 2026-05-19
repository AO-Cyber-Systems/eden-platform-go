// Test list
//   - TestSentinelErrors_Breach — errors.Is identity for ErrScreenerUnavailable + ErrInvalidPrefix
//   - TestScreener_InterfaceCompliance — compile-time assertion that all three implementations satisfy Screener
package breach

import (
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
