package cms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store implementation, backed by the
// cms_pages table (migration 018). Blocks are stored as a single JSONB
// array per page -- see the migration's header comment for why one atomic
// column (not one row per block) is what makes a reorder a single UPDATE.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore constructs a PostgresStore bound to the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Compile-time assertion that PostgresStore satisfies Store.
var _ Store = (*PostgresStore)(nil)

// CreatePage implements Store.
func (s *PostgresStore) CreatePage(ctx context.Context, p Page) (Page, error) {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Status == "" {
		p.Status = PageStatusDraft
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	blocksJSON, err := json.Marshal(p.Blocks)
	if err != nil {
		return Page{}, fmt.Errorf("cms: marshal blocks: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO cms_pages
		    (id, company_id, slug, title, blocks, status,
		     scheduled_publish_at, published_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.CompanyID, p.Slug, p.Title, blocksJSON, string(p.Status),
		p.ScheduledPublishAt, p.PublishedAt, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		if isSlugConflict(err) {
			return Page{}, ErrSlugTaken
		}
		return Page{}, fmt.Errorf("cms: create page: %w", err)
	}
	return p, nil
}

// GetPage implements Store.
func (s *PostgresStore) GetPage(ctx context.Context, companyID, id uuid.UUID) (Page, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, company_id, slug, title, blocks, status,
		       scheduled_publish_at, published_at, created_at, updated_at
		  FROM cms_pages
		 WHERE company_id = $1 AND id = $2`, companyID, id)
	p, err := scanPage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("cms: get page: %w", err)
	}
	return p, nil
}

// GetPageBySlug implements Store.
func (s *PostgresStore) GetPageBySlug(ctx context.Context, companyID uuid.UUID, slug string) (Page, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, company_id, slug, title, blocks, status,
		       scheduled_publish_at, published_at, created_at, updated_at
		  FROM cms_pages
		 WHERE company_id = $1 AND slug = $2`, companyID, slug)
	p, err := scanPage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("cms: get page by slug: %w", err)
	}
	return p, nil
}

// ListPages implements Store.
func (s *PostgresStore) ListPages(ctx context.Context, companyID uuid.UUID) ([]Page, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, slug, title, blocks, status,
		       scheduled_publish_at, published_at, created_at, updated_at
		  FROM cms_pages
		 WHERE company_id = $1
		 ORDER BY created_at DESC`, companyID)
	if err != nil {
		return nil, fmt.Errorf("cms: list pages: %w", err)
	}
	defer rows.Close()

	out := []Page{}
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, fmt.Errorf("cms: list pages scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cms: list pages rows: %w", err)
	}
	return out, nil
}

// UpdatePage implements Store. It is a full replacement of the page's
// mutable fields, including Blocks -- this is the reorder path: a caller
// that reorders p.Blocks and calls UpdatePage persists that new order, and
// a subsequent GetPage / GetPageBySlug / ListPages reflects it (index
// position IS render order -- see Block).
func (s *PostgresStore) UpdatePage(ctx context.Context, p Page) (Page, error) {
	blocksJSON, err := json.Marshal(p.Blocks)
	if err != nil {
		return Page{}, fmt.Errorf("cms: marshal blocks: %w", err)
	}
	p.UpdatedAt = time.Now().UTC()

	tag, err := s.pool.Exec(ctx, `
		UPDATE cms_pages SET
		    slug = $3,
		    title = $4,
		    blocks = $5,
		    status = $6,
		    scheduled_publish_at = $7,
		    published_at = $8,
		    updated_at = $9
		WHERE company_id = $1 AND id = $2`,
		p.CompanyID, p.ID, p.Slug, p.Title, blocksJSON, string(p.Status),
		p.ScheduledPublishAt, p.PublishedAt, p.UpdatedAt)
	if err != nil {
		if isSlugConflict(err) {
			return Page{}, ErrSlugTaken
		}
		return Page{}, fmt.Errorf("cms: update page: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Page{}, ErrNotFound
	}
	return p, nil
}

// DeletePage implements Store.
func (s *PostgresStore) DeletePage(ctx context.Context, companyID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM cms_pages WHERE company_id = $1 AND id = $2`, companyID, id)
	if err != nil {
		return fmt.Errorf("cms: delete page: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner is the subset of pgx.Row / pgx.Rows that scanPage needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanPage scans the ten columns shared by GetPage, GetPageBySlug, and
// ListPages into a Page. blocks is unmarshaled from its raw JSONB bytes via
// encoding/json straight into []Block -- Block.Data is a plain
// map[string]any, so json.Unmarshal preserves every key the JSON object
// carries, known or not; nothing here filters or re-projects it.
func scanPage(row rowScanner) (Page, error) {
	var p Page
	var statusStr string
	var blocksRaw []byte
	if err := row.Scan(
		&p.ID, &p.CompanyID, &p.Slug, &p.Title, &blocksRaw, &statusStr,
		&p.ScheduledPublishAt, &p.PublishedAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return Page{}, err
	}
	p.Status = PageStatus(statusStr)
	if len(blocksRaw) > 0 {
		if err := json.Unmarshal(blocksRaw, &p.Blocks); err != nil {
			return Page{}, fmt.Errorf("unmarshal blocks: %w", err)
		}
	}
	return p, nil
}

// isSlugConflict reports whether err is the Postgres unique_violation raised
// by uq_cms_pages_company_slug (migration 018) -- i.e. this (company_id,
// slug) pair is already in use by another row.
func isSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
