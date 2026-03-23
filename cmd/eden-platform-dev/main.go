package main

import (
	"log"
	"net/http"
	"path/filepath"

	connect "connectrpc.com/connect"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/config"
	"github.com/aocybersystems/eden-platform-go/platform/connectapi"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	platformregistry "github.com/aocybersystems/eden-platform-go/platform/registry"
	"github.com/aocybersystems/eden-platform-go/platform/server"
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

	mux := http.NewServeMux()
	authInterceptor := server.NewAuthInterceptor(jwtManager, server.DefaultPublicProcedures())
	server.RegisterPlatformHandlers(
		mux,
		server.PlatformHandlers{
			Auth:     connectapi.NewAuthHandler(authService),
			Company:  connectapi.NewCompanyHandler(companyService, companyStore),
			Registry: connectapi.NewRegistryHandler(reg, companyStore),
		},
		connect.WithInterceptors(authInterceptor),
	)
	mux.Handle("/up", (&server.HealthChecker{}).Handler())

	handler := server.CORSMiddleware(server.LoggingMiddleware(mux))
	log.Printf("eden platform dev server listening on %s", cfg.ServerAddr)
	if err := http.ListenAndServe(cfg.ServerAddr, handler); err != nil {
		log.Fatal(err)
	}
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
