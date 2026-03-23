package server

import (
	"net/http"

	connect "connectrpc.com/connect"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
)

type PlatformHandlers struct {
	Auth     platformv1connect.AuthServiceHandler
	Company  platformv1connect.CompanyServiceHandler
	Registry platformv1connect.RegistryServiceHandler
	RBAC     platformv1connect.RBACServiceHandler
	Audit    platformv1connect.AuditServiceHandler
	Webhook  platformv1connect.WebhookServiceHandler
	Bridge   platformv1connect.BridgeServiceHandler
}

func DefaultPublicProcedures() map[string]bool {
	return map[string]bool{
		platformv1connect.AuthServiceLoginProcedure:        true,
		platformv1connect.AuthServiceSignUpProcedure:       true,
		platformv1connect.AuthServiceRefreshTokenProcedure: true,
		platformv1connect.AuthServiceLogoutProcedure:       true,
	}
}

func RegisterPlatformHandlers(mux *http.ServeMux, handlers PlatformHandlers, opts ...connect.HandlerOption) {
	if handlers.Auth != nil {
		path, handler := platformv1connect.NewAuthServiceHandler(handlers.Auth, opts...)
		mux.Handle(path, handler)
	}
	if handlers.Company != nil {
		path, handler := platformv1connect.NewCompanyServiceHandler(handlers.Company, opts...)
		mux.Handle(path, handler)
	}
	if handlers.Registry != nil {
		path, handler := platformv1connect.NewRegistryServiceHandler(handlers.Registry, opts...)
		mux.Handle(path, handler)
	}
	if handlers.RBAC != nil {
		path, handler := platformv1connect.NewRBACServiceHandler(handlers.RBAC, opts...)
		mux.Handle(path, handler)
	}
	if handlers.Audit != nil {
		path, handler := platformv1connect.NewAuditServiceHandler(handlers.Audit, opts...)
		mux.Handle(path, handler)
	}
	if handlers.Webhook != nil {
		path, handler := platformv1connect.NewWebhookServiceHandler(handlers.Webhook, opts...)
		mux.Handle(path, handler)
	}
	if handlers.Bridge != nil {
		path, handler := platformv1connect.NewBridgeServiceHandler(handlers.Bridge, opts...)
		mux.Handle(path, handler)
	}
}
