package breach

import (
	"context"
)

// LocalListScreener checks against an embedded list of common passwords.
// Real implementation lands in Task 3; this file declares the type so
// Task 1 interface-compliance assertions compile.
type LocalListScreener struct{}

// Check is a stub returning (false, 0, nil) until Task 3 lands.
func (s *LocalListScreener) Check(ctx context.Context, password string) (bool, int, error) {
	return false, 0, nil
}
