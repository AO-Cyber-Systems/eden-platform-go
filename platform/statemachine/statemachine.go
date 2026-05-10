// Package statemachine provides a generic transition engine for any
// workflow with discrete states. Donor: eden-biz/statemachine.
//
// Generic over state type S and event type E (both must be comparable).
// State + history persistence is delegated to a Store interface so
// instances can live in memory (tests) or Postgres (production).
//
// See TRD 19-03.
package statemachine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors
var (
	ErrNoTransition  = errors.New("statemachine: no transition for current state + event")
	ErrGuardRejected = errors.New("statemachine: guard rejected transition")
	ErrInstanceNotFound = errors.New("statemachine: instance not found")
)

// Transition describes one allowed state change.
type Transition[S comparable, E comparable] struct {
	From  S
	Event E
	To    S
	// Guard is an optional pre-condition. Returning a non-nil error wraps
	// ErrGuardRejected and aborts the transition.
	Guard func(ctx context.Context, payload any) error
}

// HistoryEntry is one row in the per-instance audit log.
type HistoryEntry[S comparable, E comparable] struct {
	InstanceID string
	From, To   S
	Event      E
	Timestamp  time.Time
	Reason     string
	Payload    map[string]string
}

// Subscriber is invoked after every successful transition. Must not block.
type Subscriber[S comparable, E comparable] func(HistoryEntry[S, E])

// Machine binds a state-graph + initial state. Instances are stored in a
// Store (in-memory by default).
type Machine[S comparable, E comparable] struct {
	initial     S
	transitions map[transitionKey[S, E]]Transition[S, E]

	subMu       sync.RWMutex
	subscribers []Subscriber[S, E]
}

type transitionKey[S comparable, E comparable] struct {
	From  S
	Event E
}

// New constructs a Machine.
func New[S comparable, E comparable](initial S, transitions []Transition[S, E]) *Machine[S, E] {
	tm := make(map[transitionKey[S, E]]Transition[S, E], len(transitions))
	for _, t := range transitions {
		tm[transitionKey[S, E]{From: t.From, Event: t.Event}] = t
	}
	return &Machine[S, E]{initial: initial, transitions: tm}
}

// Initial returns the configured initial state (used by Stores when
// constructing a fresh instance).
func (m *Machine[S, E]) Initial() S { return m.initial }

// Subscribe registers a transition observer.
func (m *Machine[S, E]) Subscribe(fn Subscriber[S, E]) {
	if fn == nil {
		return
	}
	m.subMu.Lock()
	m.subscribers = append(m.subscribers, fn)
	m.subMu.Unlock()
}

// Transition advances instanceID through event. Loads the current state
// from store, evaluates the matching transition (if any), runs the guard,
// persists the new state + history entry, and notifies subscribers.
func (m *Machine[S, E]) Transition(ctx context.Context, store Store[S, E], instanceID string, event E, payload any) (S, error) {
	cur, _, err := store.Get(ctx, instanceID)
	if err != nil {
		// Treat missing instance as initial state.
		if !errors.Is(err, ErrInstanceNotFound) {
			return zero[S](), err
		}
		cur = m.initial
	}

	t, ok := m.transitions[transitionKey[S, E]{From: cur, Event: event}]
	if !ok {
		return cur, fmt.Errorf("%w: state=%v event=%v", ErrNoTransition, cur, event)
	}

	if t.Guard != nil {
		if guardErr := t.Guard(ctx, payload); guardErr != nil {
			return cur, fmt.Errorf("%w: %v", ErrGuardRejected, guardErr)
		}
	}

	entry := HistoryEntry[S, E]{
		InstanceID: instanceID,
		From:       cur,
		To:         t.To,
		Event:      event,
		Timestamp:  time.Now().UTC(),
		Payload:    coerceStringMap(payload),
	}
	if err := store.Save(ctx, instanceID, t.To, entry); err != nil {
		return cur, fmt.Errorf("statemachine: save: %w", err)
	}

	m.notify(entry)
	return t.To, nil
}

func (m *Machine[S, E]) notify(entry HistoryEntry[S, E]) {
	m.subMu.RLock()
	defer m.subMu.RUnlock()
	for _, sub := range m.subscribers {
		sub(entry)
	}
}

// Store persists per-instance state and history.
type Store[S comparable, E comparable] interface {
	Get(ctx context.Context, instanceID string) (state S, history []HistoryEntry[S, E], err error)
	Save(ctx context.Context, instanceID string, state S, entry HistoryEntry[S, E]) error
	History(ctx context.Context, instanceID string) ([]HistoryEntry[S, E], error)
}

// MemoryStore is an in-process Store.
type MemoryStore[S comparable, E comparable] struct {
	mu        sync.RWMutex
	states    map[string]S
	histories map[string][]HistoryEntry[S, E]
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore[S comparable, E comparable]() *MemoryStore[S, E] {
	return &MemoryStore[S, E]{
		states:    make(map[string]S),
		histories: make(map[string][]HistoryEntry[S, E]),
	}
}

func (s *MemoryStore[S, E]) Get(_ context.Context, id string) (S, []HistoryEntry[S, E], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[id]
	if !ok {
		return zero[S](), nil, ErrInstanceNotFound
	}
	return state, append([]HistoryEntry[S, E](nil), s.histories[id]...), nil
}

func (s *MemoryStore[S, E]) Save(_ context.Context, id string, state S, entry HistoryEntry[S, E]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[id] = state
	s.histories[id] = append(s.histories[id], entry)
	return nil
}

func (s *MemoryStore[S, E]) History(_ context.Context, id string) ([]HistoryEntry[S, E], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]HistoryEntry[S, E](nil), s.histories[id]...), nil
}

func zero[T any]() T {
	var z T
	return z
}

// coerceStringMap accepts payload as map[string]string, map[string]any, or
// nil — anything else is converted to fmt.Sprintf("%v") under a "payload" key.
func coerceStringMap(p any) map[string]string {
	if p == nil {
		return nil
	}
	if m, ok := p.(map[string]string); ok {
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	if m, ok := p.(map[string]any); ok {
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = fmt.Sprintf("%v", v)
		}
		return out
	}
	return map[string]string{"payload": fmt.Sprintf("%v", p)}
}
