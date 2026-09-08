//go:build !dev

package main

import (
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/config"
)

// printQuickstart is a no-op outside of `-tags dev` builds — mirrors
// devseed_stub.go's seedDevTenant stub and devsession_stub.go's
// startDevSession stub. The real implementation (which logs in the seeded
// dev user and prints the quickstart banner: identity, token, base URL, and
// a working curl) lives in quickstart.go and is dev-only by design — this
// stub exists solely so main.go's unconditional call site compiles in every
// build mode without a build tag on main.go itself.
func printQuickstart(cfg *config.PlatformConfig, authService *auth.Service) error {
	return nil
}
