package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrBufferFull is returned by CreateSignedAuditLog when the buffer's row
// count exceeds the configured hard cap. New events are dropped until the
// Forwarder drains existing rows.
//
// This is a LAST-RESORT condition. Operators should have caught the
// upstream failure (AOAudit OTLP endpoint down) long before via the
// OldestUnsentAge metric alert.
var ErrBufferFull = errors.New("audit: buffer at hard cap")

// BufferedEvent is one row from the buffer table, ready for forwarding.
//
// All fields map 1:1 to a row from the audit_buffer table the consumer
// creates. The struct is intentionally lossless — the verifier-side
// re-canonicalization needs every field intact.
type BufferedEvent struct {
	ID            int64
	JTI           string
	TenantID      uuid.UUID
	EmittedAt     time.Time
	JWSCompact    string
	PayloadCanon  []byte
	Attempts      int
	LastAttemptAt *time.Time
	LastError     string
	SigningError  string
}

// ReleaseFunc finalizes a dequeue transaction.
//
// success=true: marks rows as sent_at=now() (forward path) or clears
// signing_error (resign path — see DequeueUnsignedForResigning).
// success=false: increments attempts + persists last_error; sendErr is the
// concrete error returned by the sink/signer that justifies the retry.
//
// The function COMMITS or ROLLBACKS the underlying transaction. Calling it
// is mandatory; not calling it leaves the rows locked until the connection
// times out.
type ReleaseFunc func(ctx context.Context, success bool, sendErr error) error

// PostgresBufferStore implements both AuditStore (legacy callers) and
// SignedEventStore (preferred — SignedStore detects this) by persisting
// audit events into a configured table.
//
// The store does NOT own the migration. The consumer (e.g. AOID's TRD 09-04
// migration) creates the table with the columns referenced here. Construct
// with the schema-qualified table name (e.g. "aoid.audit_buffer").
//
// Required schema (consumer responsibility):
//
//	CREATE TABLE <tableName> (
//	  id BIGSERIAL PRIMARY KEY,
//	  jti TEXT NOT NULL UNIQUE,
//	  tenant_id UUID NOT NULL,
//	  emitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
//	  jws_compact TEXT,
//	  payload_canonical TEXT NOT NULL,
//	  attempts INT NOT NULL DEFAULT 0,
//	  last_attempt_at TIMESTAMPTZ,
//	  last_error TEXT,
//	  sent_at TIMESTAMPTZ,
//	  signing_error TEXT
//	);
//	CREATE INDEX <table>_pending_idx ON <table> (emitted_at)
//	  WHERE sent_at IS NULL;
type PostgresBufferStore struct {
	pool      *pgxpool.Pool
	tableName string
	hardCap   int64
}

// NewPostgresBufferStore constructs a PostgresBufferStore.
//
// tableName must be the schema-qualified table identifier the consumer has
// created (e.g. "aoid.audit_buffer"). It is interpolated DIRECTLY into SQL —
// callers MUST trust the source (boot config, not user input).
//
// hardCap is the row-count above which CreateSignedAuditLog returns
// ErrBufferFull. Pass 0 to disable the cap (test/dev). Production should set
// a value sized to local disk capacity (default suggestion: 1_000_000).
func NewPostgresBufferStore(pool *pgxpool.Pool, tableName string, hardCap int64) *PostgresBufferStore {
	return &PostgresBufferStore{pool: pool, tableName: tableName, hardCap: hardCap}
}

// CreateSignedAuditLog implements SignedEventStore. It INSERTs the event with
// its JWS Compact + canonical payload + (optional) signingError marker, with
// ON CONFLICT (jti) DO NOTHING so repeated calls with the same jti are no-ops
// (idempotency on retry).
//
// Hard-cap behavior: if hardCap > 0 and the current depth >= hardCap, returns
// ErrBufferFull and DOES NOT insert. Forwarder must drain before new writes
// resume.
func (s *PostgresBufferStore) CreateSignedAuditLog(ctx context.Context, e Event, jwsCompact, signingError string) error {
	if s.hardCap > 0 {
		depth, err := s.BufferDepth(ctx)
		if err == nil && depth >= s.hardCap {
			return ErrBufferFull
		}
	}
	tenantID, err := uuid.Parse(e.CompanyID)
	if err != nil {
		return fmt.Errorf("audit: parse tenant_id %q: %w", e.CompanyID, err)
	}
	jti := eventJTI(e)
	if jti == "" {
		return errors.New("audit: event missing jti — SignedStore must populate Details[\"jti\"] before delegating")
	}
	// Canonicalize the event for storage. We use iat=emitted-now() seconds; the
	// row's emitted_at column carries the high-precision timestamp separately.
	payload, err := MarshalCanonical(e, "aoid", time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("audit: canonicalize: %w", err)
	}
	q := fmt.Sprintf(`
		INSERT INTO %s (jti, tenant_id, jws_compact, payload_canonical, signing_error)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''))
		ON CONFLICT (jti) DO NOTHING
	`, s.tableName)
	_, err = s.pool.Exec(ctx, q, jti, tenantID, jwsCompact, string(payload), signingError)
	if err != nil {
		return fmt.Errorf("audit: insert buffered event: %w", err)
	}
	return nil
}

// CreateAuditLog implements AuditStore (legacy fallback). Reconstructs a
// minimal Event and delegates to CreateSignedAuditLog. The JWS will be empty
// in this path — non-signing-aware callers don't pass one.
//
// Most callers should go through SignedStore, which produces a JWS and uses
// the CreateSignedAuditLog path directly.
func (s *PostgresBufferStore) CreateAuditLog(ctx context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	detailsMap := map[string]any{}
	if len(details) > 0 {
		_ = unmarshalJSONInto(details, &detailsMap)
	}
	jti, _ := detailsMap["jti"].(string)
	if jti == "" {
		jti = generateJTI()
		detailsMap["jti"] = jti
	}
	e := Event{
		CompanyID:  companyID.String(),
		ActorID:    actorID.String(),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  ipAddress,
		Details:    detailsMap,
	}
	return s.CreateSignedAuditLog(ctx, e, "", "")
}

// DequeuePending returns up to batchSize buffered events ready for forwarding
// (sent_at IS NULL AND jws_compact IS NOT NULL). Acquires FOR UPDATE SKIP
// LOCKED row locks under a transaction. The caller MUST invoke the returned
// ReleaseFunc to commit/rollback.
//
// Returns (nil, noOpRelease, nil) on empty queue.
func (s *PostgresBufferStore) DequeuePending(ctx context.Context, batchSize int) ([]BufferedEvent, ReleaseFunc, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("audit: begin dequeue tx: %w", err)
	}
	q := fmt.Sprintf(`
		SELECT id, jti, tenant_id, emitted_at, COALESCE(jws_compact, ''),
		       payload_canonical, attempts, last_attempt_at,
		       COALESCE(last_error, ''), COALESCE(signing_error, '')
		FROM %s
		WHERE sent_at IS NULL AND jws_compact IS NOT NULL AND jws_compact <> ''
		ORDER BY emitted_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, s.tableName)
	out, err := scanBufferedRows(ctx, tx, q, batchSize)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	release := s.makeReleaseSent(tx, out)
	return out, release, nil
}

// DequeueUnsignedForResigning returns up to batchSize events that landed
// unsigned (signing_error IS NOT NULL AND (jws_compact IS NULL OR =”))
// — the re-signer pump's input set.
func (s *PostgresBufferStore) DequeueUnsignedForResigning(ctx context.Context, batchSize int) ([]BufferedEvent, ReleaseFunc, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("audit: begin resign tx: %w", err)
	}
	q := fmt.Sprintf(`
		SELECT id, jti, tenant_id, emitted_at, COALESCE(jws_compact, ''),
		       payload_canonical, attempts, last_attempt_at,
		       COALESCE(last_error, ''), COALESCE(signing_error, '')
		FROM %s
		WHERE sent_at IS NULL
		  AND (jws_compact IS NULL OR jws_compact = '')
		  AND signing_error IS NOT NULL
		ORDER BY emitted_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, s.tableName)
	out, err := scanBufferedRows(ctx, tx, q, batchSize)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	release := s.makeReleaseResign(tx, out)
	return out, release, nil
}

func (s *PostgresBufferStore) makeReleaseSent(tx pgx.Tx, out []BufferedEvent) ReleaseFunc {
	return func(rctx context.Context, success bool, sendErr error) error {
		// Tx is auto-rolled-back on error path; success path commits.
		if len(out) == 0 {
			return tx.Commit(rctx)
		}
		ids := make([]int64, 0, len(out))
		for _, ev := range out {
			ids = append(ids, ev.ID)
		}
		var upd string
		var args []any
		if success {
			upd = fmt.Sprintf(`UPDATE %s SET sent_at = now() WHERE id = ANY($1)`, s.tableName)
			args = []any{ids}
		} else {
			errStr := ""
			if sendErr != nil {
				errStr = sendErr.Error()
			}
			upd = fmt.Sprintf(`UPDATE %s SET attempts = attempts + 1, last_attempt_at = now(), last_error = $2 WHERE id = ANY($1)`, s.tableName)
			args = []any{ids, errStr}
		}
		if _, err := tx.Exec(rctx, upd, args...); err != nil {
			_ = tx.Rollback(rctx)
			return fmt.Errorf("audit: release update: %w", err)
		}
		return tx.Commit(rctx)
	}
}

func (s *PostgresBufferStore) makeReleaseResign(tx pgx.Tx, out []BufferedEvent) ReleaseFunc {
	return func(rctx context.Context, success bool, sendErr error) error {
		if len(out) == 0 {
			return tx.Commit(rctx)
		}
		ids := make([]int64, 0, len(out))
		for _, ev := range out {
			ids = append(ids, ev.ID)
		}
		var upd string
		var args []any
		if success {
			// SetJWSInTx already applied the per-row jws_compact updates
			// inside this transaction. We finalize by clearing signing_error
			// for the entire batch and committing.
			upd = fmt.Sprintf(`UPDATE %s SET signing_error = NULL WHERE id = ANY($1)`, s.tableName)
			args = []any{ids}
		} else {
			errStr := ""
			if sendErr != nil {
				errStr = sendErr.Error()
			}
			upd = fmt.Sprintf(`UPDATE %s SET attempts = attempts + 1, last_attempt_at = now(), last_error = $2 WHERE id = ANY($1)`, s.tableName)
			args = []any{ids, errStr}
		}
		if _, err := tx.Exec(rctx, upd, args...); err != nil {
			_ = tx.Rollback(rctx)
			return fmt.Errorf("audit: release resign update: %w", err)
		}
		return tx.Commit(rctx)
	}
}

// DequeueUnsignedForResigningWithTx mirrors DequeueUnsignedForResigning but
// also returns a sentinel handle the caller passes to SetJWSInTx so the
// per-row UPDATE happens inside the dequeue transaction. Tests + production
// forwarder use this path.
//
// Returns the same (events, release, err) shape; events carry an extra
// hidden tx-handle that SetJWSInTx looks up.
func (s *PostgresBufferStore) DequeueUnsignedForResigningWithTx(ctx context.Context, batchSize int) ([]BufferedEvent, ReleaseFunc, *ResignTx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("audit: begin resign tx: %w", err)
	}
	q := fmt.Sprintf(`
		SELECT id, jti, tenant_id, emitted_at, COALESCE(jws_compact, ''),
		       payload_canonical, attempts, last_attempt_at,
		       COALESCE(last_error, ''), COALESCE(signing_error, '')
		FROM %s
		WHERE sent_at IS NULL
		  AND (jws_compact IS NULL OR jws_compact = '')
		  AND signing_error IS NOT NULL
		ORDER BY emitted_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, s.tableName)
	out, err := scanBufferedRows(ctx, tx, q, batchSize)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, nil, err
	}
	handle := &ResignTx{tx: tx, table: s.tableName}
	release := s.makeReleaseResign(tx, out)
	return out, release, handle, nil
}

// ResignTx is an opaque handle the forwarder uses with SetJWSInTx so the
// per-row JWS UPDATE happens inside the dequeue transaction (avoiding the
// SetJWS-via-pool vs FOR UPDATE-tx deadlock).
type ResignTx struct {
	tx    pgx.Tx
	table string
}

// SetJWSInTx persists a re-signed JWS for one row inside the dequeue tx.
// Called by the forwarder between DequeueUnsignedForResigningWithTx and
// ReleaseFunc(true). All per-row SetJWSInTx calls happen before the single
// ReleaseFunc call commits.
func (s *PostgresBufferStore) SetJWSInTx(ctx context.Context, handle *ResignTx, id int64, jwsCompact string) error {
	if handle == nil || handle.tx == nil {
		return errors.New("audit: SetJWSInTx: nil handle")
	}
	q := fmt.Sprintf(`UPDATE %s SET jws_compact = $2 WHERE id = $1`, handle.table)
	_, err := handle.tx.Exec(ctx, q, id, jwsCompact)
	if err != nil {
		return fmt.Errorf("audit: SetJWSInTx id=%d: %w", id, err)
	}
	return nil
}

// SetJWS persists a re-signed JWS for a buffered row. Called by the forwarder
// resigner pump between DequeueUnsignedForResigning and ReleaseFunc(true).
// Clears signing_error in the same UPDATE so the row is observably "signed".
//
// Operates on the pool (its own transaction), not the dequeue tx. The
// dequeue's FOR UPDATE row lock is held until ReleaseFunc commits — but
// SetJWS uses the same row, and PostgreSQL's MVCC handles the conflict
// internally because SetJWS executes within the same connection's outer
// transaction context only if the dequeue tx is on the same conn.
// Pragmatically, the forwarder calls SetJWS via the pool (independent
// connection) right before ReleaseFunc; the SetJWS UPDATE WAITS for the
// dequeue tx to commit. That's the intended interleave — it serializes the
// re-sign without dropping the row lock.
//
// Cleaner alternative for the future: extend ReleaseFunc to accept the new
// JWS string and do everything inside the dequeue tx. Kept simple for v1.
func (s *PostgresBufferStore) SetJWS(ctx context.Context, id int64, jwsCompact string) error {
	q := fmt.Sprintf(`UPDATE %s SET jws_compact = $2, signing_error = NULL WHERE id = $1`, s.tableName)
	_, err := s.pool.Exec(ctx, q, id, jwsCompact)
	if err != nil {
		return fmt.Errorf("audit: SetJWS id=%d: %w", id, err)
	}
	return nil
}

// BufferDepth returns the count of pending (unsent) events. Hook this to an
// OTel gauge in the consumer (AOID) so ops dashboards can alert on growth.
func (s *PostgresBufferStore) BufferDepth(ctx context.Context) (int64, error) {
	q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE sent_at IS NULL`, s.tableName)
	var n int64
	if err := s.pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("audit: BufferDepth: %w", err)
	}
	return n, nil
}

// OldestUnsentAge returns the age of the oldest unsent event in seconds
// resolution. Returns 0 if the buffer is empty.
//
// This is the most important operational metric: if it grows beyond the
// alert threshold (suggested: 24h), the AOAudit endpoint is presumed down.
func (s *PostgresBufferStore) OldestUnsentAge(ctx context.Context) (time.Duration, error) {
	q := fmt.Sprintf(`SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(emitted_at))), 0)::bigint FROM %s WHERE sent_at IS NULL`, s.tableName)
	var secs int64
	if err := s.pool.QueryRow(ctx, q).Scan(&secs); err != nil {
		return 0, fmt.Errorf("audit: OldestUnsentAge: %w", err)
	}
	if secs < 0 {
		secs = 0
	}
	return time.Duration(secs) * time.Second, nil
}

// Vacuum deletes sent rows older than retentionDays and runs VACUUM ANALYZE.
// Called by the consumer's daily scheduler (e.g. AOID's lifecycle automation
// job). Tries to keep the buffer table sized for "recent operational
// observability", NOT as an archive — the actual archive is AOAudit.
//
// Default retention: 7 days post-sent (suggested in TRD 09-02).
func (s *PostgresBufferStore) Vacuum(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return errors.New("audit: retentionDays must be > 0")
	}
	// fmt.Sprintf is used for the interval (parameterized intervals in pgx
	// require concatenation here because the interval literal cannot be a
	// placeholder in a non-prepared statement on the path we use).
	delQ := fmt.Sprintf(`DELETE FROM %s WHERE sent_at IS NOT NULL AND sent_at < now() - interval '%d days'`, s.tableName, retentionDays)
	if _, err := s.pool.Exec(ctx, delQ); err != nil {
		return fmt.Errorf("audit: vacuum delete: %w", err)
	}
	// VACUUM cannot run inside a transaction; use pool.Exec directly. Some
	// hosted Postgres providers (RDS, Azure) restrict full VACUUM — ANALYZE
	// is the safe subset.
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`VACUUM ANALYZE %s`, s.tableName))
	if err != nil {
		// VACUUM may be denied (e.g. managed Postgres without superuser);
		// fall back to ANALYZE only.
		if strings.Contains(strings.ToLower(err.Error()), "permission") {
			_, err2 := s.pool.Exec(ctx, fmt.Sprintf(`ANALYZE %s`, s.tableName))
			if err2 != nil {
				return fmt.Errorf("audit: ANALYZE fallback: %w", err2)
			}
			return nil
		}
		return fmt.Errorf("audit: VACUUM ANALYZE: %w", err)
	}
	return nil
}

// scanBufferedRows runs the SELECT query in tx and returns the decoded rows.
// Used by both DequeuePending and DequeueUnsignedForResigning.
func scanBufferedRows(ctx context.Context, tx pgx.Tx, q string, batchSize int) ([]BufferedEvent, error) {
	rows, err := tx.Query(ctx, q, batchSize)
	if err != nil {
		return nil, fmt.Errorf("audit: dequeue query: %w", err)
	}
	defer rows.Close()
	var out []BufferedEvent
	for rows.Next() {
		var be BufferedEvent
		var lastAttempt *time.Time
		if err := rows.Scan(
			&be.ID, &be.JTI, &be.TenantID, &be.EmittedAt,
			&be.JWSCompact, &be.PayloadCanon, &be.Attempts,
			&lastAttempt, &be.LastError, &be.SigningError,
		); err != nil {
			return nil, fmt.Errorf("audit: dequeue scan: %w", err)
		}
		be.LastAttemptAt = lastAttempt
		out = append(out, be)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: dequeue rows: %w", err)
	}
	return out, nil
}

// unmarshalJSONInto is a tiny helper centralizing the JSON-decode shape used
// by CreateAuditLog (legacy fallback). Wrapping it lets us swap encoders
// later (e.g. for performance) without touching callers.
func unmarshalJSONInto(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

// Compile-time interface assertions.
var _ AuditStore = (*PostgresBufferStore)(nil)
var _ SignedEventStore = (*PostgresBufferStore)(nil)
