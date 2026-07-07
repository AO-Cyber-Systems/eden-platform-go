// router.go -- TRD 140-09. Route-map wiring for ExperienceService.
//
// RegisterExperienceHandlers registers the ExperienceService Connect handler on
// the SAME mux the integration test exercises (memory: webhook routing bugs are
// invisible to handler-only tests -- the outside-in test must hit THIS path).
// NewAuthInterceptor reuses the platform's JWT interceptor; every ExperienceService
// RPC requires authentication (no public procedures) -- the principal scope is the
// tenancy chokepoint, so an unauthenticated call must never reach the handler.
package experience

import (
	"context"
	"net/http"
	"strings"

	connect "connectrpc.com/connect"
	experiencev1connect "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1/experiencev1connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/server"
)

// orgScopePrefix is the JWT scope-claim prefix carrying the aocore ORG scope.
// The single AOID identity projects to a company (the cid claim) AND an org (this
// scope) -- both are AUTHENTICATED principal values, never bound from a body.
const orgScopePrefix = "org:"

// RegisterExperienceHandlers mounts the ExperienceService handler on mux. opts
// carry the interceptor chain (auth) the production wiring + the integration test
// both supply, so the dispatch path tested is the one shipped.
func RegisterExperienceHandlers(mux *http.ServeMux, handler experiencev1connect.ExperienceServiceHandler, opts ...connect.HandlerOption) {
	path, h := experiencev1connect.NewExperienceServiceHandler(handler, opts...)
	mux.Handle(path, h)
}

// NewAuthInterceptor returns the JWT auth interceptor for ExperienceService.
// There are NO public procedures -- every RPC is authenticated so the principal
// scope (the tenancy chokepoint) is always present.
func NewAuthInterceptor(jwtManager *auth.JWTManager) connect.Interceptor {
	return server.NewAuthInterceptor(jwtManager, map[string]bool{})
}

// PrincipalScope is the AUTHENTICATED principal's resolution scope, derived ONLY
// from the JWT claims -- never from a request body. TenantID is the eden-biz
// company claim (cid); OrgID is the aocore org carried in an "org:<id>" scope.
type PrincipalScope struct {
	TenantID string
	OrgID    string
}

// principalScopeFromContext extracts the scope from the request context's claims
// (set by the auth interceptor). ok is false when there are no claims (the RPC
// reached the handler unauthenticated, which the interceptor should prevent).
//
// This is THE chokepoint: the scope comes from the verified identity context, so
// a body-supplied tenant_id/org_id can never widen it (the body-binding-authority
// lesson -- authority is derived from identity, not the payload).
func principalScopeFromContext(ctx context.Context) (PrincipalScope, bool) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.CompanyID == "" {
		return PrincipalScope{}, false
	}
	scope := PrincipalScope{TenantID: claims.CompanyID}
	for _, s := range claims.Scopes {
		if strings.HasPrefix(s, orgScopePrefix) {
			scope.OrgID = strings.TrimPrefix(s, orgScopePrefix)
			break
		}
	}
	return scope, true
}
