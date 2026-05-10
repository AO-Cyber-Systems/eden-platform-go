// Command aoid runs the AO ID identity service.
//
// Boot:
//
//	go run ./cmd/aoid                                # in-memory devstore, defaults
//	AOID_DATABASE_URL=postgres://... ./aoid          # pgstore-backed
//	AOID_LISTEN_ADDR=:9090 ./aoid
//
// Configuration is exclusively env-var driven; see internal/aoid/config
// for the full surface.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/config"
	"github.com/aocybersystems/eden-platform-go/internal/aoid/server"
)

func main() {
	cfg := config.Load()

	shutdownObs := server.MustSetupObservability(cfg)
	defer shutdownObs()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := bootService(ctx, cfg); err != nil {
		log.Fatalf("aoid: %v", err)
	}
}
