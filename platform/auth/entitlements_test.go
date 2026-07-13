package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// Test-list case 1 (Pitfall 7 fix): a token body carrying `"ent":[...]`
// unmarshals into Claims.Entitlements by JSON tag — the SAME capture-by-tag
// mechanism the household backends already rely on for hid/role/child_mode.
func TestClaims_EntitlementsUnmarshalByTag(t *testing.T) {
	body := `{"uid":"u","ent":["aofamily:plan:premium","ai:unlimited_chat"]}`
	var c auth.Claims
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"aofamily:plan:premium", "ai:unlimited_chat"}
	if len(c.Entitlements) != len(want) {
		t.Fatalf("Entitlements = %v, want %v", c.Entitlements, want)
	}
	for i, w := range want {
		if c.Entitlements[i] != w {
			t.Errorf("Entitlements[%d] = %q, want %q", i, c.Entitlements[i], w)
		}
	}
}

// Test-list case 2 (omitempty byte-compat): a Claims with no `ent` marshals
// WITHOUT an "ent" key, so B2B / tnt-only tokens stay wire-identical.
func TestClaims_EntitlementsOmitEmpty(t *testing.T) {
	c := auth.Claims{UserID: "u"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"ent"`) {
		t.Errorf("marshaled Claims carries an ent key when Entitlements is empty: %s", b)
	}
}

// Test-list case 3: HasEntitlement is pure membership over Entitlements.
func TestClaims_HasEntitlement(t *testing.T) {
	c := auth.Claims{Entitlements: []string{"aofamily:plan:premium"}}
	if !c.HasEntitlement("aofamily:plan:premium") {
		t.Error("HasEntitlement(present) = false, want true")
	}
	if c.HasEntitlement("ai:unlimited_chat") {
		t.Error("HasEntitlement(absent) = true, want false")
	}
}

// Test-list case 4: empty Claims → HasEntitlement false (empty = deny,
// least-privilege — must NOT fail open).
func TestClaims_HasEntitlement_EmptyDeny(t *testing.T) {
	var c auth.Claims
	if c.HasEntitlement("anything") {
		t.Error("empty Claims HasEntitlement = true, want false (deny)")
	}
}

// Test-list case 5: RequirePlan reads verified claims from context
// mint-agnostically (no billing I/O), returning them iff the entitlement is
// present. Distinct sentinels let callers map missing-entitlement → 402 and
// no-claims → 401/403.
func TestRequirePlan(t *testing.T) {
	ctx := auth.WithClaims(context.Background(), &auth.Claims{
		Entitlements: []string{"aofamily:plan:premium"},
	})

	// present entitlement → no error + returns the claims
	got, err := auth.RequirePlan(ctx, "aofamily:plan:premium")
	if err != nil {
		t.Fatalf("RequirePlan(present) error = %v, want nil", err)
	}
	if got == nil || !got.HasEntitlement("aofamily:plan:premium") {
		t.Fatalf("RequirePlan(present) returned claims = %v", got)
	}

	// missing entitlement → non-nil error, matchable as ErrMissingEntitlement (402)
	if _, err := auth.RequirePlan(ctx, "ai:unlimited_chat"); err == nil {
		t.Error("RequirePlan(missing) error = nil, want non-nil")
	} else if !errors.Is(err, auth.ErrMissingEntitlement) {
		t.Errorf("RequirePlan(missing) error = %v, want ErrMissingEntitlement", err)
	}

	// no claims in context → non-nil error, matchable as ErrNoClaims (401/403)
	if _, err := auth.RequirePlan(context.Background(), "aofamily:plan:premium"); err == nil {
		t.Error("RequirePlan(no claims) error = nil, want non-nil")
	} else if !errors.Is(err, auth.ErrNoClaims) {
		t.Errorf("RequirePlan(no claims) error = %v, want ErrNoClaims", err)
	}
}
