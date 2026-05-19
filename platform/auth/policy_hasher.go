package auth

import "github.com/aocybersystems/eden-platform-go/platform/fipsmode"

// NewPolicyHasher returns a PasswordHasher chosen according to the runtime
// FIPS-mode flag exposed by platform/fipsmode. When fipsmode.Enabled() is
// true (the binary was built with GOFIPS140 and GODEBUG fips140=on|only is
// set), the returned hasher uses PBKDF2-SHA256 per NIST SP 800-63B Rev 4
// §5.1.1.2. Otherwise it returns the OWASP-recommended Argon2id hasher.
//
// The choice is made at construction time. The returned PasswordHasher
// will hash NEW passwords with the chosen algorithm but its Verify method
// dispatches on the encoded-hash prefix, so it can still verify rows
// produced under the other algorithm — a deployment that flips its
// hashing mode does NOT need to re-hash existing rows.
//
// Consumers SHOULD use this constructor instead of NewPasswordHasher or
// NewFIPSPasswordHasher when they want deployment-mode-aware crypto
// selection without re-implementing the build-tag check.
func NewPolicyHasher() *PasswordHasher {
	if fipsmode.Enabled() {
		return NewFIPSPasswordHasher()
	}
	return NewPasswordHasher()
}
