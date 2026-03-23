package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	platformv1 "github.com/aocybersystems/eden-platform-go/gen/go/platform/v1"
	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ audit.AuditStore = (*AuditStore)(nil)

// AuditStore implements audit.AuditStore and connectapi.AuditLogQuerier
// backed by PostgreSQL via pgx and sqlc.
type AuditStore struct {
	pool *pgxpool.Pool
}

// NewAuditStore creates a new PostgreSQL-backed audit store.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool}
}

func (s *AuditStore) queries() *db.Queries {
	return db.New(s.pool)
}

func (s *AuditStore) CreateAuditLog(ctx context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	return s.queries().CreateAuditLog(ctx, db.CreateAuditLogParams{
		CompanyID:  companyID,
		ActorID:    actorID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    json.RawMessage(details),
		IpAddress:  ipAddress,
	})
}

// QueryAuditLogs satisfies connectapi.AuditLogQuerier with dynamic filtering.
// It builds a parameterized SQL query at runtime to support optional filters.
func (s *AuditStore) QueryAuditLogs(ctx context.Context, companyID uuid.UUID, limit, offset int, actorID *uuid.UUID, action, resource *string) ([]*platformv1.AuditLogEntry, int, error) {
	// Build dynamic WHERE clause
	where := "WHERE company_id = $1"
	args := []any{companyID}
	argIdx := 2

	if actorID != nil {
		where += fmt.Sprintf(" AND actor_id = $%d", argIdx)
		args = append(args, *actorID)
		argIdx++
	}
	if action != nil {
		where += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, *action)
		argIdx++
	}
	if resource != nil {
		where += fmt.Sprintf(" AND resource = $%d", argIdx)
		args = append(args, *resource)
		argIdx++
	}

	// Count query
	countSQL := "SELECT count(*) FROM audit_logs " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	// Data query with pagination
	dataSQL := fmt.Sprintf(
		"SELECT id, company_id, actor_id, action, resource, resource_id, details, ip_address, created_at FROM audit_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		where, argIdx, argIdx+1,
	)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []*platformv1.AuditLogEntry
	for rows.Next() {
		var (
			id         uuid.UUID
			cID        uuid.UUID
			aID        uuid.UUID
			act        string
			res        string
			resID      string
			details    json.RawMessage
			ipAddr     string
			createdAt  time.Time
		)
		if err := rows.Scan(&id, &cID, &aID, &act, &res, &resID, &details, &ipAddr, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}
		entries = append(entries, &platformv1.AuditLogEntry{
			Id:          id.String(),
			CompanyId:   cID.String(),
			ActorId:     aID.String(),
			Action:      act,
			Resource:    res,
			ResourceId:  resID,
			DetailsJson: string(details),
			IpAddress:   ipAddr,
			CreatedAt:   createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return entries, total, nil
}

// Ensure unused imports don't cause issues.
var _ = pgx.ErrNoRows
