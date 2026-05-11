package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/auth"
	billingrail "github.com/aocybersystems/eden-platform-go/platform/billing-rail"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	featureflags "github.com/aocybersystems/eden-platform-go/platform/feature-flags"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/aocybersystems/eden-platform-go/platform/livekit"
	"github.com/google/uuid"
)

// edenFamilyHarness composes every platform service Eden Family launches on.
// Construct via newHarness(t); the harness installs t.Cleanup for orderly
// shutdown of the audit logger.
type edenFamilyHarness struct {
	JWT          *auth.JWTManager
	Household    *household.Service
	Consent      *consent.Service
	FlagsClient  *featureflags.Client
	FlagsSource  *featureflags.MemorySource
	Rail         *billingrail.MockRail
	Sink         *billingrail.MockSink
	Dispatcher   *billingrail.Dispatcher
	LiveKit      *livekit.Service
	Signaler     *livekit.ChannelSignaler
	Rooms        *roomsStub
	Audit        *recordingAuditStore
	AuditLogger  *audit.Logger
	LiveKitURL   string

	// Convenience UUIDs used by every test for the "company" / tenant the
	// household is billed under. Eden Family is a single billable tenant
	// in this composition.
	TenantCompanyID uuid.UUID
}

// newHarness wires every platform service on in-memory backends. Each call
// returns a fresh harness — tests are isolated.
func newHarness(t *testing.T) *edenFamilyHarness {
	t.Helper()

	// Audit + logger
	auditStore := newRecordingAuditStore()
	auditLogger := audit.NewLogger(auditStore)
	auditLogger.Start()
	t.Cleanup(auditLogger.Stop)

	// JWT manager (ephemeral key — dev mode is fine for tests).
	jm, err := auth.NewJWTManager(auth.DefaultJWTConfig())
	if err != nil {
		t.Fatalf("auth.NewJWTManager: %v", err)
	}

	// Household + consent services.
	hhSvc := household.NewService(newMemHouseholdStore(), auditLogger)
	consentSvc := consent.NewService(newMemConsentStore(), auditLogger)

	// Feature flags.
	flagsSrc := featureflags.NewMemorySource()
	flagsClient := featureflags.New(flagsSrc)

	// Billing rail.
	rail := &billingrail.MockRail{NameValue: "stripe"}
	sink := &billingrail.MockSink{}
	dispatcher := billingrail.NewDispatcher(rail, sink)

	// LiveKit.
	rooms := newRoomsStub()
	signaler := livekit.NewChannelSignaler()
	lkSvc, err := livekit.NewService(livekit.ServiceConfig{
		Store:      livekit.NewInMemoryStore(),
		Rooms:      rooms,
		Tokens:     stubTokens{},
		Signaler:   signaler,
		LiveKitURL: "wss://lk.test.local",
	})
	if err != nil {
		t.Fatalf("livekit.NewService: %v", err)
	}

	return &edenFamilyHarness{
		JWT:             jm,
		Household:       hhSvc,
		Consent:         consentSvc,
		FlagsClient:     flagsClient,
		FlagsSource:     flagsSrc,
		Rail:            rail,
		Sink:            sink,
		Dispatcher:      dispatcher,
		LiveKit:         lkSvc,
		Signaler:        signaler,
		Rooms:           rooms,
		Audit:           auditStore,
		AuditLogger:     auditLogger,
		LiveKitURL:      "wss://lk.test.local",
		TenantCompanyID: uuid.New(),
	}
}

// DrainAudit blocks briefly to let the async audit logger flush, then returns
// every event recorded so far. The audit pipeline batches on a 100ms timer;
// 250ms gives two flush windows.
func (h *edenFamilyHarness) DrainAudit() []audit.Event {
	time.Sleep(250 * time.Millisecond)
	return h.Audit.Snapshot()
}

// auditCtx is a convenience helper for constructing the platform/household
// + platform/consent AuditContext from the harness state.
func (h *edenFamilyHarness) auditCtx(actor uuid.UUID) (household.AuditContext, consent.AuditContext) {
	return household.AuditContext{
			CompanyID: h.TenantCompanyID,
			ActorID:   actor,
			IPAddress: "127.0.0.1",
		}, consent.AuditContext{
			CompanyID: h.TenantCompanyID,
			ActorID:   actor,
			IPAddress: "127.0.0.1",
		}
}

// --- LiveKit stubs ---------------------------------------------------------

// roomsStub records every CreateCallRoom / CreateMeetingRoom / DeleteRoom
// call and returns deterministic RoomInfo. Concurrency-safe.
type roomsStub struct {
	mu        sync.Mutex
	created   []string
	deleted   []string
}

func newRoomsStub() *roomsStub { return &roomsStub{} }

func (r *roomsStub) CreateCallRoom(_ context.Context, callID string) (livekit.RoomInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := "call-" + callID
	r.created = append(r.created, name)
	return livekit.RoomInfo{Name: name, MaxParticipants: 2}, nil
}

func (r *roomsStub) CreateMeetingRoom(_ context.Context, name string, max int) (livekit.RoomInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rn := "meeting-" + name
	r.created = append(r.created, rn)
	return livekit.RoomInfo{Name: rn, MaxParticipants: uint32(max)}, nil
}

func (r *roomsStub) DeleteRoom(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, name)
	return nil
}

// stubTokens implements livekit.Tokens with deterministic output.
type stubTokens struct{}

func (stubTokens) IssueJoinToken(roomName, identity, _ string, _ time.Duration) (string, error) {
	return "tok:" + roomName + ":" + identity, nil
}

