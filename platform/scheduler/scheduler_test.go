package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseStandardCron(t *testing.T) {
	cases := []struct {
		expr  string
		valid bool
	}{
		{"* * * * *", true},
		{"0 0 * * *", true},
		{"*/5 * * * *", true},
		{"0,15,30,45 * * * *", true},
		{"0 9-17 * * 1-5", true},
		{"@hourly", true},
		{"@daily", true},
		{"@every 30s", true},
		{"@every 500ms", false}, // < 1s
		{"bogus", false},
		{"", false},
		{"60 * * * *", false}, // minute out of range
	}
	for _, c := range cases {
		_, err := parseSchedule(c.expr)
		if (err == nil) != c.valid {
			t.Errorf("expr %q: expected valid=%v, got err=%v", c.expr, c.valid, err)
		}
	}
}

func TestScheduleMatches(t *testing.T) {
	s, err := parseSchedule("0 12 * * *") // noon daily
	if err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	notNoon := time.Date(2025, 1, 1, 11, 30, 0, 0, time.UTC)
	if !s.matches(noon) {
		t.Errorf("expected match at noon")
	}
	if s.matches(notNoon) {
		t.Errorf("expected no match at 11:30")
	}
}

func TestEveryShortcutMatches(t *testing.T) {
	s, err := parseSchedule("@every 5s")
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(100, 0)        // 100 % 5 == 0
	tNo := time.Unix(101, 0)       // 101 % 5 == 1
	if !s.matches(t0) {
		t.Errorf("expected match at unix 100")
	}
	if s.matches(tNo) {
		t.Errorf("expected no match at unix 101")
	}
}

func TestAddDuplicateRejected(t *testing.T) {
	s := New(NewMemoryLocker())
	spec := ScheduleSpec{Name: "n", Cron: "* * * * *", Handler: func(ctx context.Context) error { return nil }}
	if err := s.Add(spec); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(spec); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}

func TestAddInvalidCron(t *testing.T) {
	s := New(NewMemoryLocker())
	err := s.Add(ScheduleSpec{Name: "n", Cron: "bogus", Handler: func(ctx context.Context) error { return nil }})
	if !errors.Is(err, ErrInvalidCron) {
		t.Errorf("expected ErrInvalidCron, got %v", err)
	}
}

func TestDistributedDedup(t *testing.T) {
	// Three schedulers SHARING a memory locker — each tick should produce
	// at most one execution across all three.
	locker := NewMemoryLocker()

	var ran atomic.Int32
	mkSched := func() *Scheduler {
		s := New(locker)
		_ = s.Add(ScheduleSpec{
			Name:    "shared",
			Cron:    "@every 1s",
			Handler: func(ctx context.Context) error { ran.Add(1); return nil },
		})
		return s
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = mkSched().Start(ctx) }()
	}
	wg.Wait()

	if ran.Load() < 1 {
		t.Errorf("expected at least 1 run, got %d", ran.Load())
	}
	// Three schedulers across 2 unique 1s ticks should yield at most 3
	// total runs (1 per tick × ~3 ticks); we accept up to 5 to allow for
	// scheduling slop on slow CI.
	if ran.Load() > 5 {
		t.Errorf("expected at most 5 runs (likely 2-3), got %d — dedup not working", ran.Load())
	}
}

func TestHandlerTimeout(t *testing.T) {
	locker := NewMemoryLocker()
	var deadlineHit atomic.Bool

	s := New(locker)
	_ = s.Add(ScheduleSpec{
		Name: "slow",
		Cron: "@every 1s",
		Handler: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				deadlineHit.Store(true)
				return ctx.Err()
			case <-time.After(time.Second):
				return nil
			}
		},
		Timeout: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	if !deadlineHit.Load() {
		t.Errorf("expected handler to observe timeout")
	}
}

func TestHandlerPanicCaught(t *testing.T) {
	locker := NewMemoryLocker()
	var ran atomic.Int32
	s := New(locker)
	_ = s.Add(ScheduleSpec{
		Name: "panic",
		Cron: "@every 1s",
		Handler: func(ctx context.Context) error {
			ran.Add(1)
			panic("kaboom")
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)
	// Should have run at least once and not crashed the scheduler.
	if ran.Load() < 1 {
		t.Errorf("expected at least 1 invocation")
	}
}

func TestMemoryLockerSerializes(t *testing.T) {
	l := NewMemoryLocker()
	ctx := context.Background()

	ok1, rel1, _ := l.TryLock(ctx, "x", time.Minute)
	if !ok1 {
		t.Fatal("first lock should succeed")
	}
	ok2, _, _ := l.TryLock(ctx, "x", time.Minute)
	if ok2 {
		t.Errorf("second lock should be rejected")
	}
	rel1()
	ok3, _, _ := l.TryLock(ctx, "x", time.Minute)
	if !ok3 {
		t.Errorf("after release, lock should be reacquirable")
	}
}
