package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAction_String(t *testing.T) {
	if got := ActionUserLogin.String(); got != "auth.user.login" {
		t.Errorf("ActionUserLogin = %q, want %q", got, "auth.user.login")
	}
}

func TestEvent_WithDetail(t *testing.T) {
	e := Event{}.WithDetail("k", "v")
	if e.Details["k"] != "v" {
		t.Errorf("Details[k] = %v, want v", e.Details["k"])
	}
	// Chain still works on populated map.
	e = e.WithDetail("k2", 42)
	if e.Details["k2"] != 42 {
		t.Errorf("Details[k2] = %v, want 42", e.Details["k2"])
	}
}

func TestEvent_WithBeforeAfter(t *testing.T) {
	type s struct{ V int }
	e := Event{}.WithBeforeAfter(s{1}, s{2})
	if e.Details[DetailBefore].(s).V != 1 {
		t.Errorf("before = %v, want {1}", e.Details[DetailBefore])
	}
	if e.Details[DetailAfter].(s).V != 2 {
		t.Errorf("after = %v, want {2}", e.Details[DetailAfter])
	}
}

func TestEvent_WithRequestID(t *testing.T) {
	e := Event{}.WithRequestID("")
	if _, ok := e.Details[DetailRequestID]; ok {
		t.Errorf("empty request id should be ignored")
	}
	e = Event{}.WithRequestID("rid-123")
	if e.Details[DetailRequestID] != "rid-123" {
		t.Errorf("request id = %v, want rid-123", e.Details[DetailRequestID])
	}
}

func TestEvent_WithReason(t *testing.T) {
	e := Event{}.WithReason("")
	if _, ok := e.Details[DetailReason]; ok {
		t.Errorf("empty reason should be ignored")
	}
	e = Event{}.WithReason("expired")
	if e.Details[DetailReason] != "expired" {
		t.Errorf("reason = %v, want expired", e.Details[DetailReason])
	}
}

func TestEvent_WithAction(t *testing.T) {
	e := Event{}.WithAction(ActionUserLogin)
	if e.Action != "auth.user.login" {
		t.Errorf("Action = %q, want auth.user.login", e.Action)
	}
}

func TestEventFromHTTP_NilRequest(t *testing.T) {
	e := EventFromHTTP(nil)
	if e.IPAddress != "" || e.Details != nil {
		t.Errorf("nil request should yield zero event, got %+v", e)
	}
}

func TestEventFromHTTP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	r.Header.Set("User-Agent", "ua/1.0")
	r.Header.Set("X-Request-ID", "rid-9")

	e := EventFromHTTP(r)
	if e.IPAddress != "203.0.113.5" {
		t.Errorf("IP = %q, want 203.0.113.5", e.IPAddress)
	}
	if e.Details[DetailUserAgent] != "ua/1.0" {
		t.Errorf("user agent = %v, want ua/1.0", e.Details[DetailUserAgent])
	}
	if e.Details[DetailRequestID] != "rid-9" {
		t.Errorf("request id = %v, want rid-9", e.Details[DetailRequestID])
	}
}

func TestEventFromHTTP_XRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "198.51.100.7")
	e := EventFromHTTP(r)
	if e.IPAddress != "198.51.100.7" {
		t.Errorf("IP = %q, want 198.51.100.7", e.IPAddress)
	}
}

func TestEventFromHTTP_RemoteAddrFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.10:54321"
	e := EventFromHTTP(r)
	if e.IPAddress != "192.0.2.10" {
		t.Errorf("IP = %q, want 192.0.2.10", e.IPAddress)
	}
}

func TestEventFromHTTP_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "no-port"
	e := EventFromHTTP(r)
	if e.IPAddress != "no-port" {
		t.Errorf("IP = %q, want no-port", e.IPAddress)
	}
}

func TestEventFromHTTP_XFFSingleHop(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "  203.0.113.99  ")
	e := EventFromHTTP(r)
	if e.IPAddress != "203.0.113.99" {
		t.Errorf("IP = %q, want 203.0.113.99 (trimmed)", e.IPAddress)
	}
}

func TestLogger_LogSync_NilStore(t *testing.T) {
	l := NewLogger(nil)
	if err := l.LogSync(context.Background(), Event{}); err == nil {
		t.Errorf("expected error for nil store")
	}
}

func TestLogger_LogSync_InvalidCompanyID(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: "not-a-uuid",
		ActorID:   uuid.New().String(),
	})
	if err == nil {
		t.Errorf("expected error for invalid company id")
	}
}

func TestLogger_LogSync_InvalidActorID(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: uuid.New().String(),
		ActorID:   "not-a-uuid",
	})
	if err == nil {
		t.Errorf("expected error for invalid actor id")
	}
}

func TestLogger_LogSync_Success(t *testing.T) {
	store := &mockAuditStore{}
	l := NewLogger(store)
	err := l.LogSync(context.Background(), Event{
		CompanyID: uuid.New().String(),
		ActorID:   uuid.New().String(),
		Action:    ActionUserLogin.String(),
		Resource:  "user",
	}.WithDetail("k", "v"))
	if err != nil {
		t.Fatalf("LogSync error = %v, want nil", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Errorf("store events = %d, want 1", len(store.events))
	}
	if store.events[0].Action != "auth.user.login" {
		t.Errorf("action = %q, want auth.user.login", store.events[0].Action)
	}
}

// Test list (TRD 02-03 — identity lifecycle Action constants):
//   - TestActionConstants_AllUnique
//     Every Action constant added in TRD 02-03 has a unique string value, with
//     no collision against pre-existing audit, RBAC, or generic constants.
//   - TestIdentityActions_NamespacePrefix
//     All 16 new identity lifecycle constants carry the "identity." prefix in
//     their string value (operator-grep contract — AOAudit + Obj 9 pipeline
//     filter on prefix).
//   - TestIdentityActions_StringValuesExact
//     Lock the exact RESEARCH.md Task 12 string-value contract. Downstream
//     consumers parse on these strings — renaming requires a coordinated
//     cross-repo change.
//
// Naming-collision note: the pre-existing RBAC namespace defines
// ActionRoleGrant ("rbac.role.grant") and ActionRoleRevoke ("rbac.role.revoke").
// To avoid Go identifier collision, the TRD 02-03 identity-domain role
// constants use the Identity prefix:
//   - ActionIdentityRoleCreate ("identity.role.create")
//   - ActionIdentityRoleAssign ("identity.role.assign")
//   - ActionIdentityRoleRevoke ("identity.role.revoke")
// This deviation from RESEARCH.md Task 12 is documented in the TRD SUMMARY.

// identityActions is the canonical list of Action constants added in TRD 02-03.
// Updating this list when adding a new identity constant is part of the
// contract: keeps failures immediately actionable (which constant was missed).
func identityActions() map[string]Action {
	return map[string]Action{
		// Account lifecycle
		"ActionAccountCreate":  ActionAccountCreate,
		"ActionAccountUpdate":  ActionAccountUpdate,
		"ActionAccountSuspend": ActionAccountSuspend,
		"ActionAccountRecover": ActionAccountRecover,
		"ActionAccountDelete":  ActionAccountDelete,
		"ActionAccountExpire":  ActionAccountExpire,
		// Groups + group membership
		"ActionGroupCreate":       ActionGroupCreate,
		"ActionGroupDelete":       ActionGroupDelete,
		"ActionGroupMemberAdd":    ActionGroupMemberAdd,
		"ActionGroupMemberRemove": ActionGroupMemberRemove,
		// Roles + role bindings (Identity-prefixed to avoid RBAC collision)
		"ActionIdentityRoleCreate": ActionIdentityRoleCreate,
		"ActionIdentityRoleAssign": ActionIdentityRoleAssign,
		"ActionIdentityRoleRevoke": ActionIdentityRoleRevoke,
		// Entitlement attributes
		"ActionEntitlementSet":    ActionEntitlementSet,
		"ActionEntitlementDelete": ActionEntitlementDelete,
		// Tenants (super-admin only)
		"ActionTenantCreate": ActionTenantCreate,
	}
}

// preExistingActions is the pre-TRD-02-03 set. Used to assert that the new
// identity constants do not collide with any existing audit-string value.
// Keep in sync with the const block in event.go (auth.*, generic.*, rbac.*).
func preExistingActions() map[string]Action {
	return map[string]Action{
		"ActionUserLogin":    ActionUserLogin,
		"ActionUserLogout":   ActionUserLogout,
		"ActionUserSignup":   ActionUserSignup,
		"ActionUserPwReset":  ActionUserPwReset,
		"ActionTokenRefresh": ActionTokenRefresh,
		"ActionAPIKeyCreate": ActionAPIKeyCreate,
		"ActionAPIKeyRevoke": ActionAPIKeyRevoke,
		"ActionAPIKeyRotate": ActionAPIKeyRotate,
		"ActionCreate":       ActionCreate,
		"ActionUpdate":       ActionUpdate,
		"ActionDelete":       ActionDelete,
		"ActionRoleGrant":    ActionRoleGrant,
		"ActionRoleRevoke":   ActionRoleRevoke, // RBAC-namespaced; "rbac.role.revoke"
	}
}

func TestActionConstants_AllUnique(t *testing.T) {
	all := map[string]Action{}
	for k, v := range preExistingActions() {
		all[k] = v
	}
	for k, v := range identityActions() {
		all[k] = v
	}

	seen := map[string]string{} // string value -> first constant name
	for name, val := range all {
		s := val.String()
		if s == "" {
			t.Errorf("%s has empty string value", name)
			continue
		}
		if existing, ok := seen[s]; ok {
			t.Fatalf("audit.Action duplicate string value %q: %s collides with %s", s, name, existing)
		}
		seen[s] = name
	}
}

func TestIdentityActions_NamespacePrefix(t *testing.T) {
	ids := identityActions()
	if len(ids) != 16 {
		t.Fatalf("TRD 02-03 ships exactly 16 identity actions; got %d", len(ids))
	}
	for name, a := range ids {
		s := a.String()
		if len(s) <= 9 || s[:9] != "identity." {
			t.Errorf("%s must have 'identity.' prefix; got %q", name, s)
		}
	}
}

func TestIdentityActions_StringValuesExact(t *testing.T) {
	// Lock the exact string values per RESEARCH.md Task 12. Downstream
	// consumers (AOAudit, Obj 9 pipeline) treat these strings as a stable
	// contract — renaming requires a coordinated cross-repo change.
	want := map[Action]string{
		ActionAccountCreate:      "identity.account.create",
		ActionAccountUpdate:      "identity.account.update",
		ActionAccountSuspend:     "identity.account.suspend",
		ActionAccountRecover:     "identity.account.recover",
		ActionAccountDelete:      "identity.account.delete",
		ActionAccountExpire:      "identity.account.expire",
		ActionGroupCreate:        "identity.group.create",
		ActionGroupDelete:        "identity.group.delete",
		ActionGroupMemberAdd:     "identity.group.member.add",
		ActionGroupMemberRemove:  "identity.group.member.remove",
		ActionIdentityRoleCreate: "identity.role.create",
		ActionIdentityRoleAssign: "identity.role.assign",
		ActionIdentityRoleRevoke: "identity.role.revoke",
		ActionEntitlementSet:     "identity.entitlement.set",
		ActionEntitlementDelete:  "identity.entitlement.delete",
		ActionTenantCreate:       "identity.tenant.create",
	}
	if len(want) != 16 {
		t.Fatalf("test table out of sync: expected 16 entries, got %d", len(want))
	}
	for a, expected := range want {
		if got := a.String(); got != expected {
			t.Errorf("Action %q = %q, want %q", expected, got, expected)
		}
	}
}

// ============================================================================
// TRD 09-01 — backward-compat + Obj 9 Event/Action coverage.
//
// These tests lock the AUD-* + LIFE-* contract so a future refactor cannot
// silently rename a wire-string value, drop a struct field, or break
// existing Obj 2 emitters by adding the Obj 9 fields.
// ============================================================================

// TestEvent_BackwardCompat_EmptyEventMarshals confirms that an Event{}
// zero-value (no Obj 9 fields populated) still flows cleanly through
// Logger.LogSync, matching the Obj 2 baseline. AOID's pre-Obj-9 emitters
// rely on this: they fill only the original CompanyID/ActorID/Action/...
// fields and trust the new fields to be nil-safe.
func TestEvent_BackwardCompat_EmptyEventMarshals(t *testing.T) {
	companyID := uuid.New()
	actorID := uuid.New()
	e := Event{
		CompanyID:  companyID.String(),
		ActorID:    actorID.String(),
		Action:     ActionAccountCreate.String(),
		Resource:   "aoid.account",
		ResourceID: uuid.New().String(),
	}
	// New Obj 9 fields default to zero / nil.
	if e.Decision != "" {
		t.Errorf("Decision should default empty, got %q", e.Decision)
	}
	if e.ActorKind != "" {
		t.Errorf("ActorKind should default empty, got %q", e.ActorKind)
	}
	if e.MFA != nil || e.Federation != nil || e.Risk != nil {
		t.Errorf("MFA/Federation/Risk should default nil; got MFA=%v Federation=%v Risk=%v", e.MFA, e.Federation, e.Risk)
	}

	store := &mockAuditStore{}
	logger := NewLogger(store)
	if err := logger.LogSync(context.Background(), e); err != nil {
		t.Fatalf("LogSync error = %v, want nil", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Fatalf("store events = %d, want 1", len(store.events))
	}
	if store.events[0].Action != "identity.account.create" {
		t.Errorf("action = %q, want identity.account.create", store.events[0].Action)
	}
}

// TestEvent_Obj9Fields_PopulateInDetails confirms the new Event fields
// (MFA, Federation, Risk, Decision, ActorKind, SubjectID, SubjectKind)
// JSON-marshal cleanly. The SignedStore in TRD 09-02 does the canonical
// encoding; this test is a smoke check that the types serialize at all.
func TestEvent_Obj9Fields_PopulateInDetails(t *testing.T) {
	e := Event{
		CompanyID:   uuid.New().String(),
		ActorID:     uuid.New().String(),
		Action:      ActionAuthAttempt.String(),
		Decision:    "allow",
		ActorKind:   "human",
		SubjectID:   uuid.New().String(),
		SubjectKind: "account",
		MFA: &MFAAttestation{
			Presented:       []string{"totp"},
			Verified:        []string{"totp"},
			StepUpSatisfied: true,
			AALAchieved:     "AAL2",
		},
		Federation: []FederationLink{{IDP: "logingov", Level: "IAL2", TrustLink: "oidc"}},
		Risk: &RiskAttestation{
			Score:   12,
			Signals: []RiskSignal{{Signal: "new_geo_country", Weight: 12}},
		},
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"aal_achieved":"AAL2"`,
		`"score":12`,
		`"signal":"new_geo_country"`,
		`"idp":"logingov"`,
		`"Decision":"allow"`,
		`"ActorKind":"human"`,
		`"SubjectKind":"account"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("marshalled event missing %q\nfull JSON: %s", want, s)
		}
	}
}

// TestAction_Obj9Constants_HaveStableValues locks the string values of the
// 17 new Action constants so a future PR cannot accidentally rename them.
// Downstream consumers (AOAudit, Obj 9 dashboards, OPA rules) parse on
// the wire strings — renaming is a coordinated cross-repo event.
func TestAction_Obj9Constants_HaveStableValues(t *testing.T) {
	cases := map[Action]string{
		ActionAuthAttempt:                 "auth.attempt",
		ActionAuthAttemptFailed:           "auth.attempt.failed",
		ActionFederationAssertionAccepted: "federation.assertion.accepted",
		ActionFederationAssertionRejected: "federation.assertion.rejected",
		ActionFederationJITUserCreated:    "federation.jit.user_created",
		ActionRecoveryRequested:           "identity.account.recovery.requested",
		ActionRecoveryCompleted:           "identity.account.recovery.completed",
		ActionRecoveryRefused:             "identity.account.recovery.refused",
		ActionRecertReviewCreated:         "identity.recertification.review_created",
		ActionRecertDecision:              "identity.recertification.decision",
		ActionRecertExpired:               "identity.recertification.expired",
		ActionDormantWarning:              "identity.account.dormant_warning_sent",
		ActionDormantAutoSuspend:          "identity.account.dormant_auto_suspend",
		ActionMFAClearedByAdmin:           "auth.mfa.cleared_by_admin",
		ActionAC2ReportRead:               "identity.ac2_evidence.read",
		ActionAuditQueryRead:              "identity.audit_query.read",
		ActionCredentialIssued:            "auth.credential.issued",
	}
	if len(cases) != 17 {
		t.Fatalf("TRD 09-01 ships exactly 17 new Action constants; got %d", len(cases))
	}
	for got, want := range cases {
		if got.String() != want {
			t.Errorf("constant value drifted: %q != %q", got.String(), want)
		}
	}
	// Stable cross-objective: ActionEventResigned (added in TRD 09-02 const block) stays.
	if ActionEventResigned.String() != "aoid.audit.event_resigned" {
		t.Errorf("ActionEventResigned drifted: %q", ActionEventResigned.String())
	}
}
