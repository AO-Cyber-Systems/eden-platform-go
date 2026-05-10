package livekit

import (
	"sync"
	"time"
)

// Clock is the time source used by the Service. Production code uses
// realClock; tests use FakeClock.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the subset of time.Timer the Service relies on.
type Timer interface {
	Stop() bool
}

// realClock is the default time.Now-backed Clock.
type realClock struct{}

// NewRealClock returns a Clock backed by time.Now and time.AfterFunc.
func NewRealClock() Clock { return realClock{} }

// Now returns time.Now().
func (realClock) Now() time.Time { return time.Now() }

// AfterFunc wraps time.AfterFunc.
func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// FakeClock is a deterministic Clock for tests. It does NOT advance time on
// its own — tests call Advance to fire scheduled callbacks.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

// NewFakeClock returns a FakeClock starting at start.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules f to fire after d (relative to fake Now). The callback
// runs synchronously inside Advance.
func (c *FakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{firesAt: c.now.Add(d), fn: f}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the fake clock forward by d, firing any timers whose deadline
// has passed (in chronological order). Each callback runs to completion
// before the next is invoked.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	// Snapshot fired timers so we can release the lock before calling fn.
	var ready []*fakeTimer
	remaining := c.timers[:0]
	for _, t := range c.timers {
		if t.stopped {
			continue
		}
		if !t.firesAt.After(now) {
			ready = append(ready, t)
			continue
		}
		remaining = append(remaining, t)
	}
	c.timers = remaining
	c.mu.Unlock()
	for _, t := range ready {
		if !t.stopped {
			t.fn()
		}
	}
}

type fakeTimer struct {
	firesAt time.Time
	fn      func()
	stopped bool
}

// Stop prevents the timer from firing if it hasn't already.
func (t *fakeTimer) Stop() bool {
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}
