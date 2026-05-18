// Package adminauth supplies admin actor identity propagation for Eden
// platform Connect-RPC services that use mTLS.
//
// Surface:
//
//	AdminIdentity                    – value type carrying SubjectCN, TenantID,
//	                                    IsSuperAdmin, Roles.
//	AdminIdentityResolver            – consumer-implemented interface that
//	                                    maps a CN to an AdminIdentity.
//	NewAdminContextInterceptor       – Connect unary interceptor that wires
//	                                    the resolver into the request pipeline.
//	WithTLSConnectionState           – http.Handler middleware that stashes
//	                                    *tls.ConnectionState in request context
//	                                    BEFORE Connect routing.
//	WithAdminIdentity / FromContext  – context helpers for downstream code.
//
// Mode: placeholder (mTLS CN → identity).
// Replaces: planned JWT-claim → identity resolver (Obj 3 of AOID; equivalent
// upstream timing for AOEdge admin plane, AOSentry console, etc.).
//
// CN convention (enforced by the AOID resolver implementation, NOT this
// package — recorded here so future consumers know the contract):
//
//	"aoid-superadmin"                    → IsSuperAdmin=true, TenantID=uuid.Nil
//	"aoid-admin-<tenant_slug>"           → IsSuperAdmin=false, TenantID resolved
//	                                       by slug lookup against the tenant store
//
// Wire-up pattern (consumer-side):
//
//	mux := http.NewServeMux()
//	mux.Handle(platformv1connect.NewAccountAdminServiceHandler(
//	    handler,
//	    connect.WithInterceptors(
//	        adminauth.NewAdminContextInterceptor(myResolver),
//	        server.NewTenantScopeInterceptor(extractTenantFromRequest),
//	    ),
//	))
//	srv := &http.Server{ Handler: adminauth.WithTLSConnectionState(mux), ... }
//
// The TLSConnectionState wrapper is REQUIRED — Connect-RPC's req.Peer() does
// not expose the verified TLS chain (see Pitfall 6 in 02-RESEARCH.md).
package adminauth
