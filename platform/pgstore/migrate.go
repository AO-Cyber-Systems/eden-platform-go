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
// The databaseURL should use the "pgx5://" scheme for the golang-migrate pgx/v5 driver,
// or "postgres://" which golang-migrate also accepts.
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
