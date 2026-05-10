package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is a Store backed by Postgres using SELECT FOR UPDATE SKIP LOCKED.
//
// Schema (consumer-owned migration):
//
//	CREATE TABLE jobs (
//	    id           UUID PRIMARY KEY,
//	    type         TEXT NOT NULL,
//	    payload      JSONB NOT NULL,
//	    priority     INT NOT NULL DEFAULT 0,
//	    max_retries  INT NOT NULL DEFAULT 3,
//	    attempts     INT NOT NULL DEFAULT 0,
//	    status       TEXT NOT NULL DEFAULT 'pending',
//	    error        TEXT NOT NULL DEFAULT '',
//	    locked_by    TEXT,
//	    locked_at    TIMESTAMPTZ,
//	    scheduled_at TIMESTAMPTZ,
//	    completed_at TIMESTAMPTZ,
//	    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
//	CREATE INDEX jobs_dequeue_idx ON jobs (status, type, priority DESC, created_at)
//	    WHERE status = 'pending';
//
// See README.md for tuning notes (sweep interval for stale locks, etc.).
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires a Store against a pgx connection pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Enqueue(ctx context.Context, j Job) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (id, type, payload, priority, max_retries, attempts, status, scheduled_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		j.ID, j.Type, j.Payload, j.Priority, j.MaxRetries, j.Attempts, j.Status, j.ScheduledAt, j.CreatedAt)
	if err != nil {
		return fmt.Errorf("jobs: enqueue: %w", err)
	}
	return nil
}

func (s *PostgresStore) Dequeue(ctx context.Context, jobTypes []string, lockedBy string) (*Job, error) {
	now := time.Now().UTC()
	row := s.pool.QueryRow(ctx, `
		UPDATE jobs SET
		    status    = 'running',
		    locked_by = $1,
		    locked_at = $2,
		    attempts  = attempts + 1
		WHERE id = (
		    SELECT id FROM jobs
		    WHERE status = 'pending'
		      AND type = ANY($3)
		      AND (scheduled_at IS NULL OR scheduled_at <= $2)
		    ORDER BY priority DESC, created_at ASC
		    LIMIT 1
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, payload, priority, max_retries, attempts, status, error,
		          locked_by, locked_at, scheduled_at, completed_at, created_at`,
		lockedBy, now, jobTypes)

	var (
		j       Job
		payload []byte
	)
	err := row.Scan(&j.ID, &j.Type, &payload, &j.Priority, &j.MaxRetries, &j.Attempts,
		&j.Status, &j.Error, &j.LockedBy, &j.LockedAt, &j.ScheduledAt, &j.CompletedAt, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: dequeue: %w", err)
	}
	j.Payload = json.RawMessage(payload)
	return &j, nil
}

func (s *PostgresStore) Complete(ctx context.Context, jobID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='completed', completed_at=$1, locked_by=NULL, locked_at=NULL WHERE id=$2`,
		time.Now().UTC(), jobID)
	if err != nil {
		return fmt.Errorf("jobs: complete: %w", err)
	}
	return nil
}

func (s *PostgresStore) Fail(ctx context.Context, jobID, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='failed', error=$1, locked_by=NULL, locked_at=NULL WHERE id=$2`, errMsg, jobID)
	if err != nil {
		return fmt.Errorf("jobs: fail: %w", err)
	}
	return nil
}

func (s *PostgresStore) Retry(ctx context.Context, jobID string, scheduledAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='pending', scheduled_at=$1, locked_by=NULL, locked_at=NULL WHERE id=$2`,
		scheduledAt, jobID)
	if err != nil {
		return fmt.Errorf("jobs: retry: %w", err)
	}
	return nil
}

func (s *PostgresStore) MarkDead(ctx context.Context, jobID, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='dead', error=$1, locked_by=NULL, locked_at=NULL WHERE id=$2`, errMsg, jobID)
	if err != nil {
		return fmt.Errorf("jobs: mark dead: %w", err)
	}
	return nil
}

func (s *PostgresStore) Heartbeat(ctx context.Context, jobID, lockedBy string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET locked_at=$1 WHERE id=$2 AND locked_by=$3`,
		time.Now().UTC(), jobID, lockedBy)
	if err != nil {
		return fmt.Errorf("jobs: heartbeat: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListJobs(ctx context.Context, status string, limit int) ([]Job, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, type, payload, priority, max_retries, attempts, status, error,
		       locked_by, locked_at, scheduled_at, completed_at, created_at
		FROM jobs WHERE status=$1 ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: list: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var (
			j       Job
			payload []byte
		)
		if err := rows.Scan(&j.ID, &j.Type, &payload, &j.Priority, &j.MaxRetries, &j.Attempts,
			&j.Status, &j.Error, &j.LockedBy, &j.LockedAt, &j.ScheduledAt, &j.CompletedAt, &j.CreatedAt); err != nil {
			return nil, fmt.Errorf("jobs: scan: %w", err)
		}
		j.Payload = json.RawMessage(payload)
		out = append(out, j)
	}
	return out, nil
}
