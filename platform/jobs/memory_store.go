package jobs

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore is a thread-safe in-memory Store. Suitable for unit tests and
// dev environments. Not durable across process restarts.
type MemoryStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]*Job)}
}

// Enqueue persists a new job.
func (m *MemoryStore) Enqueue(_ context.Context, job Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := job
	m.jobs[job.ID] = &cp
	return nil
}

// Dequeue atomically claims the next eligible pending job. Eligibility is:
//   - status == "pending"
//   - type ∈ jobTypes
//   - scheduled_at is nil OR <= now
//
// Ordering: priority desc, then created_at asc.
func (m *MemoryStore) Dequeue(_ context.Context, jobTypes []string, lockedBy string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	candidates := make([]*Job, 0)
	allowed := make(map[string]struct{}, len(jobTypes))
	for _, t := range jobTypes {
		allowed[t] = struct{}{}
	}

	for _, j := range m.jobs {
		if j.Status != StatusPending {
			continue
		}
		if _, ok := allowed[j.Type]; !ok {
			continue
		}
		if j.ScheduledAt != nil && j.ScheduledAt.After(now) {
			continue
		}
		candidates = append(candidates, j)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.Slice(candidates, func(i, k int) bool {
		if candidates[i].Priority != candidates[k].Priority {
			return candidates[i].Priority > candidates[k].Priority
		}
		return candidates[i].CreatedAt.Before(candidates[k].CreatedAt)
	})

	chosen := candidates[0]
	chosen.Status = StatusRunning
	chosen.LockedBy = lockedBy
	t := now
	chosen.LockedAt = &t
	chosen.Attempts++

	cp := *chosen
	return &cp, nil
}

// Complete marks a job done.
func (m *MemoryStore) Complete(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	j.Status = StatusCompleted
	j.CompletedAt = &now
	j.LockedBy = ""
	j.LockedAt = nil
	return nil
}

// Fail marks a job failed (no further retry; consumer chooses to retry/dead).
func (m *MemoryStore) Fail(_ context.Context, jobID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil
	}
	j.Status = StatusFailed
	j.Error = errMsg
	j.LockedBy = ""
	j.LockedAt = nil
	return nil
}

// Retry reschedules a job for a future attempt.
func (m *MemoryStore) Retry(_ context.Context, jobID string, scheduledAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil
	}
	j.Status = StatusPending
	at := scheduledAt.UTC()
	j.ScheduledAt = &at
	j.LockedBy = ""
	j.LockedAt = nil
	return nil
}

// MarkDead permanently removes a job from circulation.
func (m *MemoryStore) MarkDead(_ context.Context, jobID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil
	}
	j.Status = StatusDead
	j.Error = errMsg
	j.LockedBy = ""
	j.LockedAt = nil
	return nil
}

// Heartbeat updates the locked_at timestamp; used to keep long-running jobs
// from being claimed by another worker after a stale-lock timeout.
func (m *MemoryStore) Heartbeat(_ context.Context, jobID, lockedBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok || j.LockedBy != lockedBy {
		return nil
	}
	now := time.Now().UTC()
	j.LockedAt = &now
	return nil
}

// ListJobs returns up to limit jobs of the given status, newest first.
func (m *MemoryStore) ListJobs(_ context.Context, status string, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0)
	for _, j := range m.jobs {
		if j.Status == status {
			out = append(out, *j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CreatedAt.After(out[k].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
