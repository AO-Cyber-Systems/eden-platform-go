package consent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/google/uuid"
)

type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Log(e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Action)
	}
	return out
}

func newSvc(t *testing.T) (*Service, *memStore, *recorder) {
	t.Helper()
	s := newMemStore()
	r := &recorder{}
	return &Service{store: s, auditor: r}, s, r
}

func newAC() AuditContext {
	return AuditContext{CompanyID: uuid.New(), ActorID: uuid.New(), IPAddress: "1.1.1.1"}
}

func grant(t *testing.T, svc *Service, ac AuditContext, principal uuid.UUID, purpose Purpose) Entry {
	t.Helper()
	e, err := svc.Grant(context.Background(), ac, GrantRequest{
		HouseholdID:        uuid.New(),
		PrincipalMemberID:  principal,
		ConsenterMemberID:  ac.ActorID,
		Purpose:            purpose,
		Scope:              json.RawMessage(`{"all":true}`),
		ConsentTextVersion: "v1.0",
		Evidence:           json.RawMessage(`{"method":"click_through"}`),
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	return e
}

func TestService_Grant_RejectsEmptyPurpose(t *testing.T) {
	svc, _, _ := newSvc(t)
	_, err := svc.Grant(context.Background(), newAC(), GrantRequest{})
	if !errors.Is(err, ErrEmptyPurpose) {
		t.Fatalf("err = %v, want ErrEmptyPurpose", err)
	}
}

func TestService_Grant_EmitsAudit(t *testing.T) {
	svc, _, rec := newSvc(t)
	principal := uuid.New()
	grant(t, svc, newAC(), principal, PurposeAITutorInteraction)

	actions := rec.actions()
	if len(actions) != 1 || actions[0] != ActionConsentGranted {
		t.Errorf("actions = %v, want [%s]", actions, ActionConsentGranted)
	}
}

func TestService_IsValid_TrueAfterGrant(t *testing.T) {
	svc, _, rec := newSvc(t)
	ctx := context.Background()
	ac := newAC()
	principal := uuid.New()

	grant(t, svc, ac, principal, PurposeChildAccountCreation)
	v, err := svc.IsValid(ctx, ac, principal, PurposeChildAccountCreation, time.Now())
	if err != nil {
		t.Fatalf("isvalid: %v", err)
	}
	if !v.Valid {
		t.Errorf("expected valid=true, got %+v", v)
	}
	if v.LatestEntry == nil {
		t.Error("expected latest entry, got nil")
	}

	// audit: 1 grant + 1 read = 2
	if got := rec.actions(); len(got) != 2 || got[1] != ActionConsentRead {
		t.Errorf("actions = %v, want grant + read", got)
	}
}

func TestService_IsValid_FalseWhenNoEntry(t *testing.T) {
	svc, _, _ := newSvc(t)
	v, err := svc.IsValid(context.Background(), newAC(), uuid.New(), PurposeAITutorInteraction, time.Now())
	if err != nil {
		t.Fatalf("isvalid: %v", err)
	}
	if v.Valid {
		t.Errorf("expected valid=false")
	}
}

func TestService_Revoke_FlipsValidity(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	ac := newAC()
	principal := uuid.New()

	g := grant(t, svc, ac, principal, PurposeMarketingCommunications)

	v, _ := svc.IsValid(ctx, ac, principal, PurposeMarketingCommunications, time.Now())
	if !v.Valid {
		t.Fatal("pre-revoke: expected valid")
	}

	rev, err := svc.Revoke(ctx, ac, g.ID, ac.ActorID, json.RawMessage(`{"method":"click_through","reason":"user_request"}`))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !rev.IsRevocation() {
		t.Error("revocation entry should have RevokesID set")
	}

	v, _ = svc.IsValid(ctx, ac, principal, PurposeMarketingCommunications, time.Now())
	if v.Valid {
		t.Errorf("post-revoke: expected !valid, got %+v", v)
	}
}

func TestService_Revoke_AlreadyRevoked(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	ac := newAC()
	principal := uuid.New()

	g := grant(t, svc, ac, principal, PurposeDataExport)
	if _, err := svc.Revoke(ctx, ac, g.ID, ac.ActorID, nil); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	_, err := svc.Revoke(ctx, ac, g.ID, ac.ActorID, nil)
	if !errors.Is(err, ErrAlreadyRevoked) {
		t.Errorf("err = %v, want ErrAlreadyRevoked", err)
	}
}

func TestService_IsValid_AsOfTime(t *testing.T) {
	svc, store, _ := newSvc(t)
	ctx := context.Background()
	ac := newAC()
	principal := uuid.New()

	// Insert directly with a fixed past granted_at to test as-of semantics.
	past := time.Now().Add(-24 * time.Hour)
	now := time.Now()

	pastGrant := Entry{
		HouseholdID:       uuid.New(),
		PrincipalMemberID: principal,
		ConsenterMemberID: ac.ActorID,
		Purpose:           PurposeThirdPartySharing,
		GrantedAt:         past,
	}
	stored, err := store.InsertEntry(ctx, pastGrant)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = stored

	// Past time before grant: invalid
	v, _ := svc.IsValid(ctx, ac, principal, PurposeThirdPartySharing, past.Add(-time.Hour))
	if v.Valid {
		t.Errorf("before grant: expected invalid")
	}
	// Now: valid
	v, _ = svc.IsValid(ctx, ac, principal, PurposeThirdPartySharing, now)
	if !v.Valid {
		t.Errorf("after grant: expected valid")
	}
}

func TestService_ListForPrincipal_EmitsRead(t *testing.T) {
	svc, _, rec := newSvc(t)
	principal := uuid.New()
	grant(t, svc, newAC(), principal, PurposeAITutorInteraction)
	grant(t, svc, newAC(), principal, PurposeChildAccountCreation)

	entries, err := svc.ListForPrincipal(context.Background(), newAC(), principal, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
	// 2 grants + 1 read
	actions := rec.actions()
	if actions[len(actions)-1] != ActionConsentRead {
		t.Errorf("last action = %q, want %q", actions[len(actions)-1], ActionConsentRead)
	}
}

func TestEntry_IsRevocation(t *testing.T) {
	e := Entry{}
	if e.IsRevocation() {
		t.Error("nil revokes_id should not be revocation")
	}
	id := uuid.New()
	e.RevokesID = &id
	if !e.IsRevocation() {
		t.Error("set revokes_id should be revocation")
	}
}

func TestEvidence_JSON(t *testing.T) {
	ev := Evidence{
		Method:   "click_through",
		Recorded: time.Now().UTC().Truncate(time.Second),
		IPAddress: "10.0.0.5",
	}
	raw, err := ev.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var back Evidence
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Method != ev.Method {
		t.Errorf("roundtrip method = %q", back.Method)
	}
}
