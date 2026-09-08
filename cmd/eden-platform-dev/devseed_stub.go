//go:build !dev

package main

import "github.com/aocybersystems/eden-platform-go/platform/devstore"

// seedDevTenant is a no-op outside of `-tags dev` builds. The real
// implementation (which seeds a demo company/user/membership/role binding)
// lives in devseed.go and is dev-only by design — this stub exists solely so
// main.go's unconditional call site compiles in every build mode without a
// build tag on main.go itself.
func seedDevTenant(backend *devstore.Backend) {}
