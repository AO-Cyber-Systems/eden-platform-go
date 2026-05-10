package pgstore

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Backend aggregates all PostgreSQL-backed store implementations.
type Backend struct {
	pool *pgxpool.Pool
}

// NewBackend creates a pgstore Backend, runs migrations, and returns the backend.
// The migrationsFS should contain migration files with the prefix already stripped
// (e.g., via fs.Sub to remove "migrations/platform").
func NewBackend(ctx context.Context, databaseURL string, migrationsFS fs.FS) (*Backend, error) {
	if err := RunMigrations(databaseURL, migrationsFS); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return &Backend{pool: pool}, nil
}

// Pool returns the underlying connection pool for use in tests or advanced queries.
func (b *Backend) Pool() *pgxpool.Pool {
	return b.pool
}

func (b *Backend) AuthStore() *AuthStore           { return NewAuthStore(b.pool) }
func (b *Backend) CompanyStore() *CompanyStore     { return NewCompanyStore(b.pool) }
func (b *Backend) RBACStore() *RBACStore           { return NewRBACStore(b.pool) }
func (b *Backend) AuditStore() *AuditStore         { return NewAuditStore(b.pool) }
func (b *Backend) WebhookStore() *WebhookStore     { return NewWebhookStore(b.pool) }
func (b *Backend) HouseholdStore() *HouseholdStore { return NewHouseholdStore(b.pool) }
func (b *Backend) ConsentStore() *ConsentStore     { return NewConsentStore(b.pool) }

// Close releases all pool connections.
func (b *Backend) Close() {
	b.pool.Close()
}
