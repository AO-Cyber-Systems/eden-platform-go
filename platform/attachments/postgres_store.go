package attachments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists Attachment rows in Postgres.
//
// Schema (consumer-owned migration):
//
//	CREATE TABLE attachments (
//	    id                       UUID PRIMARY KEY,
//	    attachment_owner_type    TEXT NOT NULL,
//	    attachment_owner_id      TEXT NOT NULL,
//	    storage_key              TEXT NOT NULL UNIQUE,
//	    filename                 TEXT NOT NULL,
//	    content_type             TEXT NOT NULL,
//	    size                     BIGINT NOT NULL,
//	    uploaded_by              TEXT NOT NULL,
//	    metadata                 JSONB NOT NULL DEFAULT '{}'::jsonb,
//	    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
//	CREATE INDEX attachments_owner_idx ON attachments
//	    (attachment_owner_type, attachment_owner_id);
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wires a Postgres-backed Store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Insert(ctx context.Context, a Attachment) error {
	meta := a.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	mb, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("attachments: marshal metadata: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO attachments
		(id, attachment_owner_type, attachment_owner_id, storage_key, filename,
		 content_type, size, uploaded_by, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, a.Owner.Type, a.Owner.ID, a.StorageKey, a.Filename, a.ContentType, a.Size,
		a.UploadedBy, mb, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("attachments: insert: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (Attachment, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, attachment_owner_type, attachment_owner_id, storage_key, filename,
		       content_type, size, uploaded_by, metadata, created_at
		FROM attachments WHERE id = $1`, id)

	var (
		a   Attachment
		ot  string
		oid string
		mb  []byte
		ts  time.Time
	)
	err := row.Scan(&a.ID, &ot, &oid, &a.StorageKey, &a.Filename, &a.ContentType, &a.Size, &a.UploadedBy, &mb, &ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("attachments: scan: %w", err)
	}
	a.Owner = Owner{Type: ot, ID: oid}
	a.CreatedAt = ts
	if len(mb) > 0 {
		_ = json.Unmarshal(mb, &a.Metadata)
	}
	return a, nil
}

func (s *PostgresStore) ListByOwner(ctx context.Context, owner Owner) ([]Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, attachment_owner_type, attachment_owner_id, storage_key, filename,
		       content_type, size, uploaded_by, metadata, created_at
		FROM attachments
		WHERE attachment_owner_type = $1 AND attachment_owner_id = $2
		ORDER BY created_at ASC`, owner.Type, owner.ID)
	if err != nil {
		return nil, fmt.Errorf("attachments: list: %w", err)
	}
	defer rows.Close()

	var out []Attachment
	for rows.Next() {
		var (
			a   Attachment
			ot  string
			oid string
			mb  []byte
			ts  time.Time
		)
		if err := rows.Scan(&a.ID, &ot, &oid, &a.StorageKey, &a.Filename, &a.ContentType, &a.Size, &a.UploadedBy, &mb, &ts); err != nil {
			return nil, fmt.Errorf("attachments: scan: %w", err)
		}
		a.Owner = Owner{Type: ot, ID: oid}
		a.CreatedAt = ts
		if len(mb) > 0 {
			_ = json.Unmarshal(mb, &a.Metadata)
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("attachments: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
