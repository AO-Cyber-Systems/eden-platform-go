package audit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockAuditStore struct {
	mu     sync.Mutex
	events []auditEntry
}

type auditEntry struct {
	CompanyID uuid.UUID
	Action    string
}

func (m *mockAuditStore) CreateAuditLog(_ context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, auditEntry{CompanyID: companyID, Action: action})
	return nil
}

func TestLogger_LogAndFlush(t *testing.T) {
	store := &mockAuditStore{}
	logger := NewLogger(store)
	logger.Start()

	companyID := uuid.New()
	actorID := uuid.New()

	logger.Log(Event{
		CompanyID: companyID.String(),
		ActorID:   actorID.String(),
		Action:    "user.login",
		Resource:  "user",
	})
	logger.Log(Event{
		CompanyID: companyID.String(),
		ActorID:   actorID.String(),
		Action:    "user.signup",
		Resource:  "user",
	})

	// Stop drains the channel
	logger.Stop()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 2 {
		t.Errorf("Store received %d events, want 2", len(store.events))
	}
}

func TestLogger_DropWhenFull(t *testing.T) {
	store := &mockAuditStore{}
	logger := NewLogger(store)
	// Don't start the logger -- channel will fill up

	// Fill the channel (capacity 10000)
	for i := 0; i < 10001; i++ {
		logger.Log(Event{
			CompanyID: uuid.New().String(),
			ActorID:   uuid.New().String(),
			Action:    "test",
		})
	}
	// Should not panic -- last event should be dropped silently
}

func TestLogger_StopDrains(t *testing.T) {
	store := &mockAuditStore{}
	logger := NewLogger(store)
	logger.Start()

	companyID := uuid.New()
	actorID := uuid.New()

	for i := 0; i < 5; i++ {
		logger.Log(Event{
			CompanyID: companyID.String(),
			ActorID:   actorID.String(),
			Action:    "test.action",
			Resource:  "test",
		})
	}

	// Small delay to allow events to be batched
	time.Sleep(200 * time.Millisecond)
	logger.Stop()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 5 {
		t.Errorf("Store received %d events after stop, want 5", len(store.events))
	}
}
