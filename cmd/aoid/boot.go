package main

import (
	"context"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/discovery"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/jwks"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/server"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// bootService is the boot entrypoint extracted into its own file so
// later TRDs can extend it (composition + fixtures wiring) without
// touching main.go.
//
// 29-02 wires the OIDC discovery and JWKS endpoints. The JWKS handler
// needs a JWTManager so we build a minimal one here from configured
// seed paths (or ephemeral if unset). The richer composition that
// platform/auth + platform/household + platform/consent share will
// arrive in 29-03 and will replace the local manager with one threaded
// through composition.Services.
func bootService(ctx context.Context, cfg *config.Config) error {
	jm, err := buildJWTManager(cfg)
	if err != nil {
		return err
	}

	srv := server.New(cfg)
	srv.AddRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("/.well-known/openid-configuration", discovery.Handler(cfg))
		mux.HandleFunc("/oauth2/token", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/authorize", discovery.IssuerNotActive)
		mux.HandleFunc("/oauth2/userinfo", discovery.IssuerNotActive)
		mux.HandleFunc("/.well-known/jwks.json", jwks.Handler(jm))
	})
	return srv.Start(ctx, nil)
}

func buildJWTManager(cfg *config.Config) (*auth.JWTManager, error) {
	jc := auth.DefaultJWTConfig()
	jc.Issuer = cfg.Issuer
	jc.AccessTokenExpiry = cfg.AccessTokenExpiry
	jc.RefreshTokenExpiry = cfg.RefreshTokenExpiry
	jc.KeySeedPath = cfg.JWTKeySeedPath
	jc.KeySeedPaths = cfg.JWTKeySeedPaths
	jc.ActiveKID = cfg.JWTActiveKID
	return auth.NewJWTManager(jc)
}
