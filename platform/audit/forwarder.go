package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Sink consumes a batch of buffered events. Implementations:
//   - OTLPLogSink (otlp_sink.go): production — emits OTLP log records to AOAudit.
//   - test mem sink: in-memory, used by forwarder_test.go.
//
// Send is expected to be ALL-OR-NOTHING: returning an error rolls back the
// entire batch in the buffer (attempts++ + last_error). Downstream dedup via
// jti UNIQUE constraint means partial-success-then-retry is also safe.
type Sink interface {
	Send(ctx context.Context, events []BufferedEvent) error
}

// MetricsRecorder is the surface the Forwarder publishes operational metrics
// against. The consumer (e.g. AOID) hooks this to OTel meters; tests use a
// fake to assert call ordering.
type MetricsRecorder interface {
	RecordAttempt(ctx context.Context, success bool, duration time.Duration)
	RecordResign(ctx context.Context, success bool)
	RecordDepth(ctx context.Context, depth int64, oldestAge time.Duration)
}

// ReSigner is the minimum surface the Forwarder's re-signer pump needs. The
// concrete SignedStore implements this via its SignForResign method.
//
// A separate interface (rather than typing the field as *SignedStore) keeps
// the package testable — forwarder_test.go provides a fakeReSigner that
// doesn't need a real KMS.
type ReSigner interface {
	SignForResign(e Event) (string, error)
}

// BackoffPolicy returns the sleep duration to apply after the Nth
// consecutive failure. n starts at 1 on the first failure; n=0 is the
// "success/just-started" zero state.
type BackoffPolicy func(consecutiveFailures int) time.Duration

// DefaultBackoff implements 1s -> 5s -> 30s -> 5m -> 30m -> 1h capped at 1h
// (per TRD 09-02 research §D.4).
//
// The cap is intentional: beyond 1h the AOAudit endpoint is almost certainly
// down (not jittered), and ops should be paging. Backing off further just
// extends the unsent-age the alert threshold cares about.
func DefaultBackoff(n int) time.Duration {
	switch {
	case n <= 0:
		return 0
	case n == 1:
		return time.Second
	case n == 2:
		return 5 * time.Second
	case n == 3:
		return 30 * time.Second
	case n == 4:
		return 5 * time.Minute
	case n == 5:
		return 30 * time.Minute
	default:
		return time.Hour
	}
}

// Forwarder drives three concurrent loops on top of a PostgresBufferStore:
//
//  1. forwardLoop (5s default tick): DequeuePending → Sink.Send → release.
//  2. resignLoop (30s default tick): DequeueUnsignedForResigning → ReSigner →
//     SetJWS → release. Skipped if no ReSigner was provided.
//  3. metricsLoop (30s default tick): BufferDepth + OldestUnsentAge →
//     MetricsRecorder.RecordDepth. Skipped if no MetricsRecorder.
//
// All loops are cancelled by Stop() or by the context passed to Start().
type Forwarder struct {
	buffer   *PostgresBufferStore
	sink     Sink
	signer   ReSigner // re-signer for unsigned rows (optional)
	resigner ReSigner // alias-style field; kept for clarity in tests

	backoff             BackoffPolicy
	metrics             MetricsRecorder
	logger              *slog.Logger
	batchSize           int
	tickInterval        time.Duration
	resignInterval      time.Duration
	metricsInterval     time.Duration
	consecutiveFailures int

	done chan struct{}
	wg   sync.WaitGroup
}

// NewForwarder constructs a Forwarder. Pass nil for signer/resigner to
// disable the re-signer pump; pass nil for metrics to disable the metrics
// loop. logger=nil falls back to slog.Default().
//
// The `signer` parameter is retained for compatibility with the TRD signature;
// the actual re-signing path uses `resigner` which is typically a *SignedStore.
// Both are aliased to the same value at construction.
func NewForwarder(
	buffer *PostgresBufferStore,
	sink Sink,
	_ any, // signer placeholder — kept for TRD signature compatibility
	resigner ReSigner,
	metrics MetricsRecorder,
	logger *slog.Logger,
) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{
		buffer:          buffer,
		sink:            sink,
		resigner:        resigner,
		backoff:         DefaultBackoff,
		metrics:         metrics,
		logger:          logger,
		batchSize:       100,
		tickInterval:    5 * time.Second,
		resignInterval:  30 * time.Second,
		metricsInterval: 30 * time.Second,
		done:            make(chan struct{}),
	}
}

// SetTickInterval overrides the default 5s forward-loop tick. Use shorter
// intervals in tests; production should keep the 5s default.
func (f *Forwarder) SetTickInterval(d time.Duration) { f.tickInterval = d }

// SetResignInterval overrides the default 30s resign-loop tick.
func (f *Forwarder) SetResignInterval(d time.Duration) { f.resignInterval = d }

// SetMetricsInterval overrides the default 30s metrics-loop tick.
func (f *Forwarder) SetMetricsInterval(d time.Duration) { f.metricsInterval = d }

// SetBackoff overrides the default backoff policy. Useful for tests that
// want to skip the 1s+ floor.
func (f *Forwarder) SetBackoff(b BackoffPolicy) { f.backoff = b }

// SetBatchSize overrides the default 100-row batch size.
func (f *Forwarder) SetBatchSize(n int) {
	if n > 0 {
		f.batchSize = n
	}
}

// Start spawns the three goroutines. They run until Stop() or ctx
// cancellation, whichever comes first.
//
// Start can only be called once per Forwarder — calling it twice causes a
// race on the WaitGroup. (Construct a new Forwarder if needed.)
func (f *Forwarder) Start(ctx context.Context) {
	f.wg.Add(3)
	go f.forwardLoop(ctx)
	go f.resignLoop(ctx)
	go f.metricsLoop(ctx)
}

// Stop signals the goroutines to exit and waits for them to finish. Safe to
// call multiple times — second call is a no-op.
func (f *Forwarder) Stop() {
	// Guard double-close.
	select {
	case <-f.done:
		// already closed
	default:
		close(f.done)
	}
	f.wg.Wait()
}

// forwardLoop drains the buffer to the sink on each tick.
func (f *Forwarder) forwardLoop(ctx context.Context) {
	defer f.wg.Done()
	t := time.NewTicker(f.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-t.C:
			f.drainOnce(ctx)
		}
	}
}

func (f *Forwarder) drainOnce(ctx context.Context) {
	start := time.Now()
	events, release, err := f.buffer.DequeuePending(ctx, f.batchSize)
	if err != nil {
		f.logger.Warn("audit forwarder: dequeue failed", "error", err)
		return
	}
	if len(events) == 0 {
		_ = release(ctx, true, nil)
		// Empty queue: zero out the failure counter so a future failure
		// starts from backoff[1] again.
		f.consecutiveFailures = 0
		return
	}
	sendErr := f.sink.Send(ctx, events)
	dur := time.Since(start)
	if sendErr != nil {
		f.consecutiveFailures++
		f.logger.Warn("audit forwarder: sink send failed",
			"count", len(events),
			"error", sendErr,
			"consecutive_failures", f.consecutiveFailures,
		)
		_ = release(ctx, false, sendErr)
		if f.metrics != nil {
			f.metrics.RecordAttempt(ctx, false, dur)
		}
		sleep := f.backoff(f.consecutiveFailures)
		if sleep > 0 {
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
			case <-f.done:
			}
		}
		return
	}
	f.consecutiveFailures = 0
	_ = release(ctx, true, nil)
	if f.metrics != nil {
		f.metrics.RecordAttempt(ctx, true, dur)
	}
}

// resignLoop re-signs events that landed in the buffer unsigned (KMS
// transient failure at insert time). Each row is signed individually so a
// single bad row doesn't block the others. Successful re-signs are
// persisted via SetJWS BEFORE release(true) is called, so the row leaves the
// dequeue tx with jws_compact set + signing_error NULL.
func (f *Forwarder) resignLoop(ctx context.Context) {
	defer f.wg.Done()
	if f.resigner == nil {
		return // re-signer disabled by construction
	}
	t := time.NewTicker(f.resignInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-t.C:
			f.resignOnce(ctx)
		}
	}
}

func (f *Forwarder) resignOnce(ctx context.Context) {
	events, release, handle, err := f.buffer.DequeueUnsignedForResigningWithTx(ctx, f.batchSize)
	if err != nil {
		f.logger.Warn("audit resigner: dequeue failed", "error", err)
		return
	}
	if len(events) == 0 {
		_ = release(ctx, true, nil)
		return
	}
	successCount := 0
	for _, be := range events {
		// Re-parse the canonical payload into an Event so the signer can
		// produce a bit-identical canonical input.
		e, _, _, uerr := UnmarshalCanonical(be.PayloadCanon)
		if uerr != nil {
			f.logger.Warn("audit resigner: unmarshal failed", "id", be.ID, "error", uerr)
			continue
		}
		// jti is preserved by UnmarshalCanonical (it round-trips through
		// Details["jti"]).
		jws, signErr := f.resigner.SignForResign(e)
		if signErr != nil {
			f.logger.Warn("audit resigner: sign failed", "id", be.ID, "error", signErr)
			if f.metrics != nil {
				f.metrics.RecordResign(ctx, false)
			}
			continue
		}
		// IMPORTANT: SetJWSInTx executes inside the dequeue's transaction.
		// SetJWS (via the pool) would deadlock waiting for the FOR UPDATE
		// lock that the dequeue tx still holds.
		if err := f.buffer.SetJWSInTx(ctx, handle, be.ID, jws); err != nil {
			f.logger.Warn("audit resigner: SetJWSInTx failed", "id", be.ID, "error", err)
			if f.metrics != nil {
				f.metrics.RecordResign(ctx, false)
			}
			continue
		}
		successCount++
		if f.metrics != nil {
			f.metrics.RecordResign(ctx, true)
		}
		f.logger.Info("audit resigner: event resigned",
			"id", be.ID,
			"jti", be.JTI,
			"action", string(ActionEventResigned),
		)
	}
	_ = release(ctx, true, nil)
	_ = successCount
}

// metricsLoop polls BufferDepth + OldestUnsentAge on a schedule. The
// MetricsRecorder forwards them to OTel meters.
func (f *Forwarder) metricsLoop(ctx context.Context) {
	defer f.wg.Done()
	if f.metrics == nil {
		return
	}
	t := time.NewTicker(f.metricsInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.done:
			return
		case <-t.C:
			depth, err := f.buffer.BufferDepth(ctx)
			if err != nil {
				continue
			}
			age, _ := f.buffer.OldestUnsentAge(ctx)
			f.metrics.RecordDepth(ctx, depth, age)
		}
	}
}
