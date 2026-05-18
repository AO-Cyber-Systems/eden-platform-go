package adminauth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// AdminIdentity describes an admin actor making a Connect-RPC call.
// Constructed by an AdminIdentityResolver from the mTLS peer CN
// (placeholder) or from JWT claims (post-Obj-3).
type AdminIdentity struct {
	// SubjectCN is the mTLS client cert CommonName for the placeholder
	// implementation, or the JWT "sub" claim for the JWT implementation.
	SubjectCN string

	// TenantID is the tenant this admin is scoped to. uuid.Nil for super admins.
	TenantID uuid.UUID

	// IsSuperAdmin allows operations across all tenants.
	IsSuperAdmin bool

	// Roles holds the system role names this admin is bound to
	// (e.g. ["super_admin"], ["tenant_admin"], ["tenant_viewer"]).
	// Consumers use HasRole to check; downstream interceptors may
	// also inspect this list directly.
	Roles []string
}

// IsZero returns true for the zero-value AdminIdentity (unconfigured).
func (a AdminIdentity) IsZero() bool {
	return a.SubjectCN == "" && a.TenantID == uuid.Nil && !a.IsSuperAdmin && len(a.Roles) == 0
}

// HasTenantAccess returns true if this admin is allowed to operate on
// resources scoped to targetTenantID. Super admins have access to every
// tenant (including uuid.Nil). Non-super admins only have access to their
// own TenantID. A non-super admin with uuid.Nil TenantID is a misconfigured
// identity and is denied by default.
func (a AdminIdentity) HasTenantAccess(targetTenantID uuid.UUID) bool {
	if a.IsSuperAdmin {
		return true
	}
	return a.TenantID != uuid.Nil && a.TenantID == targetTenantID
}

// HasRole returns true if the admin's Roles list contains the named role.
func (a AdminIdentity) HasRole(name string) bool {
	for _, r := range a.Roles {
		if r == name {
			return true
		}
	}
	return false
}

// AdminIdentityResolver is implemented by host services to map a CN
// (placeholder) or JWT subject (post-Obj-3) to an AdminIdentity.
// Returns ErrUnknownAdmin when the CN parses cleanly but doesn't match
// a provisioned admin; any other error is treated as infrastructure failure.
type AdminIdentityResolver interface {
	ResolveFromCN(ctx context.Context, cn string) (AdminIdentity, error)
}

// Sentinel errors returned by the interceptor + resolver contract.
var (
	// ErrMissingCN signals that the request did not present a verified
	// client cert CN (mTLS not enforced, or chain didn't validate).
	ErrMissingCN = errors.New("adminauth: missing mTLS peer CN")

	// ErrUnknownAdmin signals that the CN parsed but doesn't match a
	// provisioned admin actor. Authenticated, but not authorized as admin.
	ErrUnknownAdmin = errors.New("adminauth: unknown admin CN")
)

// contextKey is unexported so external packages can't collide.
type contextKey int

const adminKey contextKey = iota

// WithAdminIdentity stores the identity in ctx. The implementation stashes
// a pointer so callers can distinguish "not set" from "set to zero value".
func WithAdminIdentity(ctx context.Context, a AdminIdentity) context.Context {
	return context.WithValue(ctx, adminKey, &a)
}

// AdminIdentityFromContext returns the identity stored by WithAdminIdentity,
// or nil if no identity is present. Consumers MUST handle nil — the
// interceptor populates context only on successful resolution.
func AdminIdentityFromContext(ctx context.Context) *AdminIdentity {
	a, _ := ctx.Value(adminKey).(*AdminIdentity)
	return a
}
