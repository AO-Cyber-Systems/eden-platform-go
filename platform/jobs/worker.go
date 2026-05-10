package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNoHandlers is returned when Start is called with no registered handlers.
var ErrNoHandlers = errors.New("jobs: worker has no registered handlers")

// Observer receives lifecycle events for observability hooks (metrics,
// tracing, structured logging). All methods must be non-blocking — invoke
// async if doing IO.
type Observer interface {
	OnDequeue(job Job)
	OnComplete(job Job, dur time.Duration)
	OnFail(job Job, err error, willRetry bool)
	OnDead(job Job, err error)
}

// nopObserver is the default; logs at debug level only.
type nopObserver struct{}

func (nopObserver) OnDequeue(Job)                    {}
func (nopObserver) OnComplete(Job, time.Duration)    {}
func (nopObserver) OnFail(Job, error, bool)          {}
func (nopObserver) OnDead(Job, error)                {}

// Worker dequeues and executes jobs. Multiple Workers can run concurrently
// against the same Store (Postgres SKIP LOCKED, MemoryStore mutex).
type Worker struct {
	queue        *Queue
	handlers     map[string]Handler
	concurrency  int
	workerID     string
	pollInterval time.Duration
	jobTimeout   time.Duration
	observer     Observer
	backoff      func(attempts int) time.Duration
}

// NewWorker constructs a worker with concurrency parallel goroutines.
func NewWorker(queue *Queue, concurrency int) *Worker {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Worker{
		queue:        queue,
		handlers:     make(map[string]Handler),
		concurrency:  concurrency,
		workerID:     uuid.New().String()[:8],
		pollInterval: time.Second,
		jobTimeout:   5 * time.Minute,
		observer:     nopObserver{},
		backoff:      defaultBackoff,
	}
}

// Register binds a handler to a job type.
func (w *Worker) Register(jobType string, h Handler) {
	w.handlers[jobType] = h
}

// WithObserver attaches an Observer for lifecycle events.
func (w *Worker) WithObserver(o Observer) *Worker {
	if o != nil {
		w.observer = o
	}
	return w
}

// WithPollInterval overrides how often the worker polls when idle.
func (w *Worker) WithPollInterval(d time.Duration) *Worker {
	if d > 0 {
		w.pollInterval = d
	}
	return w
}

// WithJobTimeout overrides per-job execution timeout.
func (w *Worker) WithJobTimeout(d time.Duration) *Worker {
	if d > 0 {
		w.jobTimeout = d
	}
	return w
}

// WithBackoff overrides the retry backoff function. attempts is 1-indexed.
func (w *Worker) WithBackoff(fn func(attempts int) time.Duration) *Worker {
	if fn != nil {
		w.backoff = fn
	}
	return w
}

// RegisteredTypes returns the registered job types in stable order.
func (w *Worker) RegisteredTypes() []string {
	out := make([]string, 0, len(w.handlers))
	for t := range w.handlers {
		out = append(out, t)
	}
	return out
}

// Start runs the worker until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) error {
	jobTypes := w.RegisteredTypes()
	if len(jobTypes) == 0 {
		return ErrNoHandlers
	}
	slog.Info("worker starting", "worker_id", w.workerID, "concurrency", w.concurrency, "types", jobTypes)
	var wg sync.WaitGroup
	for i := 0; i < w.concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.processLoop(ctx, id, jobTypes)
		}(i)
	}
	wg.Wait()
	slog.Info("worker stopped", "worker_id", w.workerID)
	return nil
}

func (w *Worker) processLoop(ctx context.Context, id int, jobTypes []string) {
	lockedBy := fmt.Sprintf("%s-%d", w.workerID, id)
	logger := slog.With("worker_id", w.workerID, "goroutine", id)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, err := w.queue.Store().Dequeue(ctx, jobTypes, lockedBy)
		if err != nil {
			logger.Error("dequeue failed", "error", err)
			w.sleep(ctx)
			continue
		}
		if job == nil {
			w.sleep(ctx)
			continue
		}

		w.observer.OnDequeue(*job)
		w.processJob(ctx, job, logger)
	}
}

func (w *Worker) processJob(ctx context.Context, job *Job, logger *slog.Logger) {
	logger = logger.With("job_id", job.ID, "type", job.Type, "attempt", job.Attempts)

	handler, ok := w.handlers[job.Type]
	if !ok {
		errMsg := fmt.Sprintf("no handler for job type: %s", job.Type)
		logger.Error(errMsg)
		_ = w.queue.Store().Fail(ctx, job.ID, errMsg)
		w.observer.OnFail(*job, errors.New(errMsg), false)
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, w.jobTimeout)
	defer cancel()

	start := time.Now()
	err := w.safeExecute(jobCtx, handler, job.Payload)
	dur := time.Since(start)

	if err != nil {
		logger.Warn("job failed", "error", err, "duration_ms", dur.Milliseconds())
		w.handleFailure(ctx, job, err, logger)
		return
	}

	if err := w.queue.Store().Complete(ctx, job.ID); err != nil {
		logger.Error("failed to mark complete", "error", err)
		return
	}
	w.observer.OnComplete(*job, dur)
	logger.Info("job completed", "duration_ms", dur.Milliseconds())
}

func (w *Worker) safeExecute(ctx context.Context, h Handler, payload json.RawMessage) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, string(debug.Stack()))
		}
	}()
	return h(ctx, payload)
}

func (w *Worker) handleFailure(ctx context.Context, job *Job, jobErr error, logger *slog.Logger) {
	if job.Attempts >= job.MaxRetries {
		logger.Warn("job exceeded max retries; marking dead", "max_retries", job.MaxRetries)
		_ = w.queue.Store().MarkDead(ctx, job.ID, jobErr.Error())
		w.observer.OnDead(*job, jobErr)
		return
	}
	at := time.Now().UTC().Add(w.backoff(job.Attempts))
	logger.Info("scheduling retry", "retry_at", at)
	_ = w.queue.Store().Retry(ctx, job.ID, at)
	w.observer.OnFail(*job, jobErr, true)
}

func (w *Worker) sleep(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(w.pollInterval):
	}
}

// defaultBackoff: 10s, 40s, 90s, 160s, ...  (n*n*10s)
func defaultBackoff(attempts int) time.Duration {
	return time.Duration(attempts*attempts*10) * time.Second
}
