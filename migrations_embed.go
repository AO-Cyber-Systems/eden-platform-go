package edenplatform

import "embed"

//go:embed migrations/platform/*.sql
var MigrationsFS embed.FS
