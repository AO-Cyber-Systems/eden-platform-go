// contract_version_test.go -- TRD 140-12 (RED).
//
// The cross-repo VERSIONING GATE. These tests pin the single source-of-truth
// experience.v1 contract version + the semver BUMP POLICY that maps a proto
// change to a version increment, AND the per-consumer (Go-shaped) FORWARD-COMPAT
// decode proof. The buf breaking-change CI gate (experience-proto-breaking.yml)
// is the wire enforcement; this file is the in-schema / in-process enforcement:
//
//   - ContractVersion is a single exported constant the proto/spec carries.
//   - ClassifyChange(old, new) classifies an additive proto delta as MINOR and a
//     frozen-field removal/retype as BREAKING (the bump-policy classifier the
//     doc + the buf gate operationalize).
//   - BumpVersion applies the classification to a semver string (MINOR bumps the
//     minor, BREAKING is a BLOCKED bump -- a v1 frozen contract never majors
//     silently; the gate must reject it).
//   - A Go consumer (eden-biz-shaped) decodes a spec carrying an UNKNOWN future
//     surface and IGNORES it (forward-compat) -- never crashes, never widens
//     scope.
//   - A drift guard: the wire fixtures carry the declared ContractVersion, so a
//     proto change that forgets to acknowledge the version is caught here just as
//     the buf gen-drift gate catches an un-regenerated proto.
//   - Wrong-tenant: the versioning layer carries no tenant authority; a
//     cross-tenant spec still decodes to a tenant-scoped (non-leaking) result --
//     the compat layer never widens scope.
package experience_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// TestContractVersion_IsSingleSourceOfTruth pins the one canonical contract
// version. Every spec/AppDefinition fixture stamps THIS value; the buf breaking
// gate guards the proto SHAPE the version names.
func TestContractVersion_IsSingleSourceOfTruth(t *testing.T) {
	if experience.ContractVersion == "" {
		t.Fatal("ContractVersion must be a non-empty single-source-of-truth constant")
	}
	// The fixture default (and the proto's documented v1 line) is "experience.v1".
	if got, want := experience.ContractVersion, "experience.v1"; got != want {
		t.Errorf("ContractVersion: got %q want %q", got, want)
	}
}

// TestClassifyChange_AdditiveIsMinor proves an ADDITIVE proto delta (a new field
// / message / rpc -- modeled here as the new value being a strict superset of the
// old field set) classifies as MINOR. This is the additive=minor half of the
// bump policy the buf gate enforces on the wire.
func TestClassifyChange_AdditiveIsMinor(t *testing.T) {
	oldFields := []experience.FieldDescriptor{
		{Number: 1, Name: "spec_schema_version", Kind: "string"},
		{Number: 4, Name: "contract_version", Kind: "string"},
	}
	// Additive: keeps every old field unchanged, adds a new one.
	newFields := []experience.FieldDescriptor{
		{Number: 1, Name: "spec_schema_version", Kind: "string"},
		{Number: 4, Name: "contract_version", Kind: "string"},
		{Number: 99, Name: "future_field", Kind: "string"},
	}
	if got := experience.ClassifyChange(oldFields, newFields); got != experience.ChangeMinor {
		t.Errorf("additive delta: got %v want ChangeMinor", got)
	}
}

// TestClassifyChange_FieldRemovalIsBreaking proves removing a frozen field is
// BREAKING -- the exact class the buf gate fails the build on.
func TestClassifyChange_FieldRemovalIsBreaking(t *testing.T) {
	oldFields := []experience.FieldDescriptor{
		{Number: 1, Name: "spec_schema_version", Kind: "string"},
		{Number: 4, Name: "contract_version", Kind: "string"},
	}
	// Breaking: field 4 removed.
	newFields := []experience.FieldDescriptor{
		{Number: 1, Name: "spec_schema_version", Kind: "string"},
	}
	if got := experience.ClassifyChange(oldFields, newFields); got != experience.ChangeBreaking {
		t.Errorf("field removal: got %v want ChangeBreaking", got)
	}
}

// TestClassifyChange_FieldRetypeIsBreaking proves changing a frozen field's
// type/number (same field name, different wire kind) is BREAKING.
func TestClassifyChange_FieldRetypeIsBreaking(t *testing.T) {
	oldFields := []experience.FieldDescriptor{
		{Number: 4, Name: "contract_version", Kind: "string"},
	}
	// Breaking: same number+name, type changed string->int32.
	newFields := []experience.FieldDescriptor{
		{Number: 4, Name: "contract_version", Kind: "int32"},
	}
	if got := experience.ClassifyChange(oldFields, newFields); got != experience.ChangeBreaking {
		t.Errorf("field retype: got %v want ChangeBreaking", got)
	}
}

// TestClassifyChange_RenumberIsBreaking proves moving a field to a new number
// (name preserved) is BREAKING -- mirrors the buf gate result on a renumber.
func TestClassifyChange_RenumberIsBreaking(t *testing.T) {
	oldFields := []experience.FieldDescriptor{
		{Number: 5, Name: "contract_version", Kind: "string"},
	}
	newFields := []experience.FieldDescriptor{
		{Number: 7, Name: "contract_version", Kind: "string"},
	}
	if got := experience.ClassifyChange(oldFields, newFields); got != experience.ChangeBreaking {
		t.Errorf("renumber: got %v want ChangeBreaking", got)
	}
}

// TestClassifyChange_NoChangeIsNone proves an identical descriptor set is a
// no-op (no version bump required).
func TestClassifyChange_NoChangeIsNone(t *testing.T) {
	fields := []experience.FieldDescriptor{
		{Number: 1, Name: "spec_schema_version", Kind: "string"},
		{Number: 4, Name: "contract_version", Kind: "string"},
	}
	if got := experience.ClassifyChange(fields, fields); got != experience.ChangeNone {
		t.Errorf("no change: got %v want ChangeNone", got)
	}
}

// TestBumpVersion_MinorBumpsMinor proves the additive=minor policy applies a
// minor increment to the semver of the contract.
func TestBumpVersion_MinorBumpsMinor(t *testing.T) {
	got, err := experience.BumpVersion("1.2.3", experience.ChangeMinor)
	if err != nil {
		t.Fatalf("BumpVersion minor: unexpected error %v", err)
	}
	if want := "1.3.0"; got != want {
		t.Errorf("minor bump: got %q want %q", got, want)
	}
}

// TestBumpVersion_BreakingIsBlocked proves a BREAKING change does NOT silently
// produce a major bump -- a frozen v1 contract blocks the bump (the policy the
// buf gate hard-fails on). The caller gets an error, not a "2.0.0" string.
func TestBumpVersion_BreakingIsBlocked(t *testing.T) {
	_, err := experience.BumpVersion("1.2.3", experience.ChangeBreaking)
	if err == nil {
		t.Fatal("BumpVersion(BREAKING) must return an error (frozen v1: no silent major), got nil")
	}
}

// TestBumpVersion_NoneIsIdentity proves a no-op change leaves the version
// unchanged.
func TestBumpVersion_NoneIsIdentity(t *testing.T) {
	got, err := experience.BumpVersion("1.2.3", experience.ChangeNone)
	if err != nil {
		t.Fatalf("BumpVersion none: unexpected error %v", err)
	}
	if want := "1.2.3"; got != want {
		t.Errorf("none bump: got %q want %q", got, want)
	}
}

// TestGoConsumer_ForwardCompat_IgnoresUnknownSurface is the per-consumer compat
// proof for a Go (eden-biz-shaped) consumer: it decodes a spec that references
// an UNKNOWN future surface and, under IGNORE policy, drops it and renders the
// rest -- never crashes, never blocks on a known-good remainder.
func TestGoConsumer_ForwardCompat_IgnoresUnknownSurface(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("surface.known", "surface.future.unknown"),
		fixtures.WithUnknownSurfacePolicy(
			experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_IGNORE,
		),
	)

	// Round-trip through the wire (a real consumer reads bytes off the network).
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := &experiencev1.ExperienceSpec{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("a forward-compat consumer must DECODE the spec, got error: %v", err)
	}

	// The consumer compiles in a manifest that knows only "surface.known".
	manifest := fixtures.NewManifest(experience.ContractVersion, "surface.known")
	res := experience.Negotiate(decoded, manifest, "9.9.9")

	if res.Blocked {
		t.Fatalf("forward-compat IGNORE must NOT block, got blocked: %s", res.Reason)
	}
	if len(res.RenderedSurfaces) != 1 || res.RenderedSurfaces[0] != "surface.known" {
		t.Errorf("expected only the known surface rendered, got %v", res.RenderedSurfaces)
	}
	if len(res.DroppedSurfaces) != 1 || res.DroppedSurfaces[0] != "surface.future.unknown" {
		t.Errorf("expected the unknown future surface dropped, got %v", res.DroppedSurfaces)
	}
}

// TestGoConsumer_ContractVersionMatchesDeclared is the drift guard: a consumer
// decoding a fixture spec sees the declared ContractVersion, so a proto change
// that forgets to acknowledge the version is caught (mirrors the buf gen-drift
// gate). The fixture default is the single source of truth.
func TestGoConsumer_ContractVersionMatchesDeclared(t *testing.T) {
	spec := fixtures.NewSpec()
	wire, _ := proto.Marshal(spec)
	decoded := &experiencev1.ExperienceSpec{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := decoded.GetContractVersion(), experience.ContractVersion; got != want {
		t.Errorf("decoded spec contract_version %q does not match declared ContractVersion %q -- "+
			"proto/fixtures drifted from the version constant", got, want)
	}
}

// TestGoConsumer_ForwardCompat_WrongTenantNonLeak proves the compat layer never
// widens scope: a wrong-tenant spec still decodes to a result scoped to the
// REQUESTING (wrong) tenant only -- the negotiation carries scope through
// untouched, it never substitutes the baseline tenant. The real isolation lives
// in 140-08/09/11; this is the lightweight non-leak assertion at the compat seam.
func TestGoConsumer_ForwardCompat_WrongTenantNonLeak(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithReferencedSurfaces("surface.known"),
		fixtures.WithUnknownSurfacePolicy(
			experiencev1.UnknownSurfacePolicy_UNKNOWN_SURFACE_POLICY_IGNORE,
		),
	)
	wrong := fixtures.WrongTenant(baseline)

	wire, _ := proto.Marshal(wrong)
	decoded := &experiencev1.ExperienceSpec{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	manifest := fixtures.NewManifest(experience.ContractVersion, "surface.known")
	res := experience.Negotiate(decoded, manifest, "9.9.9")

	// The result must carry the WRONG (requesting) scope, never the baseline's --
	// the compat layer does not widen or substitute scope.
	if res.TenantID != wrong.GetTenantId() {
		t.Errorf("negotiation tenant scope leaked: got %q want the requesting wrong-tenant %q",
			res.TenantID, wrong.GetTenantId())
	}
	if res.TenantID == baseline.GetTenantId() {
		t.Errorf("negotiation substituted the baseline tenant %q -- scope must never widen",
			baseline.GetTenantId())
	}
	if res.OrgID != wrong.GetOrgId() {
		t.Errorf("negotiation org scope leaked: got %q want %q", res.OrgID, wrong.GetOrgId())
	}
}
