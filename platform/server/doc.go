// Package server provides the Eden platform Connect-RPC server primitives:
// router setup, HTTP middleware, health endpoints, and the canonical chain of
// unary interceptors that every Eden service composes into its handler graph.
//
// # Interceptor catalog
//
// NewAuthInterceptor    — validates JWT Bearer tokens and injects claims (legacy auth).
// NewRBACInterceptor    — enforces per-procedure permissions against the RBAC enforcer.
// NewAuditInterceptor   — emits audit events tagged with the calling actor.
// NewTenantScopeInterceptor — enforces that admin identities can only act on
// resources within their own tenant; super admins pass through.
//
// # Tenant scoping
//
// NewTenantScopeInterceptor enforces that an admin identified by
// NewAdminContextInterceptor (from platform/adminauth) can only operate on
// resources within their own tenant. Super admins pass through to any tenant.
// Tenant admins are rejected at the request boundary if the target tenant
// differs from their own binding.
//
// Interceptor ordering (required):
//
//	connect.WithInterceptors(
//	    adminauth.NewAdminContextInterceptor(resolver),  // sets AdminIdentity
//	    server.NewTenantScopeInterceptor(extractor),     // enforces tenant match
//	    server.NewAuditInterceptor(auditLogger),         // logs actor + decision
//	)
//
// The tenant_id is extracted from the request message body via a
// consumer-supplied TenantExtractor — NEVER from a request header (which can
// be spoofed by anything upstream of the mTLS terminator). The interceptor
// is pure: no DB, no I/O — tenant decisions are computed from
// (AdminIdentity, targetTenantID) only. The repository-layer tenant guards
// (see AOID account repo) catch cross-tenant QUERY bugs at the data layer.
package server
