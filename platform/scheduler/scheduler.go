// Package scheduler is a portable cron-style scheduler with distributed
// lock support. Reconciles aosentry/scheduler + eden-biz/cron + eden-biz/jobs.
//
// Design notes:
//   - Cron parsing is intentionally inline (not a third-party dep) to keep
//     the platform module small. Supports standard 5-field expressions
//     (minute hour dom month dow), plus shortcuts: @every <duration>,
//     @hourly, @daily, @midnight, @weekly, @monthly, @yearly, @annually.
//   - Distributed safety is achieved via a Locker — multiple replicas fight
//     for a per-tick lease, only one wins. Memory + Postgres advisory-lock
//     implementations ship in this package.
//
// See TRD 20-01.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors
var (
	ErrDuplicateName = errors.New("scheduler: duplicate task name")
	ErrInvalidCron   = errors.New("scheduler: invalid cron expression")
	ErrTimeout       = errors.New("scheduler: handler exceeded timeout")
)

// ScheduleSpec describes one scheduled task.
type ScheduleSpec struct {
	Name    string
	Cron    string                              // 5-field, "@every <dur>", or named shortcut
	Handler func(ctx context.Context) error
	Timeout time.Duration                       // 0 = no timeout
}

// Locker grants exclusive per-name leases for distributed dedup.
type Locker interface {
	TryLock(ctx context.Context, name string, ttl time.Duration) (acquired bool, release func(), err error)
}

// Scheduler runs registered tasks at their cron schedules. Distributed-safe
// when wired with a Locker (memory locker for single-replica; Postgres
// advisory-lock locker for multi-replica).
type Scheduler struct {
	mu     sync.Mutex
	tasks  map[string]*task
	locker Locker
}

type task struct {
	spec     ScheduleSpec
	schedule schedule
}

// New constructs a Scheduler. Pass NewMemoryLocker() for single-replica or
// NewPostgresLocker(pool) for multi-replica deployments.
func New(locker Locker) *Scheduler {
	if locker == nil {
		locker = NewMemoryLocker()
	}
	return &Scheduler{tasks: make(map[string]*task), locker: locker}
}

// Add registers a task. Returns an error if Name is duplicated or Cron
// fails to parse.
func (s *Scheduler) Add(spec ScheduleSpec) error {
	if spec.Name == "" || spec.Handler == nil {
		return fmt.Errorf("scheduler: Name and Handler required")
	}
	sched, err := parseSchedule(spec.Cron)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[spec.Name]; exists {
		return ErrDuplicateName
	}
	s.tasks[spec.Name] = &task{spec: spec, schedule: sched}
	return nil
}

// Start loops until ctx is cancelled, ticking every second to evaluate
// schedules. Each due task is dispatched in a separate goroutine.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if len(s.tasks) == 0 {
		s.mu.Unlock()
		return errors.New("scheduler: no tasks registered")
	}
	for n := range s.tasks {
		slog.Info("scheduler task registered", "name", n, "cron", s.tasks[n].spec.Cron)
	}
	s.mu.Unlock()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var lastTick time.Time

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return nil
		case now := <-ticker.C:
			now = now.UTC().Truncate(time.Second)
			// Skip duplicate ticks if the OS slept and ticker compressed events.
			if !lastTick.IsZero() && now.Equal(lastTick) {
				continue
			}
			lastTick = now
			s.runDueTasks(ctx, now)
		}
	}
}

func (s *Scheduler) runDueTasks(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := make([]*task, 0)
	for _, t := range s.tasks {
		if t.schedule.matches(now) {
			due = append(due, t)
		}
	}
	s.mu.Unlock()

	for _, t := range due {
		go s.runOne(ctx, t, now)
	}
}

func (s *Scheduler) runOne(ctx context.Context, t *task, tickAt time.Time) {
	leaseName := fmt.Sprintf("scheduler:%s:%d", t.spec.Name, tickAt.Unix())
	lockCtx, cancelLock := context.WithTimeout(ctx, 5*time.Second)
	ok, release, err := s.locker.TryLock(lockCtx, leaseName, 90*time.Second)
	cancelLock()
	if err != nil {
		slog.Warn("scheduler: lock acquisition failed", "task", t.spec.Name, "error", err)
		return
	}
	if !ok {
		// Another replica got it.
		return
	}
	defer release()

	jobCtx := ctx
	if t.spec.Timeout > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(ctx, t.spec.Timeout)
		defer cancel()
	}

	start := time.Now()
	err = safeRun(jobCtx, t.spec.Handler)
	dur := time.Since(start)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("scheduler: task timed out", "task", t.spec.Name, "duration_ms", dur.Milliseconds())
		} else {
			slog.Error("scheduler: task failed", "task", t.spec.Name, "error", err, "duration_ms", dur.Milliseconds())
		}
		return
	}
	slog.Info("scheduler: task ran", "task", t.spec.Name, "duration_ms", dur.Milliseconds())
}

func safeRun(ctx context.Context, h func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scheduler: panic: %v", r)
		}
	}()
	return h(ctx)
}

// MemoryLocker grants leases via an in-process map. Useful for tests and
// single-replica deployments.
type MemoryLocker struct {
	mu     sync.Mutex
	leases map[string]time.Time
}

// NewMemoryLocker constructs a MemoryLocker.
func NewMemoryLocker() *MemoryLocker { return &MemoryLocker{leases: make(map[string]time.Time)} }

// TryLock grants a lease unless an existing un-expired lease holds the name.
func (l *MemoryLocker) TryLock(_ context.Context, name string, ttl time.Duration) (bool, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if exp, ok := l.leases[name]; ok && exp.After(now) {
		return false, func() {}, nil
	}
	l.leases[name] = now.Add(ttl)
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.leases, name)
	}
	return true, release, nil
}

// schedule is the parsed cron representation.
type schedule struct {
	// Either everyDur OR cron-fields are populated.
	everyDur time.Duration

	minute, hour, dom, month, dow []int  // exact matches; nil = wildcard
}

func (s schedule) matches(t time.Time) bool {
	if s.everyDur > 0 {
		// Match every N seconds since unix epoch.
		secs := s.everyDur / time.Second
		if secs <= 0 {
			secs = 1
		}
		return (t.Unix() % int64(secs)) == 0
	}

	if !match(s.minute, t.Minute()) {
		return false
	}
	if !match(s.hour, t.Hour()) {
		return false
	}
	if !match(s.dom, t.Day()) {
		return false
	}
	if !match(s.month, int(t.Month())) {
		return false
	}
	if !match(s.dow, int(t.Weekday())) {
		return false
	}
	// 5-field cron is minute resolution; only fire on second 0.
	return t.Second() == 0
}

func match(values []int, v int) bool {
	if len(values) == 0 {
		return true
	}
	for _, val := range values {
		if val == v {
			return true
		}
	}
	return false
}

// parseSchedule parses a cron expression or @-shortcut.
func parseSchedule(expr string) (schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return schedule{}, fmt.Errorf("%w: empty", ErrInvalidCron)
	}

	if strings.HasPrefix(expr, "@every ") {
		dur, err := time.ParseDuration(strings.TrimPrefix(expr, "@every "))
		if err != nil {
			return schedule{}, fmt.Errorf("%w: %v", ErrInvalidCron, err)
		}
		if dur < time.Second {
			return schedule{}, fmt.Errorf("%w: @every must be >= 1s", ErrInvalidCron)
		}
		return schedule{everyDur: dur}, nil
	}

	// Named shortcuts
	switch expr {
	case "@hourly":
		return parseSchedule("0 * * * *")
	case "@daily", "@midnight":
		return parseSchedule("0 0 * * *")
	case "@weekly":
		return parseSchedule("0 0 * * 0")
	case "@monthly":
		return parseSchedule("0 0 1 * *")
	case "@yearly", "@annually":
		return parseSchedule("0 0 1 1 *")
	}

	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return schedule{}, fmt.Errorf("%w: expected 5 fields, got %d", ErrInvalidCron, len(parts))
	}

	minute, err := parseField(parts[0], 0, 59)
	if err != nil {
		return schedule{}, fmt.Errorf("%w (minute): %v", ErrInvalidCron, err)
	}
	hour, err := parseField(parts[1], 0, 23)
	if err != nil {
		return schedule{}, fmt.Errorf("%w (hour): %v", ErrInvalidCron, err)
	}
	dom, err := parseField(parts[2], 1, 31)
	if err != nil {
		return schedule{}, fmt.Errorf("%w (dom): %v", ErrInvalidCron, err)
	}
	month, err := parseField(parts[3], 1, 12)
	if err != nil {
		return schedule{}, fmt.Errorf("%w (month): %v", ErrInvalidCron, err)
	}
	dow, err := parseField(parts[4], 0, 6)
	if err != nil {
		return schedule{}, fmt.Errorf("%w (dow): %v", ErrInvalidCron, err)
	}
	return schedule{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

// parseField parses a cron field. Supports: *, n, n-m, n,m,..., */N, n-m/N.
// Returns nil for "*" (wildcard).
func parseField(field string, lo, hi int) ([]int, error) {
	if field == "*" {
		return nil, nil
	}
	out := make(map[int]struct{})
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step in %q", field)
			}
			step = s
			part = part[:i]
		}
		var rangeLo, rangeHi int
		if part == "*" {
			rangeLo, rangeHi = lo, hi
		} else if i := strings.Index(part, "-"); i >= 0 {
			a, errA := strconv.Atoi(part[:i])
			b, errB := strconv.Atoi(part[i+1:])
			if errA != nil || errB != nil || a < lo || b > hi || a > b {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			rangeLo, rangeHi = a, b
		} else {
			n, err := strconv.Atoi(part)
			if err != nil || n < lo || n > hi {
				return nil, fmt.Errorf("invalid value %q", part)
			}
			rangeLo, rangeHi = n, n
		}
		for v := rangeLo; v <= rangeHi; v += step {
			out[v] = struct{}{}
		}
	}
	values := make([]int, 0, len(out))
	for v := range out {
		values = append(values, v)
	}
	return values, nil
}
