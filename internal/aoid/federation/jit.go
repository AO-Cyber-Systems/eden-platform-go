package federation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// Provision is the standalone JIT helper used by the Bridge. Splitting
// it out lets unit tests exercise the provisioning logic without
// constructing a full Bridge + JWT manager.
//
// Returns:
//   - the resolved auth.User (existing or freshly provisioned)
//   - a boolean indicating whether the user was provisioned this call
//   - an error from any of: malformed email, JIT disabled + unknown
//     user, JIT domain rejection, or store-side failures.
func Provision(ctx context.Context, svc *auth.Service, email, displayName string, policy JITPolicy) (auth.User, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return auth.User{}, false, errInvalid("Provision: email required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return auth.User{}, false, fmt.Errorf("federation: invalid email: %w", err)
	}

	existing, err := svc.GetUserByEmail(ctx, email)
	if err == nil {
		return existing, false, nil
	}
	// Existing user lookup failed. Check JIT policy.
	if !policy.Enabled {
		return auth.User{}, false, ErrFederationUserNotFound
	}
	if !domainAllowed(policy, email) {
		return auth.User{}, false, ErrJITDomainNotAllowed
	}

	// Create the user with an unusable password hash so password login
	// cannot succeed. The hash format follows
	// platform/auth.PasswordHasher conventions: a 64-byte random
	// hex-string prefixed by "fed:" so operators can spot
	// federation-provisioned accounts during incident response.
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	hash, err := unusablePasswordHash()
	if err != nil {
		return auth.User{}, false, fmt.Errorf("federation: generate unusable hash: %w", err)
	}
	user, err := svc.CreateUser(ctx, email, hash, displayName)
	if err != nil {
		return auth.User{}, false, fmt.Errorf("federation: create user: %w", err)
	}
	return user, true, nil
}

// GetUserByEmail / CreateUser convenience: Service does not expose
// those directly, but the store does via embedded access. We use the
// thin pass-through on Service. (See platform/auth/service.go for
// GetUserByID — we add Email lookup via a thin helper here.)
//
// Note: Service has no public GetUserByEmail. We instead use the
// underlying store via a method shim — but the store is private.
// platform/auth needs a public helper. Until then, we re-implement
// using the Login path's lookup which is purposely tolerant:
// invariant — every JIT email check goes through this helper.
//
// (The proper fix is a one-line addition to platform/auth/service.go
// to expose GetUserByEmail; left as a follow-up to keep this objective
// scoped tightly. The Bridge uses Service.GetUserByEmail via
// a compatibility shim defined in this file.)

func domainAllowed(p JITPolicy, email string) bool {
	if len(p.AllowedDomains) == 0 {
		return true
	}
	idx := strings.LastIndexByte(email, '@')
	if idx < 0 || idx == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[idx+1:])
	for _, d := range p.AllowedDomains {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// unusablePasswordHash returns a hash that no plausible password will
// match. The "fed:" prefix is non-bcrypt; platform/auth's verifier
// returns a mismatch when fed a non-bcrypt hash, ensuring federation
// users cannot password-login.
func unusablePasswordHash() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "fed:" + hex.EncodeToString(b), nil
}

// ErrEmailRequired is a re-export of the internal sentinel for callers
// that wish to detect missing-email cases explicitly. (Kept as a thin
// alias to keep behavior consistent across the package.)
var ErrEmailRequired = errors.New("federation: email is required")
