//go:build integration

package audit_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
)

// memSink is a synchronous in-memory Sink for forwarder tests.
type memSink struct {
	mu        sync.Mutex
	received  []audit.BufferedEvent
	failCount atomic.Int32
	failUntil atomic.Int32 // # of attempts that should return an error
}

func (m *memSink) Send(_ context.Context, events []audit.BufferedEvent) error {
	if m.failCount.Add(1) <= m.failUntil.Load() {
		return errors.New("memSink: forced failure")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, events...)
	return nil
}

func (m *memSink) snapshot() []audit.BufferedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]audit.BufferedEvent, len(m.received))
	copy(out, m.received)
	return out
}

// fakeMetrics records calls to the MetricsRecorder interface.
type fakeMetrics struct {
	mu         sync.Mutex
	attempts   []bool
	resigns    []bool
	depthCalls int
	lastDepth  int64
	lastOldest time.Duration
}

func (f *fakeMetrics) RecordAttempt(_ context.Context, success bool, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, success)
}
func (f *fakeMetrics) RecordResign(_ context.Context, success bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resigns = append(f.resigns, success)
}
func (f *fakeMetrics) RecordDepth(_ context.Context, depth int64, oldest time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.depthCalls++
	f.lastDepth = depth
	f.lastOldest = oldest
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// preloadSignedEvents inserts n already-signed events directly into the buffer.
func preloadSignedEvents(t *testing.T, store *audit.PostgresBufferStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		e := audit.Event{
			CompanyID: uuid.New().String(),
			ActorID:   uuid.New().String(),
			Action:    "test.preload",
			Details:   map[string]any{"jti": fmt.Sprintf("JTI-PRELOAD-%d-%d", time.Now().UnixNano(), i)},
		}
		require.NoError(t, store.CreateSignedAuditLog(context.Background(), e, "fake.jws.signature", ""))
	}
}

// preloadUnsignedEvents inserts n events with signing_error set + empty jws.
func preloadUnsignedEvents(t *testing.T, store *audit.PostgresBufferStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		e := audit.Event{
			CompanyID: uuid.New().String(),
			ActorID:   uuid.New().String(),
			Action:    "test.unsigned",
			Details:   map[string]any{"jti": fmt.Sprintf("JTI-UNS-%d-%d", time.Now().UnixNano(), i)},
		}
		require.NoError(t, store.CreateSignedAuditLog(context.Background(), e, "", "KMS down at insert"))
	}
}

func TestForwarder_HappyPath_DrainsBuffer(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	preloadSignedEvents(t, store, 5)

	sink := &memSink{}
	metrics := &fakeMetrics{}
	fw := audit.NewForwarder(store, sink, nil, nil, metrics, quietLogger())
	fw.SetTickInterval(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)
	defer fw.Stop()

	require.Eventually(t, func() bool {
		return len(sink.snapshot()) == 5
	}, 10*time.Second, 50*time.Millisecond, "expected 5 events drained")
}

func TestForwarder_SinkFailure_RetriesWithBackoff(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	preloadSignedEvents(t, store, 2)

	sink := &memSink{}
	sink.failUntil.Store(2) // first 2 attempts fail; 3rd succeeds
	metrics := &fakeMetrics{}
	fw := audit.NewForwarder(store, sink, nil, nil, metrics, quietLogger())
	fw.SetTickInterval(50 * time.Millisecond)
	// Use a fast backoff so the test doesn't slow to 1s+ between retries.
	fw.SetBackoff(func(n int) time.Duration { return 20 * time.Millisecond })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)
	defer fw.Stop()

	require.Eventually(t, func() bool {
		return len(sink.snapshot()) == 2
	}, 15*time.Second, 50*time.Millisecond, "expected eventual drain after failures")

	// At least 2 failures must have been recorded.
	metrics.mu.Lock()
	var failCount int
	for _, ok := range metrics.attempts {
		if !ok {
			failCount++
		}
	}
	metrics.mu.Unlock()
	require.GreaterOrEqual(t, failCount, 2)
}

// fakeReSigner returns a fixed JWS string regardless of input.
type fakeReSigner struct {
	jws string
}

func (f *fakeReSigner) SignForResign(_ audit.Event) (string, error) {
	return f.jws, nil
}

func TestForwarder_ResignsUnsignedEvents(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	preloadUnsignedEvents(t, store, 2)

	sink := &memSink{}
	metrics := &fakeMetrics{}
	// Provide a re-signer that always succeeds.
	signer := &fakeReSigner{jws: "re.signed.value"}
	fw := audit.NewForwarder(store, sink, nil, signer, metrics, quietLogger())
	fw.SetTickInterval(50 * time.Millisecond)
	fw.SetResignInterval(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)
	defer fw.Stop()

	// Within ~3s, the re-signer must have run + the forwarder must have drained
	// the now-signed rows.
	require.Eventually(t, func() bool {
		return len(sink.snapshot()) == 2
	}, 15*time.Second, 100*time.Millisecond, "expected re-signer + forwarder to drain unsigned events")
}

func TestForwarder_StopGracefullyDrains(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	preloadSignedEvents(t, store, 3)

	sink := &memSink{}
	fw := audit.NewForwarder(store, sink, nil, nil, nil, quietLogger())
	fw.SetTickInterval(50 * time.Millisecond)

	ctx := context.Background()
	fw.Start(ctx)
	// Sleep briefly to let one drain happen, then Stop — must not panic.
	time.Sleep(300 * time.Millisecond)
	fw.Stop()

	require.GreaterOrEqual(t, len(sink.snapshot()), 1)
}

func TestForwarder_MetricsLoopReports(t *testing.T) {
	store, _, _ := setupBuffer(t, 0)
	preloadSignedEvents(t, store, 3)

	sink := &memSink{}
	metrics := &fakeMetrics{}
	fw := audit.NewForwarder(store, sink, nil, nil, metrics, quietLogger())
	fw.SetTickInterval(50 * time.Millisecond)
	fw.SetMetricsInterval(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)
	defer fw.Stop()

	require.Eventually(t, func() bool {
		metrics.mu.Lock()
		defer metrics.mu.Unlock()
		return metrics.depthCalls >= 1 && len(metrics.attempts) >= 1
	}, 10*time.Second, 100*time.Millisecond)
}

func TestDefaultBackoff_CapsAt1h(t *testing.T) {
	require.Equal(t, time.Duration(0), audit.DefaultBackoff(0))
	require.Equal(t, time.Second, audit.DefaultBackoff(1))
	require.Equal(t, 5*time.Second, audit.DefaultBackoff(2))
	require.Equal(t, 30*time.Second, audit.DefaultBackoff(3))
	require.Equal(t, 5*time.Minute, audit.DefaultBackoff(4))
	require.Equal(t, 30*time.Minute, audit.DefaultBackoff(5))
	require.Equal(t, time.Hour, audit.DefaultBackoff(6))
	require.Equal(t, time.Hour, audit.DefaultBackoff(10))
	require.Equal(t, time.Hour, audit.DefaultBackoff(100))
}
