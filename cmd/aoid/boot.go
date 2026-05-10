package main

import (
	"context"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/server"
)

// bootService is the boot entrypoint extracted into its own file so
// later TRDs can extend it (composition + fixtures + discovery + jwks
// wiring) without touching main.go. 29-01 ships the minimal version:
// just construct the server and run it.
func bootService(ctx context.Context, cfg *config.Config) error {
	srv := server.New(cfg)
	return srv.Start(ctx, nil)
}
