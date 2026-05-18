package server

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/adminauth"
	"github.com/google/uuid"
)

// TenantExtractor pulls the target tenant_id out of a Connect-RPC request.
// Consumers supply one function per service — typically a small type-switch
// over the service's request message types.
//
// Contract:
//
//	Returns (uuid.Nil, ErrNoTargetTenant) when the request has no tenant_id
//	field populated. Returns (uuid.Nil, err) for parse errors (malformed UUID).
//	Returns (validUUID, nil) on success.
type TenantExtractor func(req connect.AnyRequest) (uuid.UUID, error)

// Sentinel errors.
var (
	// ErrNoTargetTenant signals the request did not carry a tenant_id
	// (or the extractor returned uuid.Nil with nil error). Mapped to
	// CodeInvalidArgument for non-super admins.
	ErrNoTargetTenant = errors.New("server: request has no target tenant_id")

	// ErrCrossTenantAccess signals a tenant admin tried to operate on
	// a tenant other than their own. Mapped to CodePermissionDenied.
	// Error message contains "cross-tenant" for log-grep workflows.
	ErrCrossTenantAccess = errors.New("server: cross-tenant access denied")
)

// NewTenantScopeInterceptor returns a Connect unary interceptor that enforces
// per-tenant admin access. Place this interceptor AFTER NewAdminContextInterceptor
// in the chain — the AdminIdentity must already be in context.
//
// Decision tree:
//
//	AdminIdentity missing         → CodeUnauthenticated (admin interceptor not chained)
//	IsSuperAdmin                  → pass (any target tenant, including uuid.Nil)
//	extractor returns err         → CodeInvalidArgument (contains "tenant_id" in msg)
//	target tenant == uuid.Nil     → CodeInvalidArgument (ErrNoTargetTenant)
//	target tenant == admin tenant → pass
//	target tenant != admin tenant → CodePermissionDenied (ErrCrossTenantAccess)
//
// The interceptor is pure (no DB, no I/O) — tenant decisions are computed from
// (AdminIdentity, targetTenantID) only. Repository-layer tenant guards
// (see AOID account repo) catch cross-tenant QUERY bugs at the data layer;
// this interceptor catches cross-tenant ACTOR violations at the request boundary.
//
// Wire order (required):
//
//	connect.WithInterceptors(
//	    adminauth.NewAdminContextInterceptor(resolver),  // sets AdminIdentity
//	    server.NewTenantScopeInterceptor(extractor),     // enforces tenant match
//	    server.NewAuditInterceptor(auditLogger),         // logs actor + decision
//	)
func NewTenantScopeInterceptor(extractor TenantExtractor) connect.UnaryInterceptorFunc {
	if extractor == nil {
		panic("server: NewTenantScopeInterceptor requires a non-nil TenantExtractor")
	}
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			admin := adminauth.AdminIdentityFromContext(ctx)
			if admin == nil {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("server: tenant_scope interceptor requires admin identity in context — chain NewAdminContextInterceptor first"))
			}
			if admin.IsSuperAdmin {
				return next(ctx, req)
			}
			target, err := extractor(req)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument,
					fmt.Errorf("server: tenant_id extract: %w", err))
			}
			if target == uuid.Nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, ErrNoTargetTenant)
			}
			if !admin.HasTenantAccess(target) {
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("%w: admin tenant=%s target tenant=%s",
						ErrCrossTenantAccess, admin.TenantID, target))
			}
			return next(ctx, req)
		}
	})
}
