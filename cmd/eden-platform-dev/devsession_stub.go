//go:build !dev

package main

import (
	"net/http"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/config"
)

// startDevSession is a no-op outside of `-tags dev` builds — mirrors
// devseed_stub.go's seedDevTenant stub. The real implementation (which
// authenticates the seeded dev user through the production Login path and
// mounts a dev-only token endpoint) lives in devsession.go and is dev-only
// by design — this stub exists solely so main.go's unconditional call site
// compiles in every build mode without a build tag on main.go itself.
func startDevSession(cfg *config.PlatformConfig, authService *auth.Service, mux *http.ServeMux) error {
	return nil
}
