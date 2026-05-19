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

// tenantScopeOpts is the internal config carried by functional options on
// NewTenantScopeInterceptor. The zero value (dedicatedTenant == uuid.Nil)
// preserves the legacy super_admin-bypass behavior for backward
// compatibility with existing Eden consumers.
type tenantScopeOpts struct {
	// dedicatedTenant, when non-nil, configures the interceptor for
	// dedicated-mode operation (AOID physical-isolation tier — TRD 10-04).
	// In dedicated mode, EVERY request — including super_admin — must
	// target this tenant or have no target (chassis-wide endpoint).
	dedicatedTenant uuid.UUID
}

// TenantScopeOption configures NewTenantScopeInterceptor via functional
// options. The zero-opt call (no options) preserves the legacy behavior so
// existing Eden consumers (e.g., AOSentry) don't need to update.
type TenantScopeOption func(*tenantScopeOpts)

// WithDedicatedTenant configures the interceptor for dedicated-mode
// operation. In dedicated mode (AOID physical-isolation tier — TRD 10-04):
//
//   - Every request — INCLUDING super_admin — must target this tenant or
//     have no target (chassis-wide endpoint like /.well-known/jwks.json).
//   - Requests targeting any other tenant return CodePermissionDenied with
//     an error message containing the substring "dedicated-mode" for
//     operator log-grep workflows.
//   - Requests with target == uuid.Nil (chassis-wide intent) pass through;
//     the route/handler is responsible for tenant-resolution. Endpoints
//     that should be denied chassis-wide in dedicated mode are an
//     endpoint-level concern (rare — AOID currently has none).
//
// The dedicated tenant is set ONCE at boot from AOID_DEDICATED_TENANT_SLUG
// (see internal/tenancy.ResolveDeploymentMode). It is intentionally NOT a
// runtime feature flag — a hot-reload table would let a DB-write attacker
// disable dedicated mode.
func WithDedicatedTenant(tenantID uuid.UUID) TenantScopeOption {
	return func(o *tenantScopeOpts) { o.dedicatedTenant = tenantID }
}

// NewTenantScopeInterceptor returns a Connect unary interceptor that enforces
// per-tenant admin access. Place this interceptor AFTER NewAdminContextInterceptor
// in the chain — the AdminIdentity must already be in context.
//
// Decision tree (default mode, no options):
//
//	AdminIdentity missing         → CodeUnauthenticated (admin interceptor not chained)
//	IsSuperAdmin                  → pass (any target tenant, including uuid.Nil)
//	extractor returns err         → CodeInvalidArgument (contains "tenant_id" in msg)
//	target tenant == uuid.Nil     → CodeInvalidArgument (ErrNoTargetTenant)
//	target tenant == admin tenant → pass
//	target tenant != admin tenant → CodePermissionDenied (ErrCrossTenantAccess)
//
// Decision tree (dedicated mode, WithDedicatedTenant(id) set — id != uuid.Nil):
//
//	AdminIdentity missing         → CodeUnauthenticated (admin interceptor not chained)
//	extractor returns err         → CodeInvalidArgument (contains "tenant_id" in msg)
//	target tenant == uuid.Nil     → pass (chassis-wide intent)
//	target tenant == dedicated id → pass
//	target tenant != dedicated id → CodePermissionDenied (msg contains "dedicated-mode")
//
// Note that dedicated mode is STRICTER than non-dedicated mode: even
// super_admin requests are denied when target tenant != dedicated id. The
// AOID physical-isolation tier requires this stricter enforcement so the
// dedicated deployment cannot serve any tenant other than its dedicated
// one — see TRD 10-04 / RUNBOOK.md.
//
// The interceptor is pure (no DB, no I/O) — tenant decisions are computed from
// (AdminIdentity, targetTenantID, dedicatedTenantID) only. Repository-layer
// tenant guards (see AOID account repo) catch cross-tenant QUERY bugs at the
// data layer; this interceptor catches cross-tenant ACTOR violations at the
// request boundary.
//
// Wire order (required):
//
//	connect.WithInterceptors(
//	    adminauth.NewAdminContextInterceptor(resolver),  // sets AdminIdentity
//	    server.NewTenantScopeInterceptor(extractor, server.WithDedicatedTenant(id)),
//	    server.NewAuditInterceptor(auditLogger),         // logs actor + decision
//	)
//
// Backward compatibility: NewTenantScopeInterceptor(extractor) with no
// options preserves the legacy behavior — existing Eden consumers
// (AOSentry, AOAudit) don't need to update.
func NewTenantScopeInterceptor(extractor TenantExtractor, opts ...TenantScopeOption) connect.UnaryInterceptorFunc {
	if extractor == nil {
		panic("server: NewTenantScopeInterceptor requires a non-nil TenantExtractor")
	}
	cfg := tenantScopeOpts{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			admin := adminauth.AdminIdentityFromContext(ctx)
			if admin == nil {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("server: tenant_scope interceptor requires admin identity in context — chain NewAdminContextInterceptor first"))
			}
			// Dedicated mode: every request — including super_admin —
			// must target the dedicated tenant or have no target.
			if cfg.dedicatedTenant != uuid.Nil {
				target, err := extractor(req)
				if err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument,
						fmt.Errorf("server: tenant_id extract: %w", err))
				}
				if target == uuid.Nil {
					// Chassis-wide endpoint (e.g., health, .well-known
					// JWKS). Route/handler resolves tenant from the
					// dedicated config. Pass.
					return next(ctx, req)
				}
				if target != cfg.dedicatedTenant {
					return nil, connect.NewError(connect.CodePermissionDenied,
						fmt.Errorf("server: dedicated-mode: target tenant %s != dedicated %s",
							target, cfg.dedicatedTenant))
				}
				return next(ctx, req)
			}
			// Non-dedicated path (legacy behavior, unchanged).
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
