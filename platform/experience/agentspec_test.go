// agentspec_test.go -- TRD 160-01 (RED): the reusable versioned AgentSpec
// contract in experience.v1.
//
// AgentSpec is the FIRST additive message group after the 140 freeze. It
// COMPOSES the frozen AgentNode (field: node) + ToolDefinition (field: tools)
// -- it never redefines or mutates them -- and adds the authored-agent surface:
// persona / model_ref / knowledge / hitl / budget / lifecycle / version.
//
// Test list (from 160-01-TRD, written BEFORE any production code):
//  1. AgentSpec round-trips (marshal->unmarshal) preserving persona, model_ref,
//     node.tool_ids, tools, hitl, budget, lifecycle, version.
//  2. AgentSpec COMPOSES AgentNode: node.tool_ids readable back; the embedded
//     AgentNode's bytes are byte-identical to a standalone AgentNode
//     (composition, not a fork).
//  3. Forward-compat: bytes carrying an unknown future field number deserialize
//     into the current AgentSpec WITHOUT error and re-serialize preserving the
//     unknown field.
//  4. ValidateAgentSpec accepts the well-formed helpdesk fixture.
//  5. ValidateAgentSpec rejects: empty persona; empty model_ref; zero tools;
//     max_steps <=0 or over ceiling -- each fail-closed with a typed problem,
//     accumulated (never short-circuited).
//  6. WRONG-TENANT: a spec whose company scope diverges from the principal scope
//     yields the SAME single permission-denied as a non-existent (nil) spec --
//     byte-identical, no existence oracle, no scope echo.
//
// Fixtures only: fixtures.NewAgentSpec + With* opts (hand-built factory, no
// LLM-generated data).
package experience_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// rtAgentSpec marshals then unmarshals an AgentSpec, failing on any wire error.
func rtAgentSpec(t *testing.T, spec *experiencev1.AgentSpec) *experiencev1.AgentSpec {
	t.Helper()
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal agentspec: %v", err)
	}
	var got experiencev1.AgentSpec
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal agentspec: %v", err)
	}
	return &got
}

// detMarshal marshals with deterministic field ordering so two structurally
// equal messages produce byte-identical wire output (the composition proof).
func detMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatalf("deterministic marshal: %v", err)
	}
	return wire
}

// --- 1. Round-trip -----------------------------------------------------------

// AgentSpec round-trips every typed authored-agent field: persona, model_ref,
// the composed node's tool_ids, tools, hitl, budget, lifecycle, version.
func TestAgentSpec_RoundTrips(t *testing.T) {
	spec := fixtures.NewAgentSpec()
	got := rtAgentSpec(t, spec)

	if got.GetPersona() != spec.GetPersona() || got.GetPersona() == "" {
		t.Fatalf("persona: got %q want %q (non-empty)", got.GetPersona(), spec.GetPersona())
	}
	if got.GetModelRef() != spec.GetModelRef() || got.GetModelRef() == "" {
		t.Fatalf("model_ref: got %q want %q (non-empty)", got.GetModelRef(), spec.GetModelRef())
	}
	if got.GetVersion() != spec.GetVersion() || got.GetVersion() == "" {
		t.Fatalf("version: got %q want %q (non-empty)", got.GetVersion(), spec.GetVersion())
	}
	if got.GetLifecycle() != spec.GetLifecycle() || got.GetLifecycle() == "" {
		t.Fatalf("lifecycle: got %q want %q (non-empty)", got.GetLifecycle(), spec.GetLifecycle())
	}
	if !reflect.DeepEqual(got.GetNode().GetToolIds(), spec.GetNode().GetToolIds()) {
		t.Fatalf("node.tool_ids: got %v want %v", got.GetNode().GetToolIds(), spec.GetNode().GetToolIds())
	}
	if len(got.GetTools()) != len(spec.GetTools()) || len(got.GetTools()) == 0 {
		t.Fatalf("tools: got %d want %d (>0)", len(got.GetTools()), len(spec.GetTools()))
	}
	if got.GetHitl().GetEscalateOnLowConfidence() != spec.GetHitl().GetEscalateOnLowConfidence() {
		t.Fatalf("hitl.escalate_on_low_confidence lost on the wire")
	}
	if !reflect.DeepEqual(got.GetHitl().GetEscalateOnWriteBeyond(), spec.GetHitl().GetEscalateOnWriteBeyond()) {
		t.Fatalf("hitl.escalate_on_write_beyond: got %v want %v",
			got.GetHitl().GetEscalateOnWriteBeyond(), spec.GetHitl().GetEscalateOnWriteBeyond())
	}
	if got.GetBudget().GetMaxSteps() != spec.GetBudget().GetMaxSteps() || got.GetBudget().GetMaxSteps() == 0 {
		t.Fatalf("budget.max_steps: got %d want %d (>0)", got.GetBudget().GetMaxSteps(), spec.GetBudget().GetMaxSteps())
	}
	if got.GetKnowledge().GetGroundedOnly() != spec.GetKnowledge().GetGroundedOnly() {
		t.Fatalf("knowledge.grounded_only lost on the wire")
	}
	if got.GetCompanyId() != spec.GetCompanyId() || got.GetCompanyId() == "" {
		t.Fatalf("company_id: got %q want %q (non-empty)", got.GetCompanyId(), spec.GetCompanyId())
	}
}

// --- 2. Composition (not a fork) ----------------------------------------------

// AgentSpec.node IS the frozen 140 AgentNode: the embedded node's wire bytes are
// byte-identical to a standalone AgentNode built with the same values. If the
// contract had forked/redefined the node type, the bytes (or the Go type) would
// diverge.
func TestAgentSpec_ComposesFrozenAgentNode(t *testing.T) {
	spec := fixtures.NewAgentSpec()

	if !reflect.DeepEqual(spec.GetNode().GetToolIds(), fixtures.HelpdeskToolIDs()) {
		t.Fatalf("node.tool_ids not readable back: got %v want %v",
			spec.GetNode().GetToolIds(), fixtures.HelpdeskToolIDs())
	}

	// The composed node is the SAME Go type as the frozen standalone AgentNode.
	var node *experiencev1.AgentNode = spec.GetNode()

	standalone := fixtures.NewAgentNode(node.GetIoEnvelopeSchema(), fixtures.HelpdeskToolIDs()...)
	if !bytes.Equal(detMarshal(t, node), detMarshal(t, standalone)) {
		t.Fatalf("embedded AgentNode bytes diverge from a standalone AgentNode -- composition broken (fork?)")
	}
}

// --- 3. Forward-compat ---------------------------------------------------------

// A version-N+1 producer may add NEW fields to AgentSpec. A version-N consumer
// must (a) deserialize those bytes without error and (b) preserve the unknown
// field across a re-serialize (proto3 unknown-field retention) -- so an old
// runtime never silently strips a newer authored spec.
func TestAgentSpec_ForwardCompat_PreservesUnknownFields(t *testing.T) {
	wire, err := proto.Marshal(fixtures.NewAgentSpec())
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	// Simulate a future field: number 4001 (far above every allocated/reserved
	// AgentSpec field), length-delimited payload.
	future := protowire.AppendTag(nil, 4001, protowire.BytesType)
	future = protowire.AppendString(future, "field-from-version-n-plus-1")
	wire = append(wire, future...)

	var got experiencev1.AgentSpec
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("version-N consumer must accept version-N+1 bytes, got error: %v", err)
	}
	if len(got.ProtoReflect().GetUnknown()) == 0 {
		t.Fatalf("unknown future field was dropped at unmarshal (no unknown-field retention)")
	}

	rewire, err := proto.Marshal(&got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Contains(rewire, future) {
		t.Fatalf("unknown future field not preserved across re-serialize")
	}
}

// --- 4. Validator accepts the helpdesk fixture ---------------------------------

func TestValidateAgentSpec_AcceptsHelpdeskFixture(t *testing.T) {
	problems := experience.ValidateAgentSpec(fixtures.NewAgentSpec(), fixtures.DefaultCompanyID)
	if len(problems) != 0 {
		t.Fatalf("well-formed helpdesk fixture rejected: %v", problems)
	}
}

// --- 5. Validator rejects malformed specs (fail-closed, typed, accumulated) ----

func TestValidateAgentSpec_RejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		spec *experiencev1.AgentSpec
		want experience.AgentSpecCode
	}{
		{
			name: "empty persona",
			spec: fixtures.NewAgentSpec(fixtures.WithPersona("")),
			want: experience.AgentSpecMissingPersona,
		},
		{
			name: "empty model_ref",
			spec: fixtures.NewAgentSpec(fixtures.WithModelRef("")),
			want: experience.AgentSpecMissingModelRef,
		},
		{
			name: "zero tools",
			spec: fixtures.NewAgentSpec(fixtures.WithTools()),
			want: experience.AgentSpecNoTools,
		},
		{
			name: "max_steps zero (also the nil-budget shape)",
			spec: fixtures.NewAgentSpec(fixtures.WithMaxSteps(0)),
			want: experience.AgentSpecBudgetOutOfRange,
		},
		{
			name: "max_steps negative",
			spec: fixtures.NewAgentSpec(fixtures.WithMaxSteps(-3)),
			want: experience.AgentSpecBudgetOutOfRange,
		},
		{
			name: "max_steps over ceiling",
			spec: fixtures.NewAgentSpec(fixtures.WithMaxSteps(experience.AgentSpecMaxStepsCeiling + 1)),
			want: experience.AgentSpecBudgetOutOfRange,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := experience.ValidateAgentSpec(tc.spec, fixtures.DefaultCompanyID)
			if len(problems) == 0 {
				t.Fatalf("malformed spec accepted (want %s)", tc.want)
			}
			found := false
			for _, p := range problems {
				if p.Code == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("want a %s problem, got %v", tc.want, problems)
			}
		})
	}

	// Accumulation: two independent defects yield two problems in ONE pass
	// (the builder must see every problem at once, never short-circuited).
	both := fixtures.NewAgentSpec(fixtures.WithPersona(""), fixtures.WithModelRef(""))
	problems := experience.ValidateAgentSpec(both, fixtures.DefaultCompanyID)
	if len(problems) < 2 {
		t.Fatalf("validator short-circuited: want >=2 accumulated problems, got %v", problems)
	}
}

// --- 6. Wrong-tenant: non-leaking single denial ---------------------------------

// A spec whose declared company scope diverges from the principal scope returns
// ONE permission-denied problem that is BYTE-IDENTICAL to the missing-spec (nil)
// case -- so a caller can never distinguish "another tenant's spec exists" from
// "no such spec" (no existence oracle), and the denial never echoes either scope.
func TestValidateAgentSpec_WrongTenant_NonLeakingDenial(t *testing.T) {
	foreign := fixtures.NewAgentSpec() // company_id = DefaultCompanyID
	principal := fixtures.WrongCompanyID

	denied := experience.ValidateAgentSpec(foreign, principal)
	missing := experience.ValidateAgentSpec(nil, principal)

	if len(denied) != 1 {
		t.Fatalf("diverged scope must yield exactly ONE denial, got %v", denied)
	}
	if len(missing) != 1 {
		t.Fatalf("missing spec must yield exactly ONE denial, got %v", missing)
	}
	if denied[0].Code != experience.AgentSpecPermissionDenied {
		t.Fatalf("denial code: got %s want %s", denied[0].Code, experience.AgentSpecPermissionDenied)
	}
	if !reflect.DeepEqual(denied, missing) {
		t.Fatalf("wrong-tenant and missing-spec denials differ (existence oracle):\n diverge: %#v\n missing: %#v", denied, missing)
	}
	if denied[0].Message != missing[0].Message {
		t.Fatalf("denial messages not byte-equal:\n diverge: %q\n missing: %q", denied[0].Message, missing[0].Message)
	}
	// The denial must not echo either scope id (the echo is itself an oracle).
	if strings.Contains(denied[0].Message, fixtures.DefaultCompanyID) ||
		strings.Contains(denied[0].Message, principal) {
		t.Fatalf("denial leaks a scope id: %q", denied[0].Message)
	}

	// Fail-closed on divergence: the denial is the ONLY finding -- content checks
	// must NOT run on a foreign spec (a content finding would itself leak).
	brokenForeign := fixtures.NewAgentSpec(fixtures.WithPersona(""))
	problems := experience.ValidateAgentSpec(brokenForeign, principal)
	if !reflect.DeepEqual(problems, missing) {
		t.Fatalf("content validation ran on a foreign spec (leak): %v", problems)
	}
}

// --- 161-01: the AUDIENCE dimension ---------------------------------------------
//
// Test list (from 161-01-TRD, written BEFORE any production code):
//  A1. Back-compat: a spec with audience INTERNAL or unset/UNSPECIFIED and no
//      bindings validates exactly as a 160 spec did (helpdesk fixture unchanged).
//  A2. EXTERNAL + an EXTERNAL binding whose tool_ids all reference
//      visibility=EXTERNAL_SAFE tools -> valid.
//  A3. EXTERNAL binding referencing a visibility=INTERNAL_ONLY tool ->
//      AgentSpecExternalToolNotSafe naming the offending tool_id (deny-by-default).
//  A4. EXTERNAL binding referencing a visibility=UNSPECIFIED tool -> SAME
//      rejection (fail-closed: unspecified == internal_only).
//  A5. BOTH requires an EXTERNAL binding; its external binding obeys A3/A4; the
//      INTERNAL binding may reference ANY spec tool.
//  A6. A binding tool_id not present in the spec's tool set ->
//      AgentSpecBindingUnknownTool (binding subset-of spec tools).
//  A7. WRONG-TENANT ordering: a foreign spec that ALSO violates A3 returns ONLY
//      the single denial -- no audience finding leaks (len==1, the denial).
//  A8. Forward-compat: audience fields (20/21, tool visibility 6) round-trip;
//      UNKNOWN audience/visibility enum values unmarshal cleanly and survive
//      re-serialize; the 160 unknown-field retention still holds on an
//      audience-bearing spec.

// externalSafeSpec builds an EXTERNAL-audience helpdesk spec whose external
// binding references only the KB-search tool, marked EXTERNAL_SAFE (tool index
// 0 = kb.article.search in the fixture's binding order).
func externalSafeSpec() *experiencev1.AgentSpec {
	return fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL),
		fixtures.WithToolVisibility(0, experiencev1.ToolVisibility_TOOL_VISIBILITY_EXTERNAL_SAFE),
		fixtures.WithAudienceBinding(fixtures.NewExternalAudienceBinding()),
	)
}

// --- A1. Back-compat: unset/INTERNAL audience validates as a 160 spec ----------

func TestValidateAgentSpec_Audience_BackCompatInternal(t *testing.T) {
	// The UNCHANGED 160 helpdesk fixture (no audience field at all).
	if problems := experience.ValidateAgentSpec(fixtures.NewAgentSpec(), fixtures.DefaultCompanyID); len(problems) != 0 {
		t.Fatalf("160-era spec (no audience) rejected: %v", problems)
	}
	// Explicit INTERNAL, still no bindings.
	internal := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_INTERNAL))
	if problems := experience.ValidateAgentSpec(internal, fixtures.DefaultCompanyID); len(problems) != 0 {
		t.Fatalf("explicit INTERNAL spec rejected: %v", problems)
	}
}

// --- A2. EXTERNAL + external-safe binding is valid ------------------------------

func TestValidateAgentSpec_Audience_ExternalSafeBindingValid(t *testing.T) {
	problems := experience.ValidateAgentSpec(externalSafeSpec(), fixtures.DefaultCompanyID)
	if len(problems) != 0 {
		t.Fatalf("external spec with external_safe-only binding rejected: %v", problems)
	}
}

// --- A3/A4. Deny-by-default: internal_only AND unspecified both reject ----------

func TestValidateAgentSpec_Audience_ExternalBindingDeniesUnsafeTools(t *testing.T) {
	cases := []struct {
		name string
		vis  experiencev1.ToolVisibility
	}{
		{"internal_only tool on external binding", experiencev1.ToolVisibility_TOOL_VISIBILITY_INTERNAL_ONLY},
		{"UNSPECIFIED tool on external binding (fail-closed)", experiencev1.ToolVisibility_TOOL_VISIBILITY_UNSPECIFIED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// kb.article.search (tool 0) carries the unsafe visibility while the
			// external binding references it.
			spec := fixtures.NewAgentSpec(
				fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL),
				fixtures.WithToolVisibility(0, tc.vis),
				fixtures.WithAudienceBinding(fixtures.NewExternalAudienceBinding()),
			)
			problems := experience.ValidateAgentSpec(spec, fixtures.DefaultCompanyID)
			found := false
			for _, p := range problems {
				if p.Code == experience.AgentSpecExternalToolNotSafe {
					found = true
					// The finding must NAME the offending tool so the builder can
					// point at it (same-tenant context -- not an oracle).
					if !strings.Contains(p.Message, fixtures.HelpdeskToolKBSearch) {
						t.Fatalf("finding does not name the offending tool_id %q: %q",
							fixtures.HelpdeskToolKBSearch, p.Message)
					}
				}
			}
			if !found {
				t.Fatalf("want %s, got %v", experience.AgentSpecExternalToolNotSafe, problems)
			}
		})
	}
}

// --- A5. BOTH requires an external binding; internal binding unconstrained ------

func TestValidateAgentSpec_Audience_BothRequiresExternalBinding(t *testing.T) {
	// BOTH with NO external binding -> AgentSpecMissingExternalBinding.
	noExternal := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_BOTH),
		fixtures.WithAudienceBinding(&experiencev1.AudienceBinding{
			Audience: experiencev1.AgentAudience_AGENT_AUDIENCE_INTERNAL,
			ToolIds:  fixtures.HelpdeskToolIDs(),
		}),
	)
	problems := experience.ValidateAgentSpec(noExternal, fixtures.DefaultCompanyID)
	found := false
	for _, p := range problems {
		if p.Code == experience.AgentSpecMissingExternalBinding {
			found = true
		}
	}
	if !found {
		t.Fatalf("BOTH without external binding: want %s, got %v",
			experience.AgentSpecMissingExternalBinding, problems)
	}

	// EXTERNAL (audience without any binding at all) -> same requirement.
	bare := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL))
	problems = experience.ValidateAgentSpec(bare, fixtures.DefaultCompanyID)
	found = false
	for _, p := range problems {
		if p.Code == experience.AgentSpecMissingExternalBinding {
			found = true
		}
	}
	if !found {
		t.Fatalf("EXTERNAL without any binding: want %s, got %v",
			experience.AgentSpecMissingExternalBinding, problems)
	}

	// Valid BOTH: internal binding references ANY tool (including internal-only
	// writes); external binding references only the EXTERNAL_SAFE kb-search.
	valid := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_BOTH),
		fixtures.WithToolVisibility(0, experiencev1.ToolVisibility_TOOL_VISIBILITY_EXTERNAL_SAFE),
		fixtures.WithAudienceBinding(&experiencev1.AudienceBinding{
			Audience: experiencev1.AgentAudience_AGENT_AUDIENCE_INTERNAL,
			ToolIds:  fixtures.HelpdeskToolIDs(), // full internal set, any visibility
		}),
		fixtures.WithAudienceBinding(fixtures.NewExternalAudienceBinding()),
	)
	if problems := experience.ValidateAgentSpec(valid, fixtures.DefaultCompanyID); len(problems) != 0 {
		t.Fatalf("valid BOTH spec rejected: %v", problems)
	}

	// The external binding of a BOTH spec obeys A3: unsafe tool -> rejected.
	unsafeBoth := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_BOTH),
		fixtures.WithToolVisibility(0, experiencev1.ToolVisibility_TOOL_VISIBILITY_INTERNAL_ONLY),
		fixtures.WithAudienceBinding(fixtures.NewExternalAudienceBinding()),
	)
	problems = experience.ValidateAgentSpec(unsafeBoth, fixtures.DefaultCompanyID)
	found = false
	for _, p := range problems {
		if p.Code == experience.AgentSpecExternalToolNotSafe {
			found = true
		}
	}
	if !found {
		t.Fatalf("BOTH external binding with unsafe tool: want %s, got %v",
			experience.AgentSpecExternalToolNotSafe, problems)
	}
}

// --- A6. Binding tool_ids must be a subset of the spec's tools ------------------

func TestValidateAgentSpec_Audience_BindingUnknownToolRejected(t *testing.T) {
	spec := fixtures.NewAgentSpec(
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_INTERNAL),
		fixtures.WithAudienceBinding(&experiencev1.AudienceBinding{
			Audience: experiencev1.AgentAudience_AGENT_AUDIENCE_INTERNAL,
			ToolIds:  []string{"tool.not.in.spec"},
		}),
	)
	problems := experience.ValidateAgentSpec(spec, fixtures.DefaultCompanyID)
	found := false
	for _, p := range problems {
		if p.Code == experience.AgentSpecBindingUnknownTool {
			found = true
			if !strings.Contains(p.Message, "tool.not.in.spec") {
				t.Fatalf("finding does not name the unknown tool_id: %q", p.Message)
			}
		}
	}
	if !found {
		t.Fatalf("want %s, got %v", experience.AgentSpecBindingUnknownTool, problems)
	}
}

// --- A7. Wrong-tenant ordering UNCHANGED: tenancy first and ALONE ----------------

// A foreign-scope spec that ALSO violates the external-tool-safety rule returns
// ONLY the single non-leaking denial -- an audience finding on a foreign spec
// would itself be an oracle.
func TestValidateAgentSpec_Audience_WrongTenantSingleDenial(t *testing.T) {
	violating := fixtures.NewAgentSpec( // company_id = DefaultCompanyID
		fixtures.WithAudience(experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL),
		fixtures.WithToolVisibility(0, experiencev1.ToolVisibility_TOOL_VISIBILITY_INTERNAL_ONLY),
		fixtures.WithAudienceBinding(fixtures.NewExternalAudienceBinding()),
	)
	problems := experience.ValidateAgentSpec(violating, fixtures.WrongCompanyID)
	if len(problems) != 1 {
		t.Fatalf("foreign spec must yield exactly ONE finding, got %v", problems)
	}
	if problems[0].Code != experience.AgentSpecPermissionDenied {
		t.Fatalf("foreign spec finding: got %s want %s", problems[0].Code, experience.AgentSpecPermissionDenied)
	}
	// Byte-identical to the missing-spec denial (no existence oracle).
	missing := experience.ValidateAgentSpec(nil, fixtures.WrongCompanyID)
	if !reflect.DeepEqual(problems, missing) {
		t.Fatalf("audience finding leaked on a foreign spec:\n got: %#v\n want: %#v", problems, missing)
	}
}

// --- A8. Forward-compat: audience fields + unknown enum values ------------------

func TestAgentSpec_Audience_ForwardCompat(t *testing.T) {
	spec := externalSafeSpec()

	// (a) Audience fields round-trip: audience(20), audience_bindings(21),
	// tools[].visibility(6).
	got := rtAgentSpec(t, spec)
	if got.GetAudience() != experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL {
		t.Fatalf("audience lost on the wire: got %v", got.GetAudience())
	}
	if len(got.GetAudienceBindings()) != 1 {
		t.Fatalf("audience_bindings lost on the wire: got %v", got.GetAudienceBindings())
	}
	b := got.GetAudienceBindings()[0]
	if b.GetAudience() != experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL ||
		len(b.GetToolIds()) == 0 || b.GetPersona() == "" || b.GetEscalationTarget() == "" {
		t.Fatalf("AudienceBinding fields lost on the wire: %+v", b)
	}
	if got.GetTools()[0].GetVisibility() != experiencev1.ToolVisibility_TOOL_VISIBILITY_EXTERNAL_SAFE {
		t.Fatalf("tools[0].visibility lost on the wire: got %v", got.GetTools()[0].GetVisibility())
	}

	// (b) UNKNOWN enum values (a version-N+1 audience/visibility) unmarshal
	// cleanly (proto3 open enums) and survive re-serialize.
	unknown := externalSafeSpec()
	unknown.Audience = experiencev1.AgentAudience(99)
	unknown.Tools[0].Visibility = experiencev1.ToolVisibility(99)
	rt := rtAgentSpec(t, unknown)
	if int32(rt.GetAudience()) != 99 {
		t.Fatalf("unknown audience enum not preserved: got %d want 99", rt.GetAudience())
	}
	if int32(rt.GetTools()[0].GetVisibility()) != 99 {
		t.Fatalf("unknown visibility enum not preserved: got %d want 99", rt.GetTools()[0].GetVisibility())
	}

	// (c) The 160 unknown-FIELD retention still holds on an audience-bearing
	// spec (an old consumer strips nothing).
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal audience spec: %v", err)
	}
	future := protowire.AppendTag(nil, 4001, protowire.BytesType)
	future = protowire.AppendString(future, "field-from-version-n-plus-1")
	wire = append(wire, future...)
	var fc experiencev1.AgentSpec
	if err := proto.Unmarshal(wire, &fc); err != nil {
		t.Fatalf("version-N consumer must accept version-N+1 bytes: %v", err)
	}
	rewire, err := proto.Marshal(&fc)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Contains(rewire, future) {
		t.Fatalf("unknown future field not preserved across re-serialize on an audience-bearing spec")
	}
}
