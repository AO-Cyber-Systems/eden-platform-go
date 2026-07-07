package fixtures

import (
	"io"
	"net/http"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"google.golang.org/protobuf/proto"
)

// --- Happy: NewSpec defaults ---

// NewSpec() returns a valid, marshalable ExperienceSpec with sane defaults.
func TestNewSpec_ValidMarshalableDefaults(t *testing.T) {
	spec := NewSpec()
	if spec == nil {
		t.Fatal("NewSpec() returned nil")
	}

	// All three version axes must be populated with sane defaults so downstream
	// version/coherence tests have a non-empty baseline to mutate.
	if spec.GetSpecSchemaVersion() == "" {
		t.Error("SpecSchemaVersion empty — default not set")
	}
	if spec.GetSurfaceContractVersion() == "" {
		t.Error("SurfaceContractVersion empty — default not set")
	}
	if spec.GetContractVersion() == "" {
		t.Error("ContractVersion empty — default not set")
	}
	if spec.GetMinBinaryVersion() == "" {
		t.Error("MinBinaryVersion empty — default not set")
	}

	// A default fixture must carry a tenant + org scope so isolation tests can
	// diff against WrongTenant without first having to populate scope.
	if spec.GetTenantId() == "" {
		t.Error("TenantId empty — default scope not set")
	}
	if spec.GetOrgId() == "" {
		t.Error("OrgId empty — default scope not set")
	}

	// Must round-trip through proto marshal/unmarshal.
	b, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	var got experiencev1.ExperienceSpec
	if err := proto.Unmarshal(b, &got); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}
	if !proto.Equal(spec, &got) {
		t.Error("round-trip mismatch")
	}
}

// --- Happy: overrides touch exactly those fields ---

// NewSpec(WithTenant, WithOrg) overrides exactly those fields, leaves the rest
// at their defaults.
func TestNewSpec_OverridesTouchOnlyTargetedFields(t *testing.T) {
	base := NewSpec()
	got := NewSpec(WithTenant("t1"), WithOrg("o1"))

	if got.GetTenantId() != "t1" {
		t.Errorf("TenantId = %q want t1", got.GetTenantId())
	}
	if got.GetOrgId() != "o1" {
		t.Errorf("OrgId = %q want o1", got.GetOrgId())
	}

	// Every non-scope field must be untouched relative to the default.
	if got.GetSpecSchemaVersion() != base.GetSpecSchemaVersion() {
		t.Error("SpecSchemaVersion changed by scope override")
	}
	if got.GetSurfaceContractVersion() != base.GetSurfaceContractVersion() {
		t.Error("SurfaceContractVersion changed by scope override")
	}
	if got.GetContentHash() != base.GetContentHash() {
		t.Error("ContentHash changed by scope override")
	}
	if got.GetContractVersion() != base.GetContractVersion() {
		t.Error("ContractVersion changed by scope override")
	}
	if got.GetMinBinaryVersion() != base.GetMinBinaryVersion() {
		t.Error("MinBinaryVersion changed by scope override")
	}
}

// --- Edge: independent (non-aliased) structs ---

// Calling the same builder twice yields independent structs — mutating one must
// not bleed into the other (guards against shared-pointer / shared-slice bugs).
func TestNewSpec_TwoCallsAreIndependent(t *testing.T) {
	a := NewSpec()
	b := NewSpec()

	if a == b {
		t.Fatal("two NewSpec() calls returned the same pointer")
	}

	a.TenantId = "mutated"
	if b.GetTenantId() == "mutated" {
		t.Error("mutating one fixture bled into the other — structs are aliased")
	}
}

// --- Failure / wrong-tenant: the load-bearing one-call divergence helper ---

// WrongTenant(baseline) returns a spec whose tenant_id AND org_id differ from
// the baseline — the single helper every downstream isolation test uses. Assert
// it actually differs (guard against the helper silently echoing the baseline).
func TestWrongTenant_ProvenDivergent(t *testing.T) {
	base := NewSpec(WithTenant("tenant-A"), WithOrg("org-A"))
	wrong := WrongTenant(base)

	if wrong == nil {
		t.Fatal("WrongTenant returned nil")
	}
	if wrong == base {
		t.Fatal("WrongTenant returned the SAME pointer as baseline")
	}
	if wrong.GetTenantId() == base.GetTenantId() {
		t.Errorf("WrongTenant tenant_id %q did NOT diverge from baseline", wrong.GetTenantId())
	}
	if wrong.GetOrgId() == base.GetOrgId() {
		t.Errorf("WrongTenant org_id %q did NOT diverge from baseline", wrong.GetOrgId())
	}

	// Non-scope content must be preserved so the only axis under test is tenancy.
	if wrong.GetSpecSchemaVersion() != base.GetSpecSchemaVersion() {
		t.Error("WrongTenant altered a non-scope field (spec_schema_version)")
	}

	// Mutating the baseline after the fact must not affect the wrong-tenant copy.
	base.TenantId = "tenant-A-mutated"
	if wrong.GetTenantId() == "tenant-A-mutated" {
		t.Error("WrongTenant copy aliases the baseline pointer")
	}
}

// --- Edge: cassette replays deterministically, no live calls ---

// A cassette loads from disk and replays a recorded response deterministically
// via an httptest server — no live external call.
func TestCassetteReplay_Deterministic(t *testing.T) {
	srv := NewCassetteServer(t, "provider_ping")
	defer srv.Close()

	get := func() (int, string) {
		resp, err := http.Get(srv.URL + "/ping")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		return resp.StatusCode, string(body)
	}

	code1, body1 := get()
	code2, body2 := get()

	if code1 != http.StatusOK {
		t.Errorf("status = %d want 200", code1)
	}
	if code1 != code2 || body1 != body2 {
		t.Error("cassette replay was non-deterministic across two calls")
	}
	if body1 == "" {
		t.Error("cassette replay returned empty body")
	}
}
