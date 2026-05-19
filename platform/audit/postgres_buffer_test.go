//go:build integration

package audit_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
)

// setupBuffer spins up a PostgresBufferStore against a real Postgres pointed
// at by DATABASE_URL. Each test gets its own table name so concurrent test
// runs don't collide.
//
// Schema parity: the consumer (AOID's TRD 09-04 migration) owns the prod
// schema. This test creates an equivalent table in the public schema with the
// columns PostgresBufferStore queries against.
func setupBuffer(t *testing.T, hardCap int64) (*audit.PostgresBufferStore, *pgxpool.Pool, string) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping postgres_buffer integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	tableName := fmt.Sprintf("test_audit_buffer_%d", time.Now().UnixNano())
	createSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGSERIAL PRIMARY KEY,
			jti TEXT NOT NULL UNIQUE,
			tenant_id UUID NOT NULL,
			emitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			jws_compact TEXT,
			payload_canonical TEXT NOT NULL,
			attempts INT NOT NULL DEFAULT 0,
			last_attempt_at TIMESTAMPTZ,
			last_error TEXT,
			sent_at TIMESTAMPTZ,
			signing_error TEXT
		)
	`, tableName)
	_, err = pool.Exec(ctx, createSQL)
	require.NoError(t, err)

	t.Cleanup(func() {
		dropCtx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_, _ = pool.Exec(dropCtx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
		pool.Close()
	})

	return audit.NewPostgresBufferStore(pool, tableName, hardCap), pool, tableName
}

func insertSignedEvent(t *testing.T, store *audit.PostgresBufferStore, jti, jws, signingError string) audit.Event {
	t.Helper()
	tenantID := uuid.New()
	actorID := uuid.New()
	e := audit.Event{
		CompanyID:  tenantID.String(),
		ActorID:    actorID.String(),
		Action:     "test.event",
		Resource:   "r",
		ResourceID: "rid",
		Details:    map[string]any{"jti": jti, "k": "v"},
	}
	require.NoError(t, store.CreateSignedAuditLog(context.Background(), e, jws, signingError))
	return e
}

func TestPostgresBufferStore_CreateAndDequeue(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-1", "fake.jws.signature", "")

	events, release, err := store.DequeuePending(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "JTI-1", events[0].JTI)
	require.Equal(t, "fake.jws.signature", events[0].JWSCompact)
	require.NoError(t, release(context.Background(), true, nil))
}

func TestPostgresBufferStore_DequeueConcurrent_NoDoubleProcess(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	// Insert 20 events.
	for i := 0; i < 20; i++ {
		insertSignedEvent(t, store, fmt.Sprintf("JTI-CONCURRENT-%d", i), "x.y.z", "")
	}

	// Two goroutines call DequeuePending concurrently — they MUST receive
	// disjoint sets due to FOR UPDATE SKIP LOCKED.
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	wg.Add(2)
	for w := 0; w < 2; w++ {
		go func() {
			defer wg.Done()
			events, release, err := store.DequeuePending(context.Background(), 10)
			if err != nil {
				t.Errorf("dequeue: %v", err)
				return
			}
			mu.Lock()
			for _, ev := range events {
				seen[ev.JTI]++
			}
			mu.Unlock()
			_ = release(context.Background(), true, nil)
		}()
	}
	wg.Wait()
	for jti, count := range seen {
		require.Equal(t, 1, count, "jti %s seen %d times", jti, count)
	}
}

func TestPostgresBufferStore_ReleaseSuccess_MarksSent(t *testing.T) {
	store, pool, tableName := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-SUCCESS", "x.y.z", "")
	events, release, err := store.DequeuePending(context.Background(), 10)
	require.NoError(t, err)
	require.NoError(t, release(context.Background(), true, nil))

	var sentAt *time.Time
	err = pool.QueryRow(context.Background(), fmt.Sprintf("SELECT sent_at FROM %s WHERE id = $1", tableName), events[0].ID).Scan(&sentAt)
	require.NoError(t, err)
	require.NotNil(t, sentAt)
}

func TestPostgresBufferStore_ReleaseFailure_IncrementsAttempts(t *testing.T) {
	store, pool, tableName := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-FAIL", "x.y.z", "")
	events, release, err := store.DequeuePending(context.Background(), 10)
	require.NoError(t, err)
	require.NoError(t, release(context.Background(), false, fmt.Errorf("simulated sink failure")))

	var attempts int
	var lastError *string
	var sentAt *time.Time
	err = pool.QueryRow(context.Background(), fmt.Sprintf("SELECT attempts, last_error, sent_at FROM %s WHERE id = $1", tableName), events[0].ID).Scan(&attempts, &lastError, &sentAt)
	require.NoError(t, err)
	require.Equal(t, 1, attempts)
	require.NotNil(t, lastError)
	require.Contains(t, *lastError, "simulated sink failure")
	require.Nil(t, sentAt, "row should remain unsent after release(false)")
}

func TestPostgresBufferStore_DequeueUnsigned_OnlyMissingJWS(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	// Two unsigned (signing_error set, jws empty), one signed.
	insertSignedEvent(t, store, "JTI-UNSIGNED-1", "", "KMS down")
	insertSignedEvent(t, store, "JTI-UNSIGNED-2", "", "KMS down")
	insertSignedEvent(t, store, "JTI-SIGNED", "real.jws.sig", "")

	events, release, err := store.DequeueUnsignedForResigning(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, events, 2, "should only return rows missing jws_compact AND with signing_error")
	gotJTIs := map[string]bool{}
	for _, ev := range events {
		gotJTIs[ev.JTI] = true
	}
	require.True(t, gotJTIs["JTI-UNSIGNED-1"])
	require.True(t, gotJTIs["JTI-UNSIGNED-2"])
	require.NoError(t, release(context.Background(), true, nil))
}

func TestPostgresBufferStore_SetJWS_ClearsSigningError(t *testing.T) {
	store, pool, tableName := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-RESIGN", "", "KMS down")

	// Find the row.
	var id int64
	err := pool.QueryRow(context.Background(), fmt.Sprintf("SELECT id FROM %s WHERE jti = $1", tableName), "JTI-RESIGN").Scan(&id)
	require.NoError(t, err)

	require.NoError(t, store.SetJWS(context.Background(), id, "new.jws.value"))

	var jws *string
	var signingError *string
	err = pool.QueryRow(context.Background(), fmt.Sprintf("SELECT jws_compact, signing_error FROM %s WHERE id = $1", tableName), id).Scan(&jws, &signingError)
	require.NoError(t, err)
	require.NotNil(t, jws)
	require.Equal(t, "new.jws.value", *jws)
	require.Nil(t, signingError, "signing_error must be cleared after SetJWS")
}

func TestPostgresBufferStore_BufferDepth_CountsUnsentOnly(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	for i := 0; i < 5; i++ {
		insertSignedEvent(t, store, fmt.Sprintf("JTI-D-%d", i), "x.y.z", "")
	}
	// Mark 2 as sent.
	events, release, err := store.DequeuePending(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.NoError(t, release(context.Background(), true, nil))

	depth, err := store.BufferDepth(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(3), depth)
}

func TestPostgresBufferStore_OldestUnsentAge(t *testing.T) {
	store, pool, tableName := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-AGE", "x.y.z", "")
	// Push emitted_at into the past.
	_, err := pool.Exec(context.Background(), fmt.Sprintf("UPDATE %s SET emitted_at = now() - interval '90 seconds' WHERE jti = $1", tableName), "JTI-AGE")
	require.NoError(t, err)
	age, err := store.OldestUnsentAge(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, age, 60*time.Second)
}

func TestPostgresBufferStore_HardCap_ReturnsErrBufferFull(t *testing.T) {
	store, _, _ := setupBuffer(t, 2)
	insertSignedEvent(t, store, "JTI-CAP-1", "x.y.z", "")
	insertSignedEvent(t, store, "JTI-CAP-2", "x.y.z", "")
	// Third insert MUST fail with ErrBufferFull (depth >= hardCap).
	tenantID := uuid.New()
	actorID := uuid.New()
	e := audit.Event{
		CompanyID: tenantID.String(),
		ActorID:   actorID.String(),
		Action:    "test.cap",
		Details:   map[string]any{"jti": "JTI-CAP-3"},
	}
	err := store.CreateSignedAuditLog(context.Background(), e, "x.y.z", "")
	require.ErrorIs(t, err, audit.ErrBufferFull)
}

func TestPostgresBufferStore_Vacuum_DeletesOldSentRows(t *testing.T) {
	store, pool, tableName := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-VAC", "x.y.z", "")
	// Mark as sent 30 days ago.
	_, err := pool.Exec(context.Background(), fmt.Sprintf("UPDATE %s SET sent_at = now() - interval '30 days' WHERE jti = $1", tableName), "JTI-VAC")
	require.NoError(t, err)

	require.NoError(t, store.Vacuum(context.Background(), 7))

	var count int
	err = pool.QueryRow(context.Background(), fmt.Sprintf("SELECT count(*) FROM %s WHERE jti = $1", tableName), "JTI-VAC").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "row older than retention should be deleted")
}

// TestPostgresBufferStore_Idempotent_OnConflictDoNothing — duplicate JTI must
// be silently skipped (idempotency on retry).
func TestPostgresBufferStore_Idempotent_OnConflictDoNothing(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	insertSignedEvent(t, store, "JTI-DUP", "x.y.z", "")
	insertSignedEvent(t, store, "JTI-DUP", "different.jws", "")
	depth, err := store.BufferDepth(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), depth)
}
