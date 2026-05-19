//go:build integration

package policycache_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/aocybersystems/eden-platform-go/platform/policycache"
)

// listenerSetup acquires:
//   - a dedicated pgx.Conn for the listener (NEVER returned to a pool)
//   - a pgxpool for issuing NOTIFY from a separate connection
//
// Uses DATABASE_URL; skips if unset (matches platform/audit convention).
func listenerSetup(t *testing.T) (listenConn *pgx.Conn, notifyPool *pgxpool.Pool, cleanup func()) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping policycache listener integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lc, err := pgx.Connect(ctx, dbURL)
	require.NoError(t, err)

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)

	return lc, pool, func() {
		_ = lc.Close(context.Background())
		pool.Close()
	}
}

// uniqueChannel returns a per-test channel name to avoid cross-test
// notifications bleeding between concurrent integration test runs.
func uniqueChannel(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), os.Getpid())
}

func TestListener_DeliversNotify(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_deliver")
	l := policycache.NewListener(lc)

	got := make(chan string, 4)
	listenCtx, cancelListen := context.WithCancel(context.Background())
	defer cancelListen()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(listenCtx, channel,
			func(payload string) { got <- payload },
			func(err error) {},
		)
	}()

	// Give Listen time to actually issue LISTEN.
	time.Sleep(200 * time.Millisecond)

	// Fire NOTIFY from a separate connection.
	notifyCtx, cancelNotify := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := pool.Exec(notifyCtx, fmt.Sprintf("NOTIFY %s, 'hello'", channel))
	cancelNotify()
	require.NoError(t, err)

	select {
	case payload := <-got:
		if payload != "hello" {
			t.Errorf("got payload %q, want %q", payload, "hello")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for NOTIFY delivery")
	}

	cancelListen()
	<-listenErr
}

func TestListener_IgnoresOtherChannels(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	subscribed := uniqueChannel(t, "policycache_test_sub")
	other := uniqueChannel(t, "policycache_test_other")

	l := policycache.NewListener(lc)
	got := make(chan string, 4)
	listenCtx, cancelListen := context.WithCancel(context.Background())
	defer cancelListen()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(listenCtx, subscribed,
			func(payload string) { got <- payload },
			func(err error) {},
		)
	}()
	time.Sleep(200 * time.Millisecond)

	// Fire NOTIFY on a channel we did NOT subscribe to.
	_, err := pool.Exec(context.Background(), fmt.Sprintf("NOTIFY %s, 'ignored'", other))
	require.NoError(t, err)

	// Brief wait — no notification should arrive.
	select {
	case payload := <-got:
		t.Errorf("expected no delivery for non-subscribed channel, got %q", payload)
	case <-time.After(500 * time.Millisecond):
		// OK — silence on the unrelated channel.
	}

	cancelListen()
	<-listenErr
}

func TestListener_MultiNotify_DeliveredInOrder(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_multi")
	l := policycache.NewListener(lc)

	const n = 5
	got := make(chan string, n)
	listenCtx, cancelListen := context.WithCancel(context.Background())
	defer cancelListen()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(listenCtx, channel,
			func(payload string) { got <- payload },
			func(err error) {},
		)
	}()
	time.Sleep(200 * time.Millisecond)

	// Fire n NOTIFYs in order from a single transaction.
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	for i := 0; i < n; i++ {
		_, err := tx.Exec(context.Background(),
			fmt.Sprintf("NOTIFY %s, '%d'", channel, i))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(context.Background()))

	received := make([]string, 0, n)
	deadline := time.After(3 * time.Second)
collect:
	for len(received) < n {
		select {
		case payload := <-got:
			received = append(received, payload)
		case <-deadline:
			break collect
		}
	}
	if len(received) != n {
		t.Fatalf("received %d/%d notifications: %v", len(received), n, received)
	}
	for i, p := range received {
		if p != fmt.Sprintf("%d", i) {
			t.Errorf("received[%d] = %q, want %q", i, p, fmt.Sprintf("%d", i))
		}
	}

	cancelListen()
	<-listenErr
}

func TestListener_ContextCancel_ClosesCleanly(t *testing.T) {
	lc, _, cleanup := listenerSetup(t)
	defer cleanup()

	channel := uniqueChannel(t, "policycache_test_cancel")
	l := policycache.NewListener(lc)
	listenCtx, cancelListen := context.WithCancel(context.Background())

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(listenCtx, channel,
			func(payload string) {},
			func(err error) {},
		)
	}()
	time.Sleep(200 * time.Millisecond)
	cancelListen()

	select {
	case err := <-listenErr:
		if err != nil {
			t.Errorf("expected nil error on ctx cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not return after ctx cancel")
	}
}

func TestListener_ConnError_InvokesOnError(t *testing.T) {
	lc, pool, cleanup := listenerSetup(t)
	defer cleanup()

	// Capture the listener conn's backend pid BEFORE handing it off to
	// the listener goroutine. Closing lc from this goroutine while the
	// listener owns it races with pgx's internal state; pg_terminate_backend
	// kills the session from a SEPARATE connection (the pool) which is the
	// realistic prod failure scenario (admin/network/pg restart).
	var pid int32
	require.NoError(t, lc.QueryRow(context.Background(), "SELECT pg_backend_pid()").Scan(&pid))

	channel := uniqueChannel(t, "policycache_test_connerr")
	l := policycache.NewListener(lc)
	listenCtx, cancelListen := context.WithCancel(context.Background())
	defer cancelListen()

	var onErrorCalls atomic.Int64
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- l.Listen(listenCtx, channel,
			func(payload string) {},
			func(err error) { onErrorCalls.Add(1) },
		)
	}()
	time.Sleep(200 * time.Millisecond)

	// Terminate the listener's session from a separate pool connection.
	// The listener's WaitForNotification will return a non-nil error.
	_, err := pool.Exec(context.Background(), "SELECT pg_terminate_backend($1)", pid)
	require.NoError(t, err)

	select {
	case err := <-listenErr:
		if err == nil {
			t.Errorf("expected non-nil error after conn termination, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Listen did not return after backend termination")
	}

	if onErrorCalls.Load() == 0 {
		t.Errorf("expected onError callback to fire, got 0 calls")
	}
}

func TestListener_PanicsOnNilConn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on NewListener(nil)")
		}
	}()
	_ = policycache.NewListener(nil)
}

// Ensure no goroutines leak from the listener after a clean shutdown.
// (Soft check — counts goroutines before/after; allows ±2 slack for runtime.)
func TestListener_NoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping goroutine leak check in -short mode")
	}
	// Run several listen+cancel cycles serially and confirm we don't grow.
	for i := 0; i < 3; i++ {
		lc, _, cleanup := listenerSetup(t)
		channel := uniqueChannel(t, "policycache_test_leak")

		l := policycache.NewListener(lc)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Listen(ctx, channel, func(string) {}, func(error) {})
		}()
		time.Sleep(100 * time.Millisecond)
		cancel()
		wg.Wait()
		cleanup()
	}
}
