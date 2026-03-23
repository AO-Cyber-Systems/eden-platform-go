package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/config"
	"github.com/aocybersystems/eden-platform-go/platform/connectapi"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/bridge"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	platformregistry "github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
	"github.com/aocybersystems/eden-platform-go/platform/webhook"
	platformv1connect "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1/platformv1connect"
	"github.com/google/uuid"
)

func main() {
	cfg := config.Load()
	backend := devstore.NewMemoryBackend()
	authStore := backend.AuthStore()
	companyStore := backend.CompanyStore()
	reg := seedRegistry()

	if cfg.JWTPrivateKeyPath == "" || cfg.JWTPublicKeyPath == "" {
		cfg.JWTPrivateKeyPath = filepath.Join("dev", "jwt", "jwt_es256_private.pem")
		cfg.JWTPublicKeyPath = filepath.Join("dev", "jwt", "jwt_es256_public.pem")
	}

	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		PrivateKeyPath:     cfg.JWTPrivateKeyPath,
		PublicKeyPath:      cfg.JWTPublicKeyPath,
		Issuer:             "eden-platform-dev",
		AccessTokenExpiry:  auth.DefaultJWTConfig().AccessTokenExpiry,
		RefreshTokenExpiry: auth.DefaultJWTConfig().RefreshTokenExpiry,
	})
	if err != nil {
		log.Fatalf("create jwt manager: %v", err)
	}

	authService := auth.NewService(authStore, jwtManager, auth.NewPasswordHasher())
	companyService := company.NewService(companyStore)
	ssoService := auth.NewSSOService(authStore, jwtManager, "http://localhost"+cfg.ServerAddr)

	seedSSOForDev(backend)
	seedRBACData(backend)

	rbacStore := backend.RBACStore()
	enforcer := rbac.NewEnforcer(rbacStore, nil)

	// RBAC service
	rbacResolver := rbac.NewHierarchyResolver(rbacStore)
	rbacService := rbac.NewService(rbacStore, enforcer, rbacResolver)

	// Audit store
	auditStore := backend.AuditStore()

	// Webhook service
	webhookStore := backend.WebhookStore()
	webhookService := webhook.NewService(webhookStore)

	// Bridge adapter registry
	adapterRegistry := bridge.NewAdapterRegistry()
	seedBridgeAdapters(adapterRegistry)

	// Seed additional data
	seedWebhookData(backend)
	seedAuditData(backend)

	mux := http.NewServeMux()
	ssoService.RegisterHTTPHandlers(mux)

	authInterceptor := server.NewAuthInterceptor(jwtManager, server.DefaultPublicProcedures())
	rbacConfig := server.InterceptorConfig{
		PublicProcedures:     server.DefaultPublicProcedures(),
		ProcedurePermissions: defaultProcedurePermissions(),
	}
	rbacInterceptor := server.NewRBACInterceptor(enforcer, rbacConfig)
	slog.Info("RBAC interceptor enabled", "mapped_procedures", len(defaultProcedurePermissions()))

	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Auth:     connectapi.NewAuthHandler(authService, ssoService),
			Company:  connectapi.NewCompanyHandler(companyService, companyStore),
			Registry: connectapi.NewRegistryHandler(reg, companyStore),
			RBAC:     connectapi.NewRBACHandler(rbacService, enforcer, rbacResolver),
			Audit:    connectapi.NewAuditHandler(auditStore),
			Webhook:  connectapi.NewWebhookHandler(webhookService, webhookStore, webhookStore),
			Bridge:   connectapi.NewBridgeHandler(adapterRegistry),
		},
		connect.WithInterceptors(authInterceptor, rbacInterceptor),
	)
	mux.Handle("/up", (&server.HealthChecker{}).Handler())

	handler := server.CORSMiddleware(server.LoggingMiddleware(mux))
	log.Printf("eden platform dev server listening on %s", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, handler); err != nil {
		log.Fatal(err)
	}
}

func defaultProcedurePermissions() map[string]server.Permission {
	return map[string]server.Permission{
		platformv1connect.CompanyServiceCreateCompanyProcedure:  {Feature: "settings", Action: "admin"},
		platformv1connect.CompanyServiceUpdateCompanyProcedure:  {Feature: "settings", Action: "edit"},
		// RBAC management requires admin
		platformv1connect.RBACServiceCreateRoleProcedure:              {Feature: "settings", Action: "admin"},
		platformv1connect.RBACServiceAssignRoleProcedure:              {Feature: "settings", Action: "admin"},
		platformv1connect.RBACServiceRemoveRoleProcedure:              {Feature: "settings", Action: "admin"},
		// Webhook management requires admin
		platformv1connect.WebhookServiceRegisterWebhookProcedure:      {Feature: "settings", Action: "admin"},
		platformv1connect.WebhookServiceDeleteWebhookProcedure:        {Feature: "settings", Action: "admin"},
	}
}

func seedRBACData(backend *devstore.Backend) {
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	// Seed system roles matching auth role IDs
	systemRoles := []rbac.Role{
		{ID: rbac.OwnerRoleID, Name: "owner", Level: rbac.RoleLevelOwner, IsSystem: true},
		{ID: rbac.AdminRoleID, Name: "admin", Level: rbac.RoleLevelAdmin, IsSystem: true},
		{ID: rbac.MemberRoleID, Name: "member", Level: rbac.RoleLevelMember, IsSystem: true},
		{ID: rbac.ViewerRoleID, Name: "viewer", Level: rbac.RoleLevelViewer, IsSystem: true},
	}

	for _, role := range systemRoles {
		backend.SeedRBACRole(role)
	}

	// Seed base permissions
	type permDef struct {
		feature string
		action  string
	}
	basePerms := []permDef{
		{"settings", "view"}, {"settings", "edit"}, {"settings", "admin"},
		{"projects", "view"}, {"projects", "create"}, {"projects", "edit"}, {"projects", "delete"},
	}

	permIDs := make(map[string]uuid.UUID)
	for _, p := range basePerms {
		id := uuid.New()
		permIDs[p.feature+":"+p.action] = id
		backend.SeedRBACPermission(rbac.Permission{
			ID:      id,
			Feature: p.feature,
			Action:  p.action,
		})
	}

	// Assign permissions to roles
	// Owner gets all
	for _, pid := range permIDs {
		_ = rbacStore.AddRolePermission(ctx, rbac.OwnerRoleID, pid)
	}
	// Admin gets all except settings:admin
	for key, pid := range permIDs {
		if key != "settings:admin" {
			_ = rbacStore.AddRolePermission(ctx, rbac.AdminRoleID, pid)
		}
	}
	// Member gets view, create, edit
	for key, pid := range permIDs {
		if key == "settings:admin" || key == "projects:delete" {
			continue
		}
		_ = rbacStore.AddRolePermission(ctx, rbac.MemberRoleID, pid)
	}
	// Viewer gets view only
	for key, pid := range permIDs {
		if strings.HasSuffix(key, ":view") {
			_ = rbacStore.AddRolePermission(ctx, rbac.ViewerRoleID, pid)
		}
	}

	slog.Info("seeded RBAC data",
		"roles", len(systemRoles),
		"permissions", len(basePerms),
	)
}

func seedSSOForDev(backend *devstore.Backend) {
	testCompanyID := uuid.MustParse("20000000-0000-0000-0000-000000000001")

	backend.SetSSOConfig(testCompanyID, "oidc", auth.SSOConfig{
		CompanyID:    testCompanyID,
		Provider:     "oidc",
		IssuerURL:    "https://accounts.google.com",
		ClientID:     "dev-oidc-client-id",
		ClientSecret: "dev-oidc-client-secret",
	})

	backend.SetSSOConfig(testCompanyID, "saml", auth.SSOConfig{
		CompanyID:   testCompanyID,
		Provider:    "saml",
		MetadataURL: "https://login.microsoftonline.com/dev-tenant/federationmetadata/2007-06/federationmetadata.xml",
	})

	slog.Info("seeded SSO config for dev", "company_id", testCompanyID)
}

func seedBridgeAdapters(registry *bridge.AdapterRegistry) {
	registry.Register("eden.platform.", &devAdapter{})
	slog.Info("seeded bridge adapters", "count", 1)
}

// devAdapter is a simple adapter for dev/testing.
type devAdapter struct{}

func (a *devAdapter) EventTypes() []string {
	return []string{"eden.platform.user.created", "eden.platform.company.created", "eden.platform.role.assigned"}
}

func (a *devAdapter) Transform(subject string, envelope bridge.EventEnvelope) (*bridge.TransformedEvent, error) {
	return &bridge.TransformedEvent{
		EventType: envelope.EventType,
		SourceID:  envelope.EventID,
		CompanyID: envelope.CompanyID,
		Data:      envelope.Data,
	}, nil
}

func (a *devAdapter) ActionTypes() []bridge.ActionSchema {
	return []bridge.ActionSchema{
		{Type: "eden.notify", Label: "Send Notification", RequiresInput: true, InputHint: "Notification message", Destructive: false},
		{Type: "eden.export", Label: "Export Data", RequiresInput: false, Destructive: false},
	}
}

func (a *devAdapter) SupportsAction(actionType string) bool {
	return actionType == "eden.notify" || actionType == "eden.export"
}

func seedWebhookData(backend *devstore.Backend) {
	webhookStore := backend.WebhookStore()
	ctx := context.Background()
	testCompanyID := uuid.MustParse("20000000-0000-0000-0000-000000000001")

	_, _ = webhookStore.CreateWebhook(ctx, testCompanyID, "https://example.com/webhook", "test-secret-key", []string{"eden.platform.*"})
	slog.Info("seeded webhook data", "company_id", testCompanyID)
}

func seedAuditData(backend *devstore.Backend) {
	auditStore := backend.AuditStore()
	ctx := context.Background()
	testCompanyID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	testUserID := uuid.MustParse("30000000-0000-0000-0000-000000000001")

	actions := []struct{ action, resource, resourceID string }{
		{"user.login", "user", testUserID.String()},
		{"company.settings.updated", "company", testCompanyID.String()},
		{"role.assigned", "membership", uuid.New().String()},
	}
	for _, a := range actions {
		_ = auditStore.CreateAuditLog(ctx, testCompanyID, testUserID, a.action, a.resource, a.resourceID, "127.0.0.1", []byte(`{}`))
	}
	slog.Info("seeded audit data", "entries", len(actions))
}

func seedRegistry() *platformregistry.Registry {
	reg := platformregistry.New()
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "home",
		NavItems: []platformregistry.NavItem{
			{ID: "home", Label: "Home", Icon: "home", Path: "/home", Feature: "home", Priority: 0},
		},
		Widgets: []platformregistry.Widget{
			{ID: "welcome", Label: "Welcome", Type: "summary", Feature: "home", Priority: 0},
		},
		BadgeProvider: func(companyID, userID string) int { return 1 },
	})
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "projects",
		NavItems: []platformregistry.NavItem{
			{ID: "projects", Label: "Projects", Icon: "folder", Path: "/projects", Feature: "projects", Priority: 10},
		},
		SearchScopes: []platformregistry.SearchScope{
			{ID: "projects", Label: "Projects", Feature: "projects"},
		},
		BadgeProvider: func(companyID, userID string) int { return 4 },
	})
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "activity",
		NavItems: []platformregistry.NavItem{
			{ID: "activity", Label: "Activity", Icon: "analytics", Path: "/activity", Feature: "activity", Priority: 20},
		},
		BadgeProvider: func(companyID, userID string) int { return 2 },
	})
	reg.Register(&platformregistry.ModuleRegistration{
		Name: "settings",
		NavItems: []platformregistry.NavItem{
			{ID: "settings", Label: "Settings", Icon: "settings", Path: "/settings", Feature: "settings", Priority: 30},
		},
	})
	return reg
}
