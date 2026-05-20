package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

// The claims context key lives in platform/auth so that the canonical
// helpers (auth.WithClaims / auth.ClaimsFromContext / auth.RequireCompany)
// share a single key across HTTP middleware and ConnectRPC interceptors.
// The thin re-exports at the bottom of this file preserve the historical
// platform/server.WithClaims / platform/server.ClaimsFromContext API for
// existing callers — both delegate to the canonical helpers so claims
// written here are readable by auth.RequireCompany downstream.

// Permission represents a required permission for a procedure.
type Permission struct {
	Feature string
	Action  string
}

// InterceptorConfig configures the server interceptor chain.
type InterceptorConfig struct {
	PublicProcedures     map[string]bool
	ProcedurePermissions map[string]Permission
}

// authInterceptor implements connect.Interceptor for JWT Bearer token validation
// on both unary and server-streaming RPCs.
type authInterceptor struct {
	jwtManager       *auth.JWTManager
	publicProcedures map[string]bool
}

// NewAuthInterceptor creates a ConnectRPC interceptor that validates JWT Bearer tokens
// on unary and streaming handlers.
func NewAuthInterceptor(jwtManager *auth.JWTManager, publicProcedures map[string]bool) connect.Interceptor {
	return &authInterceptor{jwtManager: jwtManager, publicProcedures: publicProcedures}
}

func (i *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.publicProcedures[req.Spec().Procedure] {
			return next(ctx, req)
		}

		authHeader := req.Header().Get("Authorization")
		if authHeader == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		claims, err := i.jwtManager.ValidateAccessToken(parts[1])
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		ctx = WithClaims(ctx, claims)
		return next(ctx, req)
	})
}

func (i *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if i.publicProcedures[conn.Spec().Procedure] {
			return next(ctx, conn)
		}

		authHeader := conn.RequestHeader().Get("Authorization")
		if authHeader == "" {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		claims, err := i.jwtManager.ValidateAccessToken(parts[1])
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		ctx = WithClaims(ctx, claims)
		return next(ctx, conn)
	})
}

func (i *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	// server-side interceptor; client wrap is pass-through
	return next
}

// rbacInterceptor implements connect.Interceptor for permission enforcement on
// both unary and server-streaming RPCs.
type rbacInterceptor struct {
	enforcer *rbac.Enforcer
	config   InterceptorConfig
}

// NewRBACInterceptor creates a ConnectRPC interceptor that checks permissions
// on unary and streaming handlers.
func NewRBACInterceptor(enforcer *rbac.Enforcer, config InterceptorConfig) connect.Interceptor {
	return &rbacInterceptor{enforcer: enforcer, config: config}
}

func (i *rbacInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.config.PublicProcedures[req.Spec().Procedure] {
			return next(ctx, req)
		}

		perm, ok := i.config.ProcedurePermissions[req.Spec().Procedure]
		if !ok {
			return next(ctx, req)
		}

		claims := ClaimsFromContext(ctx)
		if claims == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}
		companyID, err := uuid.Parse(claims.CompanyID)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		allowed, err := i.enforcer.HasPermission(ctx, userID, companyID, perm.Feature+":"+perm.Action)
		if err != nil {
			slog.Error("RBAC check failed", "procedure", req.Spec().Procedure, "error", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("permission check failed"))
		}
		if !allowed {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("insufficient permissions"))
		}

		return next(ctx, req)
	})
}

func (i *rbacInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if i.config.PublicProcedures[conn.Spec().Procedure] {
			return next(ctx, conn)
		}

		perm, ok := i.config.ProcedurePermissions[conn.Spec().Procedure]
		if !ok {
			return next(ctx, conn)
		}

		claims := ClaimsFromContext(ctx)
		if claims == nil {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}
		companyID, err := uuid.Parse(claims.CompanyID)
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		allowed, err := i.enforcer.HasPermission(ctx, userID, companyID, perm.Feature+":"+perm.Action)
		if err != nil {
			slog.Error("RBAC check failed", "procedure", conn.Spec().Procedure, "error", err)
			return connect.NewError(connect.CodeInternal, errors.New("permission check failed"))
		}
		if !allowed {
			return connect.NewError(connect.CodePermissionDenied, errors.New("insufficient permissions"))
		}

		return next(ctx, conn)
	})
}

func (i *rbacInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// auditInterceptor implements connect.Interceptor for audit-log emission on both
// unary and server-streaming RPCs.
type auditInterceptor struct {
	logger           *audit.Logger
	publicProcedures map[string]bool
}

// NewAuditInterceptor creates a ConnectRPC interceptor that logs audit events
// after handler execution, for both unary and streaming handlers.
func NewAuditInterceptor(logger *audit.Logger, publicProcedures map[string]bool) connect.Interceptor {
	return &auditInterceptor{logger: logger, publicProcedures: publicProcedures}
}

func (i *auditInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.publicProcedures[req.Spec().Procedure] {
			return next(ctx, req)
		}

		resp, handlerErr := next(ctx, req)

		claims := ClaimsFromContext(ctx)
		if claims != nil {
			procedure := req.Spec().Procedure
			parts := strings.Split(strings.TrimPrefix(procedure, "/"), "/")
			resource := "unknown"
			action := "unknown"
			if len(parts) == 2 {
				resource = parts[0]
				action = parts[1]
			}

			details := map[string]any{"procedure": procedure}
			if handlerErr != nil {
				details["error"] = handlerErr.Error()
				details["status"] = "error"
			} else {
				details["status"] = "success"
			}

			i.logger.Log(audit.Event{
				CompanyID:  claims.CompanyID,
				ActorID:    claims.UserID,
				Action:     action,
				Resource:   resource,
				ResourceID: "",
				Details:    details,
				IPAddress:  req.Header().Get("X-Forwarded-For"),
			})
		}

		return resp, handlerErr
	})
}

func (i *auditInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if i.publicProcedures[conn.Spec().Procedure] {
			return next(ctx, conn)
		}

		handlerErr := next(ctx, conn)

		claims := ClaimsFromContext(ctx)
		if claims != nil {
			procedure := conn.Spec().Procedure
			parts := strings.Split(strings.TrimPrefix(procedure, "/"), "/")
			resource := "unknown"
			action := "unknown"
			if len(parts) == 2 {
				resource = parts[0]
				action = parts[1]
			}

			details := map[string]any{"procedure": procedure}
			if handlerErr != nil {
				details["error"] = handlerErr.Error()
				details["status"] = "error"
			} else {
				details["status"] = "success"
			}

			i.logger.Log(audit.Event{
				CompanyID:  claims.CompanyID,
				ActorID:    claims.UserID,
				Action:     action,
				Resource:   resource,
				ResourceID: "",
				Details:    details,
				IPAddress:  conn.RequestHeader().Get("X-Forwarded-For"),
			})
		}

		return handlerErr
	})
}

func (i *auditInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WithClaims stores auth claims in the context.
//
// Backward-compat shim — the canonical home is platform/auth.WithClaims.
// Existing callers (ConnectRPC interceptor wiring, downstream services)
// keep working unchanged because both this and the canonical version
// write to the same context key.
func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return auth.WithClaims(ctx, claims)
}

// ClaimsFromContext retrieves auth claims from the context.
//
// Backward-compat shim — the canonical home is platform/auth.ClaimsFromContext.
func ClaimsFromContext(ctx context.Context) *auth.Claims {
	return auth.ClaimsFromContext(ctx)
}

// ExtractClaims returns userID, companyID, role from context.
func ExtractClaims(ctx context.Context) (userID, companyID, role string) {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return "", "", ""
	}
	return claims.UserID, claims.CompanyID, claims.Role
}
