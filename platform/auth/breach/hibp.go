package breach

import (
	"context"
)

// HIBPScreener queries the HaveIBeenPwned PwnedPasswords k-anonymity API.
// Real implementation lands in Task 2; this file declares the type so
// Task 1 interface-compliance assertions compile.
type HIBPScreener struct{}

// Check is a stub returning (false, 0, nil) until Task 2 lands.
func (s *HIBPScreener) Check(ctx context.Context, password string) (bool, int, error) {
	return false, 0, nil
}
