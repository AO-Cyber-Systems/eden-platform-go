package pgstore

import (
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// RunMigrations applies all pending migrations to the database.
// The migrationsFS should contain migration files at its root (already sub-pathed).
// The databaseURL must use the "pgx5://" scheme — this file registers ONLY the
// pgx/v5 driver via the blank import below. The "postgres://" scheme would
// require additionally importing "github.com/golang-migrate/migrate/v4/database/postgres"
// (lib/pq-backed), which the codebase does not.
func RunMigrations(databaseURL string, migrationsFS fs.FS) error {
	source, err := iofs.New(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
