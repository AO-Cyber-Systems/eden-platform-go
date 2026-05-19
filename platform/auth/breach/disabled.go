package breach

import (
	"context"
)

// DisabledScreener is a no-op Screener. Real (final) implementation
// lands in Task 4; this stub permits Task 1 interface-compliance
// assertions to compile.
type DisabledScreener struct{}

// Check is a stub returning (false, 0, nil) until Task 4 lands.
func (s *DisabledScreener) Check(ctx context.Context, password string) (bool, int, error) {
	return false, 0, nil
}
