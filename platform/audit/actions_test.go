package audit

import "testing"

// Test list (TRD 06-03 — federation Action constants):
//   - TestActionConstants_Obj6_Uniqueness
//     Every new Obj-6 federation Action constant has a unique string value, no
//     collision against any prior-Obj constant (auth.*, generic.*, rbac.*,
//     identity.*).
//   - TestActionConstants_Obj6_Regression
//     All Action constants from prior Obj 1/2/3/4/5 TRDs remain present with
//     their original string values. Guards against accidental deletion or
//     value-mutation in this file or in event.go.
//   - TestActionConstants_Obj6_StringValuesExact
//     Lock the exact string-value contract for federation events. Downstream
//     consumers (AOID federation service, Obj 9 audit dashboards) parse on
//     these strings — renaming requires a coordinated cross-repo change.
//   - TestRejectReasons_Uniqueness
//     The exported RejectReasons slice contains no duplicate codes; it is the
//     vetted finite set ActionFederationReject emissions draw from.
//   - TestRejectReasons_ExactContents
//     Lock the exact membership of RejectReasons — adding a new code is a
//     deliberate cross-repo event (audit dashboards key on these codes).

// obj6FederationActions is the canonical list of Action constants added in
// TRD 06-03. Updating this list when adding a new federation constant is part
// of the contract: keeps failures immediately actionable (which constant was
// missed).
func obj6FederationActions() map[string]Action {
	return map[string]Action{
		// Inbound assertion decisions
		"ActionFederationAccept": ActionFederationAccept,
		"ActionFederationReject": ActionFederationReject,
		// JIT account provisioning
		"ActionAccountCreatedJIT": ActionAccountCreatedJIT,
		// Admin RPC lifecycle (IdP / policy / SP / client)
		"ActionFederationIdPConfigured":    ActionFederationIdPConfigured,
		"ActionFederationIdPRevoked":       ActionFederationIdPRevoked,
		"ActionFederationPolicyChanged":    ActionFederationPolicyChanged,
		"ActionDownstreamSPRegistered":     ActionDownstreamSPRegistered,
		"ActionDownstreamClientRegistered": ActionDownstreamClientRegistered,
	}
}

func TestActionConstants_Obj6_Uniqueness(t *testing.T) {
	// Aggregate every Action constant the package knows about (pre-existing +
	// identity from TRD 02-03 + federation from TRD 06-03) and assert no two
	// share a string value.
	all := map[string]Action{}
	for k, v := range preExistingActions() {
		all[k] = v
	}
	for k, v := range identityActions() {
		all[k] = v
	}
	for k, v := range obj6FederationActions() {
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

func TestActionConstants_Obj6_Regression(t *testing.T) {
	// Snapshot of prior-Obj constant string values. If any of these change,
	// downstream consumers (AOAudit pipeline, Obj 9 dashboards, AOID federation
	// service emitting against the typed constants) will silently mis-classify
	// events. This test pins the contract.
	want := map[Action]string{
		// Pre-existing (auth / generic / rbac).
		ActionUserLogin:    "auth.user.login",
		ActionUserLogout:   "auth.user.logout",
		ActionUserSignup:   "auth.user.signup",
		ActionUserPwReset:  "auth.user.password_reset",
		ActionTokenRefresh: "auth.token.refresh",
		ActionAPIKeyCreate: "auth.apikey.create",
		ActionAPIKeyRevoke: "auth.apikey.revoke",
		ActionAPIKeyRotate: "auth.apikey.rotate",
		ActionCreate:       "generic.create",
		ActionUpdate:       "generic.update",
		ActionDelete:       "generic.delete",
		ActionRoleGrant:    "rbac.role.grant",
		ActionRoleRevoke:   "rbac.role.revoke",
		// Identity lifecycle (TRD 02-03).
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
	if len(want) != 29 {
		t.Fatalf("regression test snapshot out of sync: expected 29 prior-Obj constants, got %d", len(want))
	}
	for a, expected := range want {
		if got := a.String(); got != expected {
			t.Errorf("Action %q regressed: got %q, want %q", expected, got, expected)
		}
	}
}

func TestActionConstants_Obj6_StringValuesExact(t *testing.T) {
	// Lock the exact string values for Obj-6 federation constants. Downstream
	// consumers parse on these strings — renaming requires a coordinated
	// cross-repo change.
	want := map[Action]string{
		ActionFederationAccept:           "federation.accept",
		ActionFederationReject:           "federation.reject",
		ActionAccountCreatedJIT:          "account.create.jit",
		ActionFederationIdPConfigured:    "federation.idp.configure",
		ActionFederationIdPRevoked:       "federation.idp.revoke",
		ActionFederationPolicyChanged:    "federation.policy.change",
		ActionDownstreamSPRegistered:     "federation.sp.register",
		ActionDownstreamClientRegistered: "federation.client.register",
	}
	if len(want) != 8 {
		t.Fatalf("test table out of sync: expected 8 entries, got %d", len(want))
	}
	for a, expected := range want {
		if got := a.String(); got != expected {
			t.Errorf("Action %q = %q, want %q", expected, got, expected)
		}
	}
}

func TestRejectReasons_Uniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	for _, r := range RejectReasons {
		if r == "" {
			t.Errorf("RejectReasons contains empty string")
			continue
		}
		if _, dup := seen[r]; dup {
			t.Errorf("RejectReasons has duplicate code %q", r)
		}
		seen[r] = struct{}{}
	}
}

func TestRejectReasons_ExactContents(t *testing.T) {
	// Lock the exact membership of RejectReasons. Adding a new code is a
	// deliberate cross-repo event (Obj 9 audit dashboards join on these codes;
	// AOID federation service emits ActionFederationReject with one of them).
	want := []string{
		"idp_not_allowlisted",
		"signature_invalid",
		"audience_mismatch",
		"nonce_mismatch",
		"expired",
		"replay_detected",
		"attribute_missing",
		"email_domain_not_allowed",
		"email_conflict_different_idp",
		"policy_denied",
		"jit_disabled",
		"xsw_detected",
		"xml_roundtrip_mismatch",
	}
	if len(RejectReasons) != len(want) {
		t.Fatalf("RejectReasons length = %d, want %d", len(RejectReasons), len(want))
	}
	for i := range want {
		if RejectReasons[i] != want[i] {
			t.Errorf("RejectReasons[%d] = %q, want %q", i, RejectReasons[i], want[i])
		}
	}
}
