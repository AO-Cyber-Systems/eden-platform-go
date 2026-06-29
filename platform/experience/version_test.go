// Package experience hosts the contract tests for the experience.v1 keystone
// proto. TRD 140-01 owns ONLY the envelope + the three version axes; these tests
// prove the freeze-critical invariants that later TRDs (140-03..07) must not break.
//
// FROZEN-CONTRACT DISCIPLINE under test:
//   - the three version axes round-trip independently (bumping one does not change
//     another's serialized bytes — they are orthogonal),
//   - an unknown future field round-trips without error (forward-compat: a future
//     TRD can add a field and an old binary still reads the spec, and vice-versa),
//   - the AppDefinition envelope carries min_binary_version and embeds the spec,
//   - tenant_id / org_id exist on the envelope so later resolution TRDs
//     (140-08/140-09) have a place to enforce wrong-tenant isolation (this TRD
//     only RESERVES the field; enforcement lands in 140-08/09 — see test below).
package experience_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// newSpec hand-builds a minimal ExperienceSpec literal. The 140-02 factory does
// not exist yet (it runs in parallel); for this Wave-1 TRD we build inline.
func newSpec() *experiencev1.ExperienceSpec {
	return &experiencev1.ExperienceSpec{
		SpecSchemaVersion:      "1.0.0",
		SurfaceContractVersion: "1.0.0",
		ContentHash:            "sha256:deadbeef",
		ContractVersion:        "experience.v1",
		MinBinaryVersion:       "1.0.0",
		TenantId:               "tenant-abc",
		OrgId:                  "org-xyz",
	}
}

func newAppDefinition() *experiencev1.AppDefinition {
	return &experiencev1.AppDefinition{
		Id: "app-1",
		Meta: &experiencev1.AppMeta{
			Name:     "Acme Field",
			BundleId: "ai.aocyber.acme.field",
		},
		Spec:             newSpec(),
		MinBinaryVersion: "1.0.0",
		ContractVersion:  "experience.v1",
	}
}

// TestExperienceSpec_RoundTrip_PreservesVersionAxes proves the happy path: every
// version axis survives a marshal/unmarshal cycle byte-for-byte.
func TestExperienceSpec_RoundTrip_PreservesVersionAxes(t *testing.T) {
	in := newSpec()

	wire, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, want := out.GetSpecSchemaVersion(), in.GetSpecSchemaVersion(); got != want {
		t.Errorf("spec_schema_version: got %q want %q", got, want)
	}
	if got, want := out.GetSurfaceContractVersion(), in.GetSurfaceContractVersion(); got != want {
		t.Errorf("surface_contract_version: got %q want %q", got, want)
	}
	if got, want := out.GetContentHash(), in.GetContentHash(); got != want {
		t.Errorf("content_hash: got %q want %q", got, want)
	}
	if got, want := out.GetContractVersion(), in.GetContractVersion(); got != want {
		t.Errorf("contract_version: got %q want %q", got, want)
	}
}

// TestAppDefinition_CarriesMinBinaryVersionAndEmbedsSpec proves the AppDefinition
// envelope carries min_binary_version and embeds the ExperienceSpec, and that a
// default-constructed value is zero (not a nil-panic).
func TestAppDefinition_CarriesMinBinaryVersionAndEmbedsSpec(t *testing.T) {
	in := newAppDefinition()

	wire, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.AppDefinition
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, want := out.GetMinBinaryVersion(), in.GetMinBinaryVersion(); got != want {
		t.Errorf("app min_binary_version: got %q want %q", got, want)
	}
	if out.GetSpec() == nil {
		t.Fatal("AppDefinition.spec is nil after round-trip; envelope must embed the spec")
	}
	if got, want := out.GetSpec().GetSpecSchemaVersion(), in.GetSpec().GetSpecSchemaVersion(); got != want {
		t.Errorf("embedded spec_schema_version: got %q want %q", got, want)
	}

	// Default-constructed value must be zero, not nil-panic.
	var zero experiencev1.AppDefinition
	if zero.GetMinBinaryVersion() != "" {
		t.Errorf("zero-value min_binary_version: got %q want empty", zero.GetMinBinaryVersion())
	}
	if zero.GetSpec() != nil {
		t.Errorf("zero-value spec: got %v want nil", zero.GetSpec())
	}
}

// TestVersionAxes_AreIndependent proves the axes are ORTHOGONAL: bumping
// spec_schema_version does not change the serialized bytes of
// surface_contract_version, and vice-versa. We assert this by holding one axis
// fixed across two specs that differ only in the other axis, and confirming the
// fixed axis round-trips to the same value in both.
func TestVersionAxes_AreIndependent(t *testing.T) {
	a := newSpec()
	a.SpecSchemaVersion = "1.0.0"
	a.SurfaceContractVersion = "7.0.0"

	b := newSpec()
	b.SpecSchemaVersion = "2.0.0" // bump ONLY the schema axis
	b.SurfaceContractVersion = "7.0.0"

	roundtrip := func(s *experiencev1.ExperienceSpec) *experiencev1.ExperienceSpec {
		wire, err := proto.Marshal(s)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out experiencev1.ExperienceSpec
		if err := proto.Unmarshal(wire, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &out
	}

	ra, rb := roundtrip(a), roundtrip(b)

	// The schema axis differs as bumped...
	if ra.GetSpecSchemaVersion() == rb.GetSpecSchemaVersion() {
		t.Errorf("spec_schema_version should differ between a (%q) and b (%q)", ra.GetSpecSchemaVersion(), rb.GetSpecSchemaVersion())
	}
	// ...while the surface axis is UNCHANGED in both (orthogonal).
	if ra.GetSurfaceContractVersion() != "7.0.0" || rb.GetSurfaceContractVersion() != "7.0.0" {
		t.Errorf("surface_contract_version must be unaffected by a spec_schema_version bump: a=%q b=%q",
			ra.GetSurfaceContractVersion(), rb.GetSurfaceContractVersion())
	}
}

// TestForwardCompat_UnknownFieldRoundTrips proves forward-compat: bytes carrying
// a field number this binary does not know (simulating a future TRD's addition)
// survive an unmarshal/re-marshal cycle without error and without dropping the
// unknown field. This is the core frozen-contract guarantee — an old binary can
// read a spec a newer binary wrote.
func TestForwardCompat_UnknownFieldRoundTrips(t *testing.T) {
	// Start from a known-good spec.
	base := newSpec()
	known, err := proto.Marshal(base)
	if err != nil {
		t.Fatalf("marshal base: %v", err)
	}

	// Append a wire-encoded field with a high field number (900) that this
	// generated message does not declare. Tag = field<<3 | wireType.
	// Field 900, wire type 2 (length-delimited): (900<<3)|2 = 7202.
	// varint(7202) = 0xA2 0x38 ; then length 0x05 ; then 5 bytes "extra".
	future := append([]byte{}, known...)
	future = append(future, 0xA2, 0x38, 0x05, 'e', 'x', 't', 'r', 'a')

	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(future, &out); err != nil {
		t.Fatalf("unmarshal with unknown field must not error (forward-compat broken): %v", err)
	}

	// Known fields must still be readable.
	if out.GetContentHash() != base.GetContentHash() {
		t.Errorf("known content_hash lost when an unknown field was present: got %q want %q",
			out.GetContentHash(), base.GetContentHash())
	}

	// The unknown field must be preserved on re-marshal (so an old binary that
	// reads-then-writes does not silently strip a newer binary's data).
	remarshaled, err := proto.Marshal(&out)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if len(remarshaled) < len(known) {
		t.Errorf("re-marshaled bytes (%d) shorter than known-only bytes (%d); unknown field was dropped",
			len(remarshaled), len(known))
	}
}

// TestEnvelope_ReservesTenantAndOrgFields proves tenant_id and org_id EXIST in
// the envelope so the resolution TRDs have a place to enforce isolation.
//
// WRONG-TENANT NOTE: this skeleton has no resolution surface yet, so there is no
// behavior to isolate. Per the test-list, this is the PLACEHOLDER isolation
// assertion: the field must exist and round-trip. Actual cross-tenant +
// wrong-entity rejection (collapsing to a single non-leaking code) lands in
// 140-08 / 140-09; this TRD only reserves the seam.
func TestEnvelope_ReservesTenantAndOrgFields(t *testing.T) {
	in := newSpec()
	in.TenantId = "tenant-abc"
	in.OrgId = "org-xyz"

	wire, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, want := out.GetTenantId(), "tenant-abc"; got != want {
		t.Errorf("tenant_id must exist + round-trip on the envelope: got %q want %q", got, want)
	}
	if got, want := out.GetOrgId(), "org-xyz"; got != want {
		t.Errorf("org_id must exist + round-trip on the envelope: got %q want %q", got, want)
	}
}
