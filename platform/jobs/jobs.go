// Package jobs provides a portable background-job queue with PostgreSQL and
// in-memory store implementations. It is the canonical job system for the
// Eden portfolio (see TRD 18-01 for the library-underneath decision).
//
// Quickstart:
//
//	store := jobs.NewMemoryStore() // or jobs.NewPostgresStore(pool)
//	q := jobs.NewQueue(store)
//
//	w := jobs.NewWorker(q, 4)
//	w.Register("email.send", func(ctx context.Context, payload json.RawMessage) error {
//	    var p struct{ To, Subject string }
//	    _ = json.Unmarshal(payload, &p)
//	    return mailer.Send(ctx, p.To, p.Subject)
//	})
//	go w.Start(ctx)
//
//	q.Enqueue(ctx, "email.send", map[string]string{"to": "x@y", "subject": "hi"})
//	q.EnqueueDelayed(ctx, "email.send", payload, 5*time.Minute)
//
// The Worker safely recovers from handler panics and applies exponential
// backoff (10s, 40s, 90s, ...) up to MaxRetries before marking jobs dead.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Status values for a Job.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusDead      = "dead"
)

// Priority constants.
const (
	PriorityNormal int = 0
	PriorityHigh   int = 1
)

// DefaultMaxRetries is the retry ceiling applied when not explicitly set.
const DefaultMaxRetries = 3

// Job is a unit of work in the queue.
type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	MaxRetries  int             `json:"max_retries"`
	Attempts    int             `json:"attempts"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	LockedBy    string          `json:"locked_by,omitempty"`
	LockedAt    *time.Time      `json:"locked_at,omitempty"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Handler executes a single job. Returning a non-nil error triggers retry.
// Panics inside Handler are caught by the Worker and treated as failures.
type Handler func(ctx context.Context, payload json.RawMessage) error

// Store is the persistence contract. Implementations: PostgresStore (production),
// MemoryStore (tests/dev).
type Store interface {
	Enqueue(ctx context.Context, job Job) error
	Dequeue(ctx context.Context, jobTypes []string, lockedBy string) (*Job, error)
	Complete(ctx context.Context, jobID string) error
	Fail(ctx context.Context, jobID, errMsg string) error
	Retry(ctx context.Context, jobID string, scheduledAt time.Time) error
	MarkDead(ctx context.Context, jobID, errMsg string) error
	Heartbeat(ctx context.Context, jobID, lockedBy string) error
	ListJobs(ctx context.Context, status string, limit int) ([]Job, error)
}

// Queue is the producer-side handle.
type Queue struct {
	store Store
}

// NewQueue constructs a Queue.
func NewQueue(store Store) *Queue {
	return &Queue{store: store}
}

// Store returns the underlying store (for advanced consumers).
func (q *Queue) Store() Store { return q.store }

// Enqueue queues a job for immediate processing at normal priority.
func (q *Queue) Enqueue(ctx context.Context, jobType string, payload any) error {
	return q.enqueue(ctx, jobType, payload, PriorityNormal, nil)
}

// EnqueueHigh queues a high-priority job for immediate processing.
func (q *Queue) EnqueueHigh(ctx context.Context, jobType string, payload any) error {
	return q.enqueue(ctx, jobType, payload, PriorityHigh, nil)
}

// EnqueueDelayed queues a job to run after delay.
func (q *Queue) EnqueueDelayed(ctx context.Context, jobType string, payload any, delay time.Duration) error {
	at := time.Now().UTC().Add(delay)
	return q.enqueue(ctx, jobType, payload, PriorityNormal, &at)
}

// EnqueueAt queues a job to run at a specific UTC time.
func (q *Queue) EnqueueAt(ctx context.Context, jobType string, payload any, at time.Time) error {
	utc := at.UTC()
	return q.enqueue(ctx, jobType, payload, PriorityNormal, &utc)
}

func (q *Queue) enqueue(ctx context.Context, jobType string, payload any, priority int, scheduledAt *time.Time) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jobs: marshal payload: %w", err)
	}

	job := Job{
		ID:          uuid.New().String(),
		Type:        jobType,
		Payload:     raw,
		Priority:    priority,
		MaxRetries:  DefaultMaxRetries,
		Status:      StatusPending,
		ScheduledAt: scheduledAt,
		CreatedAt:   time.Now().UTC(),
	}

	if err := q.store.Enqueue(ctx, job); err != nil {
		return fmt.Errorf("jobs: enqueue %s: %w", jobType, err)
	}
	slog.Debug("job enqueued", "id", job.ID, "type", jobType, "priority", priority)
	return nil
}
