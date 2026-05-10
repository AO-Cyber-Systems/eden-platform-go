package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx := context.Background()

	if err := q.Enqueue(ctx, "test.job", map[string]string{"x": "1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := store.Dequeue(ctx, []string{"test.job"}, "w1")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job == nil {
		t.Fatalf("expected job, got nil")
	}
	if job.Status != StatusRunning || job.Attempts != 1 || job.LockedBy != "w1" {
		t.Errorf("unexpected: %+v", job)
	}

	if err := store.Complete(ctx, job.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	again, _ := store.Dequeue(ctx, []string{"test.job"}, "w1")
	if again != nil {
		t.Fatalf("expected nil after complete, got %+v", again)
	}
}

func TestMemoryStorePriorityOrdering(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx := context.Background()

	if err := q.Enqueue(ctx, "t", map[string]string{"id": "low"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // ensure created_at differs
	if err := q.EnqueueHigh(ctx, "t", map[string]string{"id": "high"}); err != nil {
		t.Fatal(err)
	}

	// First dequeue should be the high-priority job.
	got, _ := store.Dequeue(ctx, []string{"t"}, "w")
	var p map[string]string
	_ = json.Unmarshal(got.Payload, &p)
	if p["id"] != "high" {
		t.Fatalf("expected high first, got %s", p["id"])
	}
}

func TestMemoryStoreDelayedNotImmediatelyEligible(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx := context.Background()

	if err := q.EnqueueDelayed(ctx, "t", "p", time.Hour); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Dequeue(ctx, []string{"t"}, "w")
	if got != nil {
		t.Fatalf("expected nil for future-scheduled job, got %+v", got)
	}
}

func TestWorkerHappyPath(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var ran atomic.Int32
	w := NewWorker(q, 1).WithPollInterval(10 * time.Millisecond)
	w.Register("test.ok", func(ctx context.Context, payload json.RawMessage) error {
		ran.Add(1)
		return nil
	})

	if err := q.Enqueue(ctx, "test.ok", map[string]string{}); err != nil {
		t.Fatal(err)
	}

	go func() { _ = w.Start(ctx) }()
	waitFor(t, time.Second, func() bool { return ran.Load() == 1 })

	jobs, _ := store.ListJobs(ctx, StatusCompleted, 10)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(jobs))
	}
}

func TestWorkerRetryThenDead(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w := NewWorker(q, 1).
		WithPollInterval(10 * time.Millisecond).
		WithBackoff(func(attempts int) time.Duration { return 20 * time.Millisecond })

	w.Register("test.fail", func(ctx context.Context, payload json.RawMessage) error {
		return errors.New("boom")
	})

	if err := q.Enqueue(ctx, "test.fail", map[string]string{}); err != nil {
		t.Fatal(err)
	}

	go func() { _ = w.Start(ctx) }()
	waitFor(t, 4*time.Second, func() bool {
		dead, _ := store.ListJobs(context.Background(), StatusDead, 10)
		return len(dead) == 1
	})
	dead, _ := store.ListJobs(context.Background(), StatusDead, 10)
	if len(dead) != 1 {
		t.Fatalf("expected 1 dead, got %d", len(dead))
	}
	if dead[0].Attempts < DefaultMaxRetries {
		t.Errorf("expected attempts >= %d, got %d", DefaultMaxRetries, dead[0].Attempts)
	}
}

func TestWorkerPanicCaught(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w := NewWorker(q, 1).
		WithPollInterval(10 * time.Millisecond).
		WithBackoff(func(int) time.Duration { return 10 * time.Millisecond })

	var panics atomic.Int32
	w.Register("test.panic", func(ctx context.Context, _ json.RawMessage) error {
		panics.Add(1)
		panic("kaboom")
	})

	if err := q.Enqueue(ctx, "test.panic", nil); err != nil {
		t.Fatal(err)
	}
	go func() { _ = w.Start(ctx) }()

	waitFor(t, 1500*time.Millisecond, func() bool {
		dead, _ := store.ListJobs(context.Background(), StatusDead, 10)
		return len(dead) == 1
	})
	if panics.Load() < int32(DefaultMaxRetries) {
		t.Errorf("expected panic to be invoked at least %d times, got %d", DefaultMaxRetries, panics.Load())
	}
}

type recordingObserver struct {
	mu                              sync.Mutex
	dequeues, completes, fails, dead int
}

func (o *recordingObserver) OnDequeue(Job)                 { o.mu.Lock(); o.dequeues++; o.mu.Unlock() }
func (o *recordingObserver) OnComplete(Job, time.Duration) { o.mu.Lock(); o.completes++; o.mu.Unlock() }
func (o *recordingObserver) OnFail(Job, error, bool)       { o.mu.Lock(); o.fails++; o.mu.Unlock() }
func (o *recordingObserver) OnDead(Job, error)             { o.mu.Lock(); o.dead++; o.mu.Unlock() }

func TestWorkerObserver(t *testing.T) {
	store := NewMemoryStore()
	q := NewQueue(store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	obs := &recordingObserver{}
	w := NewWorker(q, 1).WithPollInterval(10 * time.Millisecond).WithObserver(obs)
	w.Register("o.ok", func(ctx context.Context, _ json.RawMessage) error { return nil })

	_ = q.Enqueue(ctx, "o.ok", nil)
	go func() { _ = w.Start(ctx) }()

	waitFor(t, time.Second, func() bool {
		obs.mu.Lock()
		defer obs.mu.Unlock()
		return obs.completes == 1
	})

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if obs.dequeues != 1 || obs.completes != 1 {
		t.Errorf("observer counts: dequeues=%d completes=%d", obs.dequeues, obs.completes)
	}
}

func TestWorkerNoHandlers(t *testing.T) {
	q := NewQueue(NewMemoryStore())
	w := NewWorker(q, 1)
	if err := w.Start(context.Background()); !errors.Is(err, ErrNoHandlers) {
		t.Errorf("expected ErrNoHandlers, got %v", err)
	}
}

// waitFor polls until cond returns true or duration elapses.
func waitFor(t *testing.T, max time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", max)
}
