package statemachine

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type State string
type Event string

const (
	StateDraft     State = "draft"
	StateSubmitted State = "submitted"
	StateApproved  State = "approved"
	StateRejected  State = "rejected"

	EventSubmit  Event = "submit"
	EventApprove Event = "approve"
	EventReject  Event = "reject"
)

func newTestMachine() *Machine[State, Event] {
	return New[State, Event](StateDraft, []Transition[State, Event]{
		{From: StateDraft, Event: EventSubmit, To: StateSubmitted},
		{From: StateSubmitted, Event: EventApprove, To: StateApproved},
		{From: StateSubmitted, Event: EventReject, To: StateRejected,
			Guard: func(_ context.Context, payload any) error {
				switch m := payload.(type) {
				case map[string]string:
					if m["reason"] == "" {
						return errors.New("rejection requires reason")
					}
					return nil
				case map[string]any:
					if r, _ := m["reason"].(string); r == "" {
						return errors.New("rejection requires reason")
					}
					return nil
				default:
					return errors.New("rejection requires reason")
				}
			}},
	})
}

func TestHappyPath(t *testing.T) {
	m := newTestMachine()
	store := NewMemoryStore[State, Event]()
	ctx := context.Background()

	state, err := m.Transition(ctx, store, "inst-1", EventSubmit, nil)
	if err != nil || state != StateSubmitted {
		t.Fatalf("expected submitted, got %v err=%v", state, err)
	}

	state, err = m.Transition(ctx, store, "inst-1", EventApprove, nil)
	if err != nil || state != StateApproved {
		t.Fatalf("expected approved, got %v err=%v", state, err)
	}

	hist, _ := store.History(ctx, "inst-1")
	if len(hist) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(hist))
	}
}

func TestGuardRejects(t *testing.T) {
	m := newTestMachine()
	store := NewMemoryStore[State, Event]()
	ctx := context.Background()

	_, _ = m.Transition(ctx, store, "i", EventSubmit, nil)
	_, err := m.Transition(ctx, store, "i", EventReject, nil) // missing reason
	if !errors.Is(err, ErrGuardRejected) {
		t.Errorf("expected ErrGuardRejected, got %v", err)
	}

	// State should still be Submitted (unchanged).
	state, _, _ := store.Get(ctx, "i")
	if state != StateSubmitted {
		t.Errorf("state changed despite guard rejection: %v", state)
	}

	// History should NOT have been appended.
	hist, _ := store.History(ctx, "i")
	if len(hist) != 1 {
		t.Errorf("expected only the submit entry, got %d", len(hist))
	}

	// With reason, transition succeeds.
	state, err = m.Transition(ctx, store, "i", EventReject, map[string]string{"reason": "bad data"})
	if err != nil || state != StateRejected {
		t.Errorf("expected rejected with reason, got %v err=%v", state, err)
	}
}

func TestNoTransitionForCurrentState(t *testing.T) {
	m := newTestMachine()
	store := NewMemoryStore[State, Event]()
	ctx := context.Background()

	// Approve from Draft is invalid.
	_, err := m.Transition(ctx, store, "i", EventApprove, nil)
	if !errors.Is(err, ErrNoTransition) {
		t.Errorf("expected ErrNoTransition, got %v", err)
	}
}

func TestSubscribersReceiveTransitions(t *testing.T) {
	m := newTestMachine()
	store := NewMemoryStore[State, Event]()
	ctx := context.Background()

	var (
		mu      sync.Mutex
		entries []HistoryEntry[State, Event]
	)
	m.Subscribe(func(e HistoryEntry[State, Event]) {
		mu.Lock()
		entries = append(entries, e)
		mu.Unlock()
	})

	_, _ = m.Transition(ctx, store, "i", EventSubmit, nil)
	_, _ = m.Transition(ctx, store, "i", EventApprove, nil)

	mu.Lock()
	defer mu.Unlock()
	if len(entries) != 2 {
		t.Fatalf("expected 2 events, got %d", len(entries))
	}
	if entries[0].Event != EventSubmit || entries[1].Event != EventApprove {
		t.Errorf("events out of order: %+v", entries)
	}
}

func TestPayloadCoercion(t *testing.T) {
	m := newTestMachine()
	store := NewMemoryStore[State, Event]()
	ctx := context.Background()

	_, _ = m.Transition(ctx, store, "i", EventSubmit, nil)
	_, _ = m.Transition(ctx, store, "i", EventReject, map[string]any{"reason": "noise", "score": 0.42})

	hist, _ := store.History(ctx, "i")
	last := hist[len(hist)-1]
	if last.Payload["reason"] != "noise" {
		t.Errorf("payload string field lost: %+v", last.Payload)
	}
	if last.Payload["score"] != "0.42" {
		t.Errorf("payload number field not coerced: %+v", last.Payload)
	}
}
