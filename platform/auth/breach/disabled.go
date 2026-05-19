package breach

import "context"

// DisabledScreener is a no-op Screener. Use in deployments where the
// breach-corpus check is intentionally not enforced — typically
// FedRAMP-High environments that cannot egress to api.pwnedpasswords.com
// AND have a deployment policy forbidding bundled corpus material.
//
// Defense-in-depth still applies via Argon2id (or PBKDF2-SHA256 in FIPS
// mode), length floor, complexity rules, and MFA — DisabledScreener
// simply opts the breach-corpus control out without rejecting the
// password verification flow.
//
// Safe for concurrent use (stateless).
type DisabledScreener struct{}

// NewDisabledScreener constructs a DisabledScreener.
func NewDisabledScreener() *DisabledScreener { return &DisabledScreener{} }

// Check always returns (false, 0, nil). This is the documented contract;
// callers that want screening MUST select a different implementation.
func (s *DisabledScreener) Check(ctx context.Context, password string) (bool, int, error) {
	return false, 0, nil
}
