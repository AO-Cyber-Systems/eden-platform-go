package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/composition"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/discovery"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/fixtures"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/jwks"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/server"
)

// bootService composes the platform packages aoid wraps (auth +
// household + consent), wires the OIDC discovery and JWKS endpoints
// against the resulting JWTManager, and runs the HTTP server.
//
// Backend selection follows the existing eden-platform-dev convention:
// non-empty AOID_DATABASE_URL selects pgstore, anything else uses the
// in-memory devstore + fixture seeding.
func bootService(ctx context.Context, cfg *config.Config) error {
	svcs, err := buildServices(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build services: %w", err)
	}
	defer func() {
		if cerr := svcs.Close(); cerr != nil {
			slog.Error("aoid: close services", "error", cerr)
		}
	}()

	srv := server.New(cfg)
	srv.AddRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("/.well-known/openid-configuration", discovery.Handler(cfg))
		mux.HandleFunc("/oauth2/token", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/authorize", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/userinfo", discovery.IssuerNotActive)
		mux.HandleFunc("/.well-known/jwks.json", jwks.Handler(svcs.JWTManager))
	})
	return srv.Start(ctx, nil)
}

func buildServices(ctx context.Context, cfg *config.Config) (*composition.Services, error) {
	if cfg.DatabaseURL != "" {
		slog.Info("aoid: using pgstore backend", "database_url_present", true)
		return composition.BuildPostgres(ctx, cfg)
	}
	slog.Info("aoid: using in-memory devstore backend; seeding dev fixtures")
	svcs, err := composition.BuildInMemory(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := fixtures.Seed(ctx, svcs); err != nil {
		return nil, fmt.Errorf("seed fixtures: %w", err)
	}
	return svcs, nil
}
