package bridge

import (
	"testing"
)

// testAdapter implements Adapter for testing.
type testAdapter struct {
	prefix      string
	eventTypes  []string
	actionTypes []ActionSchema
}

func (a *testAdapter) EventTypes() []string { return a.eventTypes }

func (a *testAdapter) Transform(subject string, envelope EventEnvelope) (*TransformedEvent, error) {
	return &TransformedEvent{EventType: subject, CompanyID: envelope.CompanyID}, nil
}

func (a *testAdapter) ActionTypes() []ActionSchema { return a.actionTypes }

func (a *testAdapter) SupportsAction(actionType string) bool {
	for _, at := range a.actionTypes {
		if at.Type == actionType {
			return true
		}
	}
	return false
}

func TestAdapterRegistry_Register(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &testAdapter{prefix: "eden.", eventTypes: []string{"user.created"}}

	registry.Register("eden.", adapter)

	found, ok := registry.FindAdapter("eden.platform.user")
	if !ok {
		t.Fatalf("FindAdapter() = false, want true")
	}
	if found != adapter {
		t.Errorf("FindAdapter() returned different adapter")
	}
}

func TestAdapterRegistry_FindAdapter_LongestPrefix(t *testing.T) {
	registry := NewAdapterRegistry()
	shortAdapter := &testAdapter{prefix: "eden."}
	longAdapter := &testAdapter{prefix: "eden.platform."}

	registry.Register("eden.", shortAdapter)
	registry.Register("eden.platform.", longAdapter)

	found, ok := registry.FindAdapter("eden.platform.user")
	if !ok {
		t.Fatalf("FindAdapter() = false, want true")
	}
	if found != longAdapter {
		t.Errorf("FindAdapter() matched short prefix, want longest prefix 'eden.platform.'")
	}
}

func TestAdapterRegistry_FindAdapter_NotFound(t *testing.T) {
	registry := NewAdapterRegistry()
	registry.Register("eden.", &testAdapter{})

	_, ok := registry.FindAdapter("other.subject")
	if ok {
		t.Errorf("FindAdapter() = true for unknown subject, want false")
	}
}

func TestAdapterRegistry_FindAdapterForAction(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &testAdapter{
		actionTypes: []ActionSchema{
			{Type: "approve", Label: "Approve"},
		},
	}
	registry.Register("eden.", adapter)

	found, ok := registry.FindAdapterForAction("approve")
	if !ok {
		t.Fatalf("FindAdapterForAction() = false, want true")
	}
	if found != adapter {
		t.Errorf("FindAdapterForAction() returned different adapter")
	}

	_, ok = registry.FindAdapterForAction("nonexistent")
	if ok {
		t.Errorf("FindAdapterForAction('nonexistent') = true, want false")
	}
}

func TestAdapterRegistry_ListAll(t *testing.T) {
	registry := NewAdapterRegistry()
	a1 := &testAdapter{}
	a2 := &testAdapter{}

	registry.Register("prefix1.", a1)
	registry.Register("prefix2.", a2)

	all := registry.ListAll()
	if len(all) != 2 {
		t.Errorf("ListAll() returned %d adapters, want 2", len(all))
	}
}
