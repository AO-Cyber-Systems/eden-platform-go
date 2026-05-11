// Package integration's Eden Family suite: end-to-end scenarios that
// compose every platform package the Eden Family launch surface depends
// on. Each test reads as a user journey from the perspective of an Eden
// Family caller.
//
// The integration suite is the M9 verification artifact for objective 33.
// See docs/eden-family-integration.md for the architecture diagram and
// docs/eden-family-launch-checklist.md for the launch-readiness gating.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	billingrail "github.com/aocybersystems/eden-platform-go/platform/billing-rail"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	featureflags "github.com/aocybersystems/eden-platform-go/platform/feature-flags"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/aocybersystems/eden-platform-go/platform/livekit"
	"github.com/google/uuid"
)

// childThirteenYearsAgo returns a birthdate that makes a member under 13 at
// test time. COPPA / GDPR-K classification keys off this value.
func childBirthdate(age int) *time.Time {
	t := time.Now().AddDate(-age, 0, 0)
	return &t
}

// signupResult is what TestEdenFamily_SignupFlow returns to its helpers /
// downstream sub-tests (composed via t.Run subtests inside other tests via
// reusable helpers). Each top-level test runs the wiring it needs.
type signupResult struct {
	HouseholdID      uuid.UUID
	PrimaryUserID    uuid.UUID
	PrimaryMemberID  uuid.UUID
	CoParentUserID   uuid.UUID
	CoParentMemberID uuid.UUID
	ChildUserID      uuid.UUID
	ChildMemberID    uuid.UUID
	PORID            uuid.UUID
}

// signupFamily is a reusable helper that runs the canonical Eden Family
// signup flow and returns the resulting IDs. Tests call this directly when
// the signup itself isn't the unit under test.
func signupFamily(t *testing.T, h *edenFamilyHarness) signupResult {
	t.Helper()
	ctx := context.Background()

	primary := uuid.New()
	hhAC, _ := h.auditCtx(primary)

	hh, err := h.Household.CreateHousehold(ctx, hhAC, "The Doe Family",
		json.RawMessage(`{"product":"eden-family"}`))
	if err != nil {
		t.Fatalf("CreateHousehold: %v", err)
	}

	primaryMember, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID:  hh.ID,
		UserID:       primary,
		Role:         household.RoleParentOfRecord,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		t.Fatalf("AddMember primary: %v", err)
	}

	coParent := uuid.New()
	coParentMember, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID:  hh.ID,
		UserID:       coParent,
		Role:         household.RoleParentOfRecord,
		Capabilities: household.DefaultCapabilities(household.RoleParentOfRecord),
	})
	if err != nil {
		t.Fatalf("AddMember coParent: %v", err)
	}

	childUser := uuid.New()
	childMember, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID:  hh.ID,
		UserID:       childUser,
		Role:         household.RoleChild,
		Birthdate:    childBirthdate(8),
		Capabilities: household.DefaultCapabilities(household.RoleChild),
	})
	if err != nil {
		t.Fatalf("AddMember child: %v", err)
	}

	por, err := h.Household.EstablishParentOfRecord(ctx, hhAC, childMember.ID, primaryMember.ID)
	if err != nil {
		t.Fatalf("EstablishParentOfRecord: %v", err)
	}

	return signupResult{
		HouseholdID:      hh.ID,
		PrimaryUserID:    primary,
		PrimaryMemberID:  primaryMember.ID,
		CoParentUserID:   coParent,
		CoParentMemberID: coParentMember.ID,
		ChildUserID:      childUser,
		ChildMemberID:    childMember.ID,
		PORID:            por.ID,
	}
}

// grantStandardConsents records the canonical Eden Family consent set for
// the child principal.
func grantStandardConsents(t *testing.T, h *edenFamilyHarness, s signupResult) []consent.Entry {
	t.Helper()
	ctx := context.Background()
	_, cAC := h.auditCtx(s.PrimaryUserID)

	purposes := []consent.Purpose{
		consent.PurposeChildAccountCreation,
		consent.PurposeAITutorInteraction,
	}
	var out []consent.Entry
	for _, p := range purposes {
		ev := consent.Evidence{
			Method:    "click_through",
			Recorded:  time.Now().UTC(),
			IPAddress: "127.0.0.1",
			Reference: "eden-family-onboarding-v1",
		}
		evJSON, err := ev.JSON()
		if err != nil {
			t.Fatalf("Evidence.JSON: %v", err)
		}
		entry, err := h.Consent.Grant(ctx, cAC, consent.GrantRequest{
			HouseholdID:        s.HouseholdID,
			PrincipalMemberID:  s.ChildMemberID,
			ConsenterMemberID:  s.PrimaryMemberID,
			Purpose:            p,
			ConsentTextVersion: "eden-family-tos-v1",
			Evidence:           evJSON,
		})
		if err != nil {
			t.Fatalf("Grant %s: %v", p, err)
		}
		out = append(out, entry)
	}
	return out
}

// --- Tests ----------------------------------------------------------------

func TestEdenFamily_SignupFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res := signupFamily(t, h)

	// Verify the household contains 3 active members (primary, co-parent, child).
	members, err := h.Household.ListMembers(ctx, res.HouseholdID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("ListMembers: got %d members, want 3", len(members))
	}

	// Parent-of-record link is queryable both directions.
	porsForChild, err := h.Household.ListParentsOfRecord(ctx, res.ChildMemberID)
	if err != nil {
		t.Fatalf("ListParentsOfRecord: %v", err)
	}
	if len(porsForChild) != 1 || porsForChild[0].ParentMemberID != res.PrimaryMemberID {
		t.Fatalf("ListParentsOfRecord: got %+v, want one POR pointing at primary", porsForChild)
	}

	childrenForParent, err := h.Household.ListChildrenForParent(ctx, res.PrimaryMemberID)
	if err != nil {
		t.Fatalf("ListChildrenForParent: %v", err)
	}
	if len(childrenForParent) != 1 || childrenForParent[0].ChildMemberID != res.ChildMemberID {
		t.Fatalf("ListChildrenForParent: got %+v, want one child", childrenForParent)
	}

	// Verify audit events fired for every mutation.
	events := h.DrainAudit()
	required := []string{
		household.ActionHouseholdCreated,
		household.ActionMemberAdded,
		household.ActionParentOfRecordEstablished,
	}
	for _, action := range required {
		if h.Audit.ActionCount(action) == 0 {
			t.Errorf("no %s event in audit log; got %d total events", action, len(events))
		}
	}
}

func TestEdenFamily_ChildAccountWithConsent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res := signupFamily(t, h)

	entries := grantStandardConsents(t, h, res)
	if len(entries) != 2 {
		t.Fatalf("grantStandardConsents: got %d entries, want 2", len(entries))
	}

	// Verify each consent is currently valid.
	_, cAC := h.auditCtx(res.PrimaryUserID)
	for _, p := range []consent.Purpose{
		consent.PurposeChildAccountCreation,
		consent.PurposeAITutorInteraction,
	} {
		v, err := h.Consent.IsValid(ctx, cAC, res.ChildMemberID, p, time.Now())
		if err != nil {
			t.Fatalf("IsValid %s: %v", p, err)
		}
		if !v.Valid {
			t.Errorf("IsValid %s: want valid", p)
		}
		if v.LatestEntry == nil {
			t.Errorf("IsValid %s: LatestEntry nil", p)
		}
	}

	// Audit events: consent.granted + consent.read for each purpose.
	h.DrainAudit()
	if got := h.Audit.ActionCount(consent.ActionConsentGranted); got < 2 {
		t.Errorf("consent.granted events = %d, want >= 2", got)
	}
	if got := h.Audit.ActionCount(consent.ActionConsentRead); got < 2 {
		t.Errorf("consent.read events = %d, want >= 2", got)
	}
}

func TestEdenFamily_ParentChildJWTSession(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)

	// Parent-mode token (no child id, child_mode=false).
	parentToken, err := h.JWT.CreateHouseholdAccessToken(
		res.PrimaryUserID.String(),
		res.HouseholdID.String(),
		"",
		false,
	)
	if err != nil {
		t.Fatalf("CreateHouseholdAccessToken parent: %v", err)
	}
	parentClaims, err := h.JWT.ValidateAccessToken(parentToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken parent: %v", err)
	}
	if parentClaims.HouseholdID != res.HouseholdID.String() {
		t.Errorf("parent claims.HouseholdID = %q, want %q", parentClaims.HouseholdID, res.HouseholdID.String())
	}
	if parentClaims.ChildMode {
		t.Errorf("parent claims.ChildMode = true, want false")
	}

	// Child-mode token.
	childToken, err := h.JWT.CreateHouseholdAccessToken(
		res.PrimaryUserID.String(),
		res.HouseholdID.String(),
		res.ChildUserID.String(),
		true,
	)
	if err != nil {
		t.Fatalf("CreateHouseholdAccessToken child: %v", err)
	}
	childClaims, err := h.JWT.ValidateAccessToken(childToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken child: %v", err)
	}
	if !childClaims.ChildMode {
		t.Errorf("child claims.ChildMode = false, want true")
	}
	if childClaims.ChildID != res.ChildUserID.String() {
		t.Errorf("child claims.ChildID = %q, want %q", childClaims.ChildID, res.ChildUserID.String())
	}

	// Exercise the middleware sentinel pathways.
	parentCtx := auth.WithClaims(context.Background(), parentClaims)
	if _, err := auth.RequireParentMode(parentCtx); err != nil {
		t.Errorf("RequireParentMode on parent ctx: %v", err)
	}
	if _, err := auth.RequireChildMode(parentCtx); !errors.Is(err, auth.ErrNotChildMode) {
		t.Errorf("RequireChildMode on parent ctx: got %v, want ErrNotChildMode", err)
	}
	hid, err := auth.RequireHousehold(parentCtx)
	if err != nil {
		t.Fatalf("RequireHousehold on parent ctx: %v", err)
	}
	if hid != res.HouseholdID {
		t.Errorf("RequireHousehold: got %s, want %s", hid, res.HouseholdID)
	}

	childCtx := auth.WithClaims(context.Background(), childClaims)
	if _, err := auth.RequireChildMode(childCtx); err != nil {
		t.Errorf("RequireChildMode on child ctx: %v", err)
	}
	if _, err := auth.RequireParentMode(childCtx); !errors.Is(err, auth.ErrNotParentMode) {
		t.Errorf("RequireParentMode on child ctx: got %v, want ErrNotParentMode", err)
	}
}

func TestEdenFamily_FeatureFlagGate(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)

	// Configure a tier-aware flag: group_calling is off for this household
	// (basic tier) and on for an override demonstrating "premium" via the
	// per-household override axis.
	h.FlagsSource.Set(featureflags.Flag{
		Key:     "group_calling",
		Enabled: true,
		Overrides: []featureflags.Override{
			{HouseholdID: res.HouseholdID.String(), Value: true},
		},
	})
	h.FlagsSource.Set(featureflags.Flag{
		Key:     "video_recording",
		Enabled: true,
		// No matching override; default for boolean flag without rollout = true.
		// To prove "off for non-premium", we test a household that has no
		// override and a flag with a 0% rollout.
	})
	h.FlagsSource.Set(featureflags.Flag{
		Key:     "premium_ai_tutor",
		Enabled: true,
		Rollout: &featureflags.Rollout{Percentage: 0, Salt: "premium_ai_tutor"},
	})

	ctx := context.Background()
	eval := featureflags.Eval{
		SubjectID:   res.PrimaryUserID.String(),
		HouseholdID: res.HouseholdID.String(),
	}
	if !h.FlagsClient.IsEnabled(ctx, "group_calling", eval) {
		t.Errorf("group_calling should be enabled for this household via override")
	}
	if !h.FlagsClient.IsEnabled(ctx, "video_recording", eval) {
		t.Errorf("video_recording should be on (boolean flag, no rollout)")
	}
	if h.FlagsClient.IsEnabled(ctx, "premium_ai_tutor", eval) {
		t.Errorf("premium_ai_tutor should be off at 0%% rollout")
	}

	// Unknown flag → off (fail-closed contract).
	if h.FlagsClient.IsEnabled(ctx, "nonexistent_flag", eval) {
		t.Errorf("unknown flag should be off")
	}
}

func TestEdenFamily_BillingRailSubscription(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)
	ctx := context.Background()

	customer := billingrail.Customer{
		ID:       res.HouseholdID.String(),
		Email:    "primary@doe.example",
		TenantID: h.TenantCompanyID.String(),
		Metadata: map[string]string{"household_id": res.HouseholdID.String()},
	}

	// 1) CreateSubscription via the rail. (In production, the consumer
	//    drives this from Eden-Biz; we exercise it to confirm the mock
	//    surface.)
	h.Rail.CreateSubscriptionResp = billingrail.SubscriptionResult{
		RailSubscriptionID: "sub_test_001",
		Status:             billingrail.SubStatusActive,
		CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
	}
	subResult, err := h.Rail.CreateSubscription(ctx, billingrail.SubscriptionRequest{
		PlanID:         "eden_family_basic",
		Customer:       customer,
		IdempotencyKey: "idemp_" + res.HouseholdID.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subResult.RailSubscriptionID == "" || subResult.Status != billingrail.SubStatusActive {
		t.Errorf("subResult = %+v, want active sub", subResult)
	}
	if got := len(h.Rail.CreateSubCalls); got != 1 {
		t.Errorf("CreateSubCalls = %d, want 1", got)
	}

	// 2) Webhook intake: simulate a subscription.created event arriving.
	occurredAt := time.Now().UTC().Truncate(time.Second)
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID:    "evt_test_001",
		Type:           billingrail.EventSubCreated,
		OccurredAt:     occurredAt,
		Customer:       customer,
		SubscriptionID: "sub_test_001",
		RailObject:     []byte(`{"id":"evt_test_001"}`),
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{"stripe-signature": "fake"}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle (sub.created): %v", err)
	}
	if got := len(h.Sink.SubscriptionCalls); got != 1 {
		t.Fatalf("Sink.SubscriptionCalls = %d, want 1", got)
	}
	if got := h.Sink.SubscriptionCalls[0].Event.Type; got != billingrail.EventSubCreated {
		t.Errorf("Sink event type = %q, want %q", got, billingrail.EventSubCreated)
	}
	if got := h.Sink.SubscriptionCalls[0].Event.RailName; got != "stripe" {
		t.Errorf("Sink event RailName = %q, want %q (dispatcher must set it)", got, "stripe")
	}

	// 3) Webhook intake: subscription.renewed.
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID:    "evt_test_002",
		Type:           billingrail.EventSubRenewed,
		OccurredAt:     occurredAt.Add(30 * 24 * time.Hour),
		Customer:       customer,
		SubscriptionID: "sub_test_001",
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle (sub.renewed): %v", err)
	}
	if got := len(h.Sink.SubscriptionCalls); got != 2 {
		t.Errorf("Sink.SubscriptionCalls = %d, want 2 after renewal", got)
	}
}

func TestEdenFamily_BillingRailChargeAndRefund(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)
	ctx := context.Background()

	customer := billingrail.Customer{
		ID:       res.HouseholdID.String(),
		Email:    "primary@doe.example",
		TenantID: h.TenantCompanyID.String(),
	}
	amt := billingrail.Money{AmountMinor: 999, Currency: "usd"}

	// Charge succeeded webhook.
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID: "evt_charge_001",
		Type:        billingrail.EventChargeSucceeded,
		OccurredAt:  time.Now().UTC(),
		Customer:    customer,
		Amount:      amt,
		ChargeID:    "ch_test_001",
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle charge: %v", err)
	}
	if got := len(h.Sink.ChargeCalls); got != 1 {
		t.Fatalf("Sink.ChargeCalls = %d, want 1", got)
	}
	if got := h.Sink.ChargeCalls[0].Result.Status; got != billingrail.ChargeStatusSucceeded {
		t.Errorf("charge status = %v, want succeeded", got)
	}

	// Refund webhook.
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID: "evt_refund_001",
		Type:        billingrail.EventChargeRefunded,
		OccurredAt:  time.Now().UTC(),
		Customer:    customer,
		Amount:      amt,
		ChargeID:    "ch_test_001",
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle refund: %v", err)
	}
	if got := len(h.Sink.RefundCalls); got != 1 {
		t.Fatalf("Sink.RefundCalls = %d, want 1", got)
	}
	if got := h.Sink.RefundCalls[0].Result.Status; got != billingrail.RefundStatusSucceeded {
		t.Errorf("refund status = %v, want succeeded", got)
	}

	// Invalid signature → no sink call (dispatcher returns ErrInvalidSignature).
	h.Rail.ParseWebhookErr = billingrail.ErrInvalidSignature
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{} // not relevant
	prevSubs, prevCharges, prevRefunds := len(h.Sink.SubscriptionCalls), len(h.Sink.ChargeCalls), len(h.Sink.RefundCalls)
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`bad`)); !errors.Is(err, billingrail.ErrInvalidSignature) {
		t.Errorf("Dispatcher.Handle bad sig: got %v, want ErrInvalidSignature", err)
	}
	if len(h.Sink.SubscriptionCalls) != prevSubs ||
		len(h.Sink.ChargeCalls) != prevCharges ||
		len(h.Sink.RefundCalls) != prevRefunds {
		t.Errorf("sink saw a call after bad-signature reject")
	}
	h.Rail.ParseWebhookErr = nil // reset

	// Subscription cancellation webhook.
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID:    "evt_cancel_001",
		Type:           billingrail.EventSubCanceled,
		OccurredAt:     time.Now().UTC(),
		Customer:       customer,
		SubscriptionID: "sub_test_001",
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle cancel: %v", err)
	}
	if got := len(h.Sink.SubscriptionCalls); got != 1 {
		t.Errorf("Sink.SubscriptionCalls after cancel = %d, want 1", got)
	}
}

func TestEdenFamily_ConsentRevocationGatesAI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res := signupFamily(t, h)
	entries := grantStandardConsents(t, h, res)

	// Helper: gate function representing an AI tutor endpoint. Returns nil
	// when consent is valid, error when not.
	aiTutorGate := func() error {
		_, cAC := h.auditCtx(res.PrimaryUserID)
		v, err := h.Consent.IsValid(ctx, cAC, res.ChildMemberID, consent.PurposeAITutorInteraction, time.Now())
		if err != nil {
			return err
		}
		if !v.Valid {
			return errors.New("consent revoked")
		}
		return nil
	}

	// Initially valid.
	if err := aiTutorGate(); err != nil {
		t.Fatalf("aiTutorGate pre-revoke: %v", err)
	}

	// Revoke the AI tutor consent.
	var aiEntry consent.Entry
	for _, e := range entries {
		if e.Purpose == consent.PurposeAITutorInteraction {
			aiEntry = e
			break
		}
	}
	if aiEntry.ID == uuid.Nil {
		t.Fatalf("no AI tutor consent entry to revoke")
	}

	_, cAC := h.auditCtx(res.PrimaryUserID)
	revEvidence := consent.Evidence{
		Method:    "click_through",
		Recorded:  time.Now().UTC(),
		IPAddress: "127.0.0.1",
		Reference: "parent-revocation",
	}
	revJSON, _ := revEvidence.JSON()
	if _, err := h.Consent.Revoke(ctx, cAC, aiEntry.ID, res.PrimaryMemberID, revJSON); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Gate now denies.
	if err := aiTutorGate(); err == nil {
		t.Fatalf("aiTutorGate post-revoke: expected error, got nil")
	}

	// Child account creation consent is unaffected.
	v, err := h.Consent.IsValid(ctx, cAC, res.ChildMemberID, consent.PurposeChildAccountCreation, time.Now())
	if err != nil {
		t.Fatalf("IsValid child_account_creation: %v", err)
	}
	if !v.Valid {
		t.Errorf("child_account_creation should still be valid after AI revoke")
	}

	h.DrainAudit()
	if got := h.Audit.ActionCount(consent.ActionConsentRevoked); got == 0 {
		t.Errorf("no consent.revoked audit event")
	}

	// Double-revoke is idempotent — returns ErrAlreadyRevoked.
	if _, err := h.Consent.Revoke(ctx, cAC, aiEntry.ID, res.PrimaryMemberID, revJSON); !errors.Is(err, consent.ErrAlreadyRevoked) {
		t.Errorf("double Revoke: got %v, want ErrAlreadyRevoked", err)
	}
}

func TestEdenFamily_OneToOneVideoCall(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res := signupFamily(t, h)

	// Caller = primary parent's user id; callee = co-parent's user id.
	call, err := h.LiveKit.InitiateCall(ctx, res.PrimaryUserID, res.CoParentUserID, livekit.CallTypeVideo)
	if err != nil {
		t.Fatalf("InitiateCall: %v", err)
	}
	if call.State != livekit.StateRinging {
		t.Errorf("call state = %s, want ringing", call.State)
	}

	// Callee accepts.
	accepted, err := h.LiveKit.AcceptCall(ctx, call.ID, res.CoParentUserID)
	if err != nil {
		t.Fatalf("AcceptCall: %v", err)
	}
	if accepted.LiveKitURL != h.LiveKitURL {
		t.Errorf("livekit url = %q, want %q", accepted.LiveKitURL, h.LiveKitURL)
	}
	if accepted.CalleeToken == "" {
		t.Errorf("callee token empty")
	}
	if accepted.Call.State != livekit.StateConnecting {
		t.Errorf("post-accept state = %s, want connecting", accepted.Call.State)
	}

	// Mark connected (the webhook event path).
	if err := h.LiveKit.MarkConnected(ctx, call.ID); err != nil {
		t.Fatalf("MarkConnected: %v", err)
	}

	// End the call from the caller.
	if err := h.LiveKit.EndCall(ctx, call.ID, res.PrimaryUserID); err != nil {
		t.Fatalf("EndCall: %v", err)
	}

	// Final state.
	final, err := h.LiveKit.Store().GetCall(ctx, call.ID)
	if err != nil {
		t.Fatalf("Store.GetCall: %v", err)
	}
	if final.State != livekit.StateEnded {
		t.Errorf("final state = %s, want ended", final.State)
	}

	// Signaler captured the invite + accept + end.
	events := h.Signaler.Events()
	if len(events) < 3 {
		t.Errorf("signaler events = %d, want >= 3 (invite, accept, end)", len(events))
	}

	// Room was created with the expected name shape.
	if got := len(h.Rooms.created); got == 0 {
		t.Errorf("rooms created = %d, want >= 1", got)
	} else if !strings.HasPrefix(h.Rooms.created[0], "call-") {
		t.Errorf("room name = %q, want call-* prefix", h.Rooms.created[0])
	}
}

func TestEdenFamily_NegativeChildCannotActAsParent(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)

	// Issue a child-mode token for the child principal.
	childToken, err := h.JWT.CreateHouseholdAccessToken(
		res.ChildUserID.String(),
		res.HouseholdID.String(),
		res.ChildUserID.String(),
		true,
	)
	if err != nil {
		t.Fatalf("CreateHouseholdAccessToken: %v", err)
	}
	claims, err := h.JWT.ValidateAccessToken(childToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	ctx := auth.WithClaims(context.Background(), claims)

	// A handler that requires parent mode (e.g. "manage billing") rejects.
	if _, err := auth.RequireParentMode(ctx); !errors.Is(err, auth.ErrNotParentMode) {
		t.Errorf("RequireParentMode on child token: got %v, want ErrNotParentMode", err)
	}

	// Without any claims, both require helpers reject with ErrNoHousehold.
	emptyCtx := context.Background()
	if _, err := auth.RequireParentMode(emptyCtx); !errors.Is(err, auth.ErrNoHousehold) {
		t.Errorf("RequireParentMode on empty ctx: got %v, want ErrNoHousehold", err)
	}
	if _, err := auth.RequireChildMode(emptyCtx); !errors.Is(err, auth.ErrNoHousehold) {
		t.Errorf("RequireChildMode on empty ctx: got %v, want ErrNoHousehold", err)
	}
}

func TestEdenFamily_NegativeChildAccountRequiresEligibleParent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Build a household where the would-be POR is an adult non-parent.
	primary := uuid.New()
	hhAC, _ := h.auditCtx(primary)
	hh, err := h.Household.CreateHousehold(ctx, hhAC, "Test", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CreateHousehold: %v", err)
	}
	// "Cousin" is RoleAdultNonParent — not eligible for POR.
	cousinUserID := uuid.New()
	cousin, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID:  hh.ID,
		UserID:       cousinUserID,
		Role:         household.RoleAdultNonParent,
		Capabilities: household.DefaultCapabilities(household.RoleAdultNonParent),
	})
	if err != nil {
		t.Fatalf("AddMember cousin: %v", err)
	}
	childUserID := uuid.New()
	child, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID: hh.ID,
		UserID:      childUserID,
		Role:        household.RoleChild,
		Birthdate:   childBirthdate(7),
	})
	if err != nil {
		t.Fatalf("AddMember child: %v", err)
	}

	if _, err := h.Household.EstablishParentOfRecord(ctx, hhAC, child.ID, cousin.ID); !errors.Is(err, household.ErrParentNotEligible) {
		t.Errorf("EstablishParentOfRecord cousin->child: got %v, want ErrParentNotEligible", err)
	}

	// Adding a child without a birthdate must also fail (COPPA gate).
	if _, err := h.Household.AddMember(ctx, hhAC, household.Member{
		HouseholdID: hh.ID,
		UserID:      uuid.New(),
		Role:        household.RoleChild,
	}); !errors.Is(err, household.ErrChildBirthdateRequired) {
		t.Errorf("AddMember child no-birthdate: got %v, want ErrChildBirthdateRequired", err)
	}
}

func TestEdenFamily_AuditTrailEndToEnd(t *testing.T) {
	h := newHarness(t)
	res := signupFamily(t, h)
	grantStandardConsents(t, h, res)

	// Run an IsValid + Revoke + IsValid sequence to fan-out audit reads.
	ctx := context.Background()
	_, cAC := h.auditCtx(res.PrimaryUserID)
	v, err := h.Consent.IsValid(ctx, cAC, res.ChildMemberID, consent.PurposeAITutorInteraction, time.Now())
	if err != nil {
		t.Fatalf("IsValid: %v", err)
	}
	if v.LatestEntry == nil {
		t.Fatalf("LatestEntry nil")
	}
	if _, err := h.Consent.Revoke(ctx, cAC, v.LatestEntry.ID, res.PrimaryMemberID, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	events := h.DrainAudit()
	// Every action constant we exercise must appear at least once.
	wantActions := []string{
		household.ActionHouseholdCreated,
		household.ActionMemberAdded,
		household.ActionParentOfRecordEstablished,
		consent.ActionConsentGranted,
		consent.ActionConsentRead,
		consent.ActionConsentRevoked,
	}
	for _, a := range wantActions {
		if h.Audit.ActionCount(a) == 0 {
			t.Errorf("audit log missing action %q (total events: %d)", a, len(events))
		}
	}

	// Every event has CompanyID matching the harness tenant.
	for _, e := range events {
		if e.CompanyID != h.TenantCompanyID.String() {
			t.Errorf("audit event company id = %q, want %q (action=%s)", e.CompanyID, h.TenantCompanyID.String(), e.Action)
		}
	}
}

func TestEdenFamily_FullJourney(t *testing.T) {
	// The integration scenario in TRD 33-01 strung end-to-end. Each step
	// composes the platform packages a real Eden Family caller would touch.
	h := newHarness(t)
	ctx := context.Background()

	// Step 1: signup
	res := signupFamily(t, h)

	// Step 2: consent
	entries := grantStandardConsents(t, h, res)
	if len(entries) != 2 {
		t.Fatalf("consent entries = %d, want 2", len(entries))
	}

	// Step 3: AO ID equivalent — issue a household-aware JWT.
	parentToken, err := h.JWT.CreateHouseholdAccessToken(
		res.PrimaryUserID.String(),
		res.HouseholdID.String(),
		"",
		false,
	)
	if err != nil {
		t.Fatalf("CreateHouseholdAccessToken: %v", err)
	}
	claims, err := h.JWT.ValidateAccessToken(parentToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	authedCtx := auth.WithClaims(ctx, claims)
	if _, err := auth.RequireHousehold(authedCtx); err != nil {
		t.Fatalf("RequireHousehold: %v", err)
	}

	// Step 4: feature flag gate.
	h.FlagsSource.Set(featureflags.Flag{
		Key:     "family_video_calls",
		Enabled: true,
	})
	if !h.FlagsClient.IsEnabled(ctx, "family_video_calls", featureflags.Eval{
		HouseholdID: res.HouseholdID.String(),
	}) {
		t.Fatalf("family_video_calls should be on")
	}

	// Step 5: billing — Stripe-style subscription.created webhook arrives.
	customer := billingrail.Customer{
		ID:       res.HouseholdID.String(),
		TenantID: h.TenantCompanyID.String(),
	}
	h.Rail.ParseWebhookResp = billingrail.WebhookEvent{
		RailEventID:    "evt_journey",
		Type:           billingrail.EventSubCreated,
		OccurredAt:     time.Now().UTC(),
		Customer:       customer,
		SubscriptionID: "sub_journey",
	}
	if err := h.Dispatcher.Handle(ctx, map[string]string{}, []byte(`{}`)); err != nil {
		t.Fatalf("Dispatcher.Handle: %v", err)
	}

	// Step 6: livekit call between the two parents.
	call, err := h.LiveKit.InitiateCall(ctx, res.PrimaryUserID, res.CoParentUserID, livekit.CallTypeVideo)
	if err != nil {
		t.Fatalf("InitiateCall: %v", err)
	}
	if _, err := h.LiveKit.AcceptCall(ctx, call.ID, res.CoParentUserID); err != nil {
		t.Fatalf("AcceptCall: %v", err)
	}
	if err := h.LiveKit.MarkConnected(ctx, call.ID); err != nil {
		t.Fatalf("MarkConnected: %v", err)
	}
	if err := h.LiveKit.EndCall(ctx, call.ID, res.PrimaryUserID); err != nil {
		t.Fatalf("EndCall: %v", err)
	}

	// Final invariant: audit log records every meaningful action.
	h.DrainAudit()
	for _, a := range []string{
		household.ActionHouseholdCreated,
		household.ActionMemberAdded,
		household.ActionParentOfRecordEstablished,
		consent.ActionConsentGranted,
	} {
		if h.Audit.ActionCount(a) == 0 {
			t.Errorf("end-to-end journey missing audit action %q", a)
		}
	}
}
