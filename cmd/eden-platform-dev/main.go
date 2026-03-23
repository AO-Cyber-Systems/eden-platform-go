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
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	platformregistry "github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
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
	}
}

func seedRBACData(backend *devstore.Backend) {
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	// Seed system roles matching auth role IDs
	systemRoles := []rbac.Role{
		{ID: auth.OwnerRoleID, Name: "owner", Level: rbac.RoleLevelOwner, IsSystem: true},
		{ID: auth.AdminRoleID, Name: "admin", Level: rbac.RoleLevelAdmin, IsSystem: true},
		{ID: auth.MemberRoleID, Name: "member", Level: rbac.RoleLevelMember, IsSystem: true},
		{ID: auth.ViewerRoleID, Name: "viewer", Level: rbac.RoleLevelViewer, IsSystem: true},
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
		_ = rbacStore.AddRolePermission(ctx, auth.OwnerRoleID, pid)
	}
	// Admin gets all except settings:admin
	for key, pid := range permIDs {
		if key != "settings:admin" {
			_ = rbacStore.AddRolePermission(ctx, auth.AdminRoleID, pid)
		}
	}
	// Member gets view, create, edit
	for key, pid := range permIDs {
		if key == "settings:admin" || key == "projects:delete" {
			continue
		}
		_ = rbacStore.AddRolePermission(ctx, auth.MemberRoleID, pid)
	}
	// Viewer gets view only
	for key, pid := range permIDs {
		if strings.HasSuffix(key, ":view") {
			_ = rbacStore.AddRolePermission(ctx, auth.ViewerRoleID, pid)
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
