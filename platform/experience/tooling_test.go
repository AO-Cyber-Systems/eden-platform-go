// tooling_test.go -- TRD 140-07, must-haves #7..#10 (the LAST proto-writer).
//
// This is the RED test for the FINAL proto shape. It exercises the typed
// tool/agent/signing/telemetry surface + every reserved-now-cheap seam, then the
// ValidateTooling allowlist + side_effect + io-envelope rules, then the
// wrong-tenant non-leak. After GREEN the experience.v1 message shape is FROZEN.
//
// What each block proves:
//   - ToolDefinition is TYPED (adapter_id allowlist FK + JSON-Schema input/output
//     envelopes + side_effect gate + idempotency_key), NOT a config_json blob.
//     WRITE is distinguishable from READ on the wire.
//   - AgentNode round-trips a TYPED io_envelope_schema -- the swap-stable seam the
//     real LLM dispatcher (obj 144) plugs into without a contract change.
//   - SigningSpec carries BOTH ios + android CredentialRefs (ref + custody) and
//     NEVER inline cert material (asserted: no cert/key bytes on the wire).
//   - TelemetryEnvelope round-trips every observability ID in-schema.
//   - server_killable + an error-boundary marker are present per-surface.
//   - ResolutionContext + LockedSurface + every reserved-cheap seam round-trip.
//   - ValidateTooling rejects an off-allowlist adapter, flags side_effect=external
//     as deferred-but-representable, and rejects a malformed io-envelope.
//   - Wrong-tenant: a tool/telemetry built for tenant A, run through
//     fixtures.WrongTenant, diverges scope while every typed value survives, and
//     ValidateTooling against B's adapter scope collapses to the SAME not-allowed
//     code as an unknown adapter (no existence oracle).
//
// Fixtures only: NewTool/NewAgentNode/NewSigningSpec/NewCredentialRef/
// NewTelemetryEnvelope/NewActionGate/NewLockedSurface/NewAppDef + the With* opts
// + WrongTenant. No hand-built literals.
package experience_test

import (
	"strings"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
	"google.golang.org/protobuf/proto"
)

// rt marshals then unmarshals a spec, failing on any wire error.
func rt(t *testing.T, spec *experiencev1.ExperienceSpec) *experiencev1.ExperienceSpec {
	t.Helper()
	wire, err := proto.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got experiencev1.ExperienceSpec
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &got
}

// rtAppDef marshals then unmarshals an AppDefinition (SigningSpec lives here).
func rtAppDef(t *testing.T, def *experiencev1.AppDefinition) *experiencev1.AppDefinition {
	t.Helper()
	wire, err := proto.Marshal(def)
	if err != nil {
		t.Fatalf("marshal appdef: %v", err)
	}
	var got experiencev1.AppDefinition
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal appdef: %v", err)
	}
	return &got
}

// --- Happy: ToolDefinition -------------------------------------------------

// ToolDefinition round-trips adapter_id + typed input/output schemas +
// side_effect + idempotency_key. WRITE is distinguishable from READ.
func TestToolDefinition_RoundTrips(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithTool(fixtures.NewTool(
			fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
			fixtures.WithSchemas(
				`{"type":"object","properties":{"amount":{"type":"number"}}}`,
				`{"type":"object","properties":{"invoiceId":{"type":"string"}}}`,
			),
			fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
			fixtures.WithIdempotencyKey("idem-key-001"),
		)),
	)

	got := rt(t, spec)
	if len(got.GetTools()) != 1 {
		t.Fatalf("tools len = %d, want 1", len(got.GetTools()))
	}
	tool := got.GetTools()[0]
	if tool.GetAdapterId() != fixtures.AdapterCreateInvoice {
		t.Errorf("adapter_id = %q, want %q", tool.GetAdapterId(), fixtures.AdapterCreateInvoice)
	}
	if !strings.Contains(tool.GetInputSchema(), "amount") {
		t.Errorf("input_schema lost typed envelope: %q", tool.GetInputSchema())
	}
	if !strings.Contains(tool.GetOutputSchema(), "invoiceId") {
		t.Errorf("output_schema lost typed envelope: %q", tool.GetOutputSchema())
	}
	if tool.GetSideEffect() != experiencev1.SideEffect_SIDE_EFFECT_WRITE {
		t.Errorf("side_effect = %v, want WRITE", tool.GetSideEffect())
	}
	if tool.GetIdempotencyKey() != "idem-key-001" {
		t.Errorf("idempotency_key = %q", tool.GetIdempotencyKey())
	}
}

// WRITE side_effect is distinguishable from READ on the wire (gates the
// dispatcher). A READ tool and a WRITE tool on one spec keep distinct effects.
func TestToolDefinition_WriteDistinctFromRead(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithTool(fixtures.NewTool()), // default READ
		fixtures.WithTool(fixtures.NewTool(
			fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
			fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
		)),
	)
	got := rt(t, spec)
	if got.GetTools()[0].GetSideEffect() != experiencev1.SideEffect_SIDE_EFFECT_READ {
		t.Errorf("tool[0] side_effect = %v, want READ", got.GetTools()[0].GetSideEffect())
	}
	if got.GetTools()[1].GetSideEffect() != experiencev1.SideEffect_SIDE_EFFECT_WRITE {
		t.Errorf("tool[1] side_effect = %v, want WRITE", got.GetTools()[1].GetSideEffect())
	}
}

// --- Happy: AgentNode io-envelope ------------------------------------------

// AgentNode round-trips its tool ids + the TYPED io_envelope_schema. The
// io-envelope is the swap-stable seam the real LLM dispatcher plugs into.
func TestAgentNode_IoEnvelopeRoundTrips(t *testing.T) {
	envelope := `{"type":"object","properties":{"input":{"type":"string"},"output":{"type":"string"}}}`
	spec := fixtures.NewSpec(
		fixtures.WithAgentNode(fixtures.NewAgentNode(envelope,
			fixtures.AdapterSearchContacts, fixtures.AdapterCreateInvoice)),
	)
	got := rt(t, spec)
	if len(got.GetAgentNodes()) != 1 {
		t.Fatalf("agent_nodes len = %d, want 1", len(got.GetAgentNodes()))
	}
	node := got.GetAgentNodes()[0]
	if node.GetIoEnvelopeSchema() != envelope {
		t.Errorf("io_envelope_schema dropped/changed: %q", node.GetIoEnvelopeSchema())
	}
	if len(node.GetToolIds()) != 2 {
		t.Errorf("tool_ids len = %d, want 2", len(node.GetToolIds()))
	}
}

// --- Happy: SigningSpec (ios + android, refs only) -------------------------

// SigningSpec round-trips BOTH ios + android CredentialRefs (ref + custody) and
// carries NO inline cert/key MATERIAL -- only references.
func TestSigningSpec_BothPlatformsRefsOnly(t *testing.T) {
	def := fixtures.NewAppDef(
		fixtures.WithSigning(fixtures.NewSigningSpec(map[string]*experiencev1.CredentialRef{
			fixtures.PlatformIOS:     fixtures.NewCredentialRef("op://AOCyber/ios-dist-cert", "1password"),
			fixtures.PlatformAndroid: fixtures.NewCredentialRef("op://AOCyber/android-keystore", "1password"),
		})),
	)
	got := rtAppDef(t, def)
	sign := got.GetSigning()
	if sign == nil {
		t.Fatal("signing dropped on round-trip")
	}
	ios := sign.GetByPlatform()[fixtures.PlatformIOS]
	android := sign.GetByPlatform()[fixtures.PlatformAndroid]
	if ios == nil || android == nil {
		t.Fatalf("missing platform ref: ios=%v android=%v", ios, android)
	}
	if ios.GetRef() != "op://AOCyber/ios-dist-cert" || ios.GetCustody() != "1password" {
		t.Errorf("ios ref = %q custody = %q", ios.GetRef(), ios.GetCustody())
	}
	if android.GetRef() != "op://AOCyber/android-keystore" {
		t.Errorf("android ref = %q", android.GetRef())
	}

	// NO cert MATERIAL: a ref points to custody (op://...), it is not PEM/DER
	// bytes. Assert no cert/key markers ever appear on the wire.
	wire, err := proto.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, marker := range []string{"BEGIN CERTIFICATE", "PRIVATE KEY", "BEGIN RSA", "-----BEGIN"} {
		if strings.Contains(string(wire), marker) {
			t.Errorf("signing carried inline cert material (%q) -- must be ref-only", marker)
		}
	}
}

// --- Happy: TelemetryEnvelope ----------------------------------------------

// TelemetryEnvelope round-trips every observability ID in-schema (not a blob).
func TestTelemetryEnvelope_AllIDsRoundTrip(t *testing.T) {
	env := fixtures.NewTelemetryEnvelope(
		"spec-1", "v1.2.3", "surface.invoices", "binding.invoice",
		"ent-hash-abc", "acme", "eden-light", "tablet", "buildsha-deadbeef",
		"fedramp-moderate", fixtures.DefaultTenantID,
	)
	wire, err := proto.Marshal(env)
	if err != nil {
		t.Fatalf("marshal telemetry: %v", err)
	}
	var got experiencev1.TelemetryEnvelope
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal telemetry: %v", err)
	}
	if got.GetSpecId() != "spec-1" || got.GetSpecVersion() != "v1.2.3" {
		t.Errorf("spec id/version lost: %q / %q", got.GetSpecId(), got.GetSpecVersion())
	}
	if got.GetSurfaceId() != "surface.invoices" || got.GetBindingId() != "binding.invoice" {
		t.Errorf("surface/binding id lost: %q / %q", got.GetSurfaceId(), got.GetBindingId())
	}
	if got.GetEntitlementSetHash() != "ent-hash-abc" {
		t.Errorf("entitlement_set_hash lost: %q", got.GetEntitlementSetHash())
	}
	if got.GetBrand() != "acme" || got.GetThemeProfile() != "eden-light" || got.GetFormFactor() != "tablet" {
		t.Errorf("brand/theme/form lost: %q/%q/%q", got.GetBrand(), got.GetThemeProfile(), got.GetFormFactor())
	}
	if got.GetBuildSha() != "buildsha-deadbeef" || got.GetComplianceProfile() != "fedramp-moderate" {
		t.Errorf("buildSHA/compliance lost: %q/%q", got.GetBuildSha(), got.GetComplianceProfile())
	}
	if got.GetTenantId() != fixtures.DefaultTenantID {
		t.Errorf("tenant_id = %q, want %q", got.GetTenantId(), fixtures.DefaultTenantID)
	}
}

// --- Happy: server_killable + error-boundary -------------------------------

// A spec carries server_killable (the required fast-rollback kill flag) and it
// round-trips true. server_killable is the lever the gap-review demanded for the
// generated fleet (a customer-own store account gives no kill switch otherwise).
func TestServerKillable_RoundTrips(t *testing.T) {
	spec := fixtures.NewSpec(fixtures.WithServerKillable(true))
	got := rt(t, spec)
	if !got.GetServerKillable() {
		t.Error("server_killable dropped on round-trip (must persist true)")
	}
}

// --- Happy: ResolutionContext + LockedSurface ------------------------------

// ResolutionContext + LockedSurface round-trip (telemetry/upsell provenance).
func TestResolutionContextAndLockedSurface_RoundTrip(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithResolutionContext(fixtures.DefaultTenantID, fixtures.DefaultOrgID, "2026-06-29T20:00:00Z", "resolver-v1"),
		fixtures.WithLockedSurface(fixtures.NewLockedSurface("surface.payroll", "upgrade to Pro for payroll")),
	)
	got := rt(t, spec)
	rc := got.GetResolutionContext()
	if rc == nil || rc.GetResolvedAt() != "2026-06-29T20:00:00Z" || rc.GetResolverVersion() != "resolver-v1" {
		t.Errorf("resolution_context lost: %+v", rc)
	}
	if rc.GetTenantId() != fixtures.DefaultTenantID {
		t.Errorf("resolution_context tenant = %q", rc.GetTenantId())
	}
	if len(got.GetLockedSurfaces()) != 1 ||
		got.GetLockedSurfaces()[0].GetSurfaceId() != "surface.payroll" ||
		got.GetLockedSurfaces()[0].GetUpsellReason() != "upgrade to Pro for payroll" {
		t.Errorf("locked_surfaces lost: %+v", got.GetLockedSurfaces())
	}
}

// --- Edge: reserved-cheap seams round-trip (populated) ----------------------

// Every reserved-now-cheap seam round-trips when populated: action_gates,
// flag_overrides, variant, declared_states, custom_fields, rule_policy +
// app_service_slots (on AppDefinition).
func TestReservedCheapSeams_RoundTrip(t *testing.T) {
	spec := fixtures.NewSpec(
		fixtures.WithActionGate(fixtures.NewActionGate("action.refund", "entitlement.refunds")),
		fixtures.WithFlagOverrides(map[string]string{"newCheckout": "on"}),
		fixtures.WithVariant("variant-B"),
		fixtures.WithDeclaredStates("populated", "empty", "error", "loading"),
		fixtures.WithCustomFields(map[string]string{"vertical": "field-service"}),
		fixtures.WithRulePolicy(map[string]string{"refund": "manager-approval"}),
	)
	got := rt(t, spec)
	if len(got.GetActionGates()) != 1 || got.GetActionGates()[0].GetEntitlementKey() != "entitlement.refunds" {
		t.Errorf("action_gates lost: %+v", got.GetActionGates())
	}
	if got.GetFlagOverrides()["newCheckout"] != "on" {
		t.Errorf("flag_overrides lost: %+v", got.GetFlagOverrides())
	}
	if got.GetVariant() != "variant-B" {
		t.Errorf("variant lost: %q", got.GetVariant())
	}
	if len(got.GetDeclaredStates()) != 4 {
		t.Errorf("declared_states lost: %+v", got.GetDeclaredStates())
	}
	if got.GetCustomFields()["vertical"] != "field-service" {
		t.Errorf("custom_fields lost: %+v", got.GetCustomFields())
	}
	if got.GetRulePolicy()["refund"] != "manager-approval" {
		t.Errorf("rule_policy lost: %+v", got.GetRulePolicy())
	}

	def := fixtures.NewAppDef(fixtures.WithAppServiceSlots("search", "notify", "export", "attach", "print", "audit"))
	gotDef := rtAppDef(t, def)
	if len(gotDef.GetAppServiceSlots()) != 6 {
		t.Errorf("app_service_slots lost: %+v", gotDef.GetAppServiceSlots())
	}
}

// Reserved-cheap seams round-trip EMPTY without breaking the message (a spec
// that declares none of them still marshals/unmarshals cleanly).
func TestReservedCheapSeams_EmptyRoundTrip(t *testing.T) {
	spec := fixtures.NewSpec()
	got := rt(t, spec)
	if got.GetActionGates() != nil || got.GetFlagOverrides() != nil ||
		got.GetVariant() != "" || got.GetDeclaredStates() != nil ||
		got.GetCustomFields() != nil || got.GetRulePolicy() != nil ||
		got.GetServerKillable() {
		t.Errorf("empty reserved seams did not round-trip empty: %+v", got)
	}
}

// --- Edge: ValidateTooling allowlist + side_effect + io-envelope -----------

// An off-allowlist adapter_id is rejected (no arbitrary binding).
func TestValidateTooling_OffAllowlistRejected(t *testing.T) {
	tool := fixtures.NewTool(fixtures.WithAdapter("adapter.arbitrary_sql_DROP_TABLE"))
	errs := experience.ValidateTooling([]*experiencev1.ToolDefinition{tool}, fixtures.AllowedAdapters())
	if len(errs) == 0 {
		t.Fatal("off-allowlist adapter accepted -- arbitrary binding allowed")
	}
	if errs[0].Code != experience.ToolingAdapterNotAllowed {
		t.Errorf("code = %q, want %q", errs[0].Code, experience.ToolingAdapterNotAllowed)
	}
}

// An allowlisted adapter passes ValidateTooling clean.
func TestValidateTooling_AllowlistedPasses(t *testing.T) {
	tool := fixtures.NewTool(fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
		fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE))
	errs := experience.ValidateTooling([]*experiencev1.ToolDefinition{tool}, fixtures.AllowedAdapters())
	if len(errs) != 0 {
		t.Errorf("allowlisted tool rejected: %+v", errs)
	}
}

// side_effect=EXTERNAL is representable but flagged DEFERRED (outbound webhooks
// out of scope) -- a warn-level code, not a hard reject.
func TestValidateTooling_ExternalFlaggedDeferred(t *testing.T) {
	tool := fixtures.NewTool(fixtures.WithAdapter(fixtures.AdapterSendWebhook),
		fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_EXTERNAL))
	errs := experience.ValidateTooling([]*experiencev1.ToolDefinition{tool}, fixtures.AllowedAdapters())
	found := false
	for _, e := range errs {
		if e.Code == experience.ToolingExternalDeferred {
			found = true
		}
		if e.Code == experience.ToolingAdapterNotAllowed {
			t.Errorf("allowlisted external adapter wrongly rejected as not-allowed")
		}
	}
	if !found {
		t.Errorf("side_effect=external not flagged deferred: %+v", errs)
	}
}

// A malformed io-envelope (empty/blank schema) on a tool is rejected -- the typed
// envelope must be present (not a free blob, not absent).
func TestValidateTooling_MalformedEnvelopeRejected(t *testing.T) {
	tool := fixtures.NewTool(fixtures.WithSchemas("", ""))
	errs := experience.ValidateTooling([]*experiencev1.ToolDefinition{tool}, fixtures.AllowedAdapters())
	found := false
	for _, e := range errs {
		if e.Code == experience.ToolingMalformedEnvelope {
			found = true
		}
	}
	if !found {
		t.Errorf("empty io-envelope not rejected: %+v", errs)
	}
}

// --- Failure / wrong-tenant ------------------------------------------------

// Wrong-tenant: a spec carrying tools/agents/telemetry, run through
// fixtures.WrongTenant, diverges scope while every typed tooling value survives
// untouched -- no tool/agent bleed across tenants.
func TestWrongTenant_ToolingSurvivesScopeDivergence(t *testing.T) {
	baseline := fixtures.NewSpec(
		fixtures.WithTool(fixtures.NewTool(fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
			fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE))),
		fixtures.WithAgentNode(fixtures.NewAgentNode("", fixtures.AdapterCreateInvoice)),
		fixtures.WithServerKillable(true),
	)
	wrong := fixtures.WrongTenant(baseline)

	if wrong.GetTenantId() == baseline.GetTenantId() || wrong.GetOrgId() == baseline.GetOrgId() {
		t.Fatal("WrongTenant did not diverge scope")
	}
	// Typed tooling survives untouched across the scope divergence.
	if len(wrong.GetTools()) != 1 || wrong.GetTools()[0].GetAdapterId() != fixtures.AdapterCreateInvoice {
		t.Errorf("tool bled/lost across wrong-tenant: %+v", wrong.GetTools())
	}
	if wrong.GetTools()[0].GetSideEffect() != experiencev1.SideEffect_SIDE_EFFECT_WRITE {
		t.Errorf("tool side_effect changed across wrong-tenant")
	}
	if len(wrong.GetAgentNodes()) != 1 {
		t.Errorf("agent node lost across wrong-tenant")
	}
	if !wrong.GetServerKillable() {
		t.Errorf("server_killable lost across wrong-tenant")
	}
}

// Wrong-tenant non-leak: a tool whose adapter is scoped to tenant B, requested
// from an A spec, collapses to the SAME not-allowed code as an UNKNOWN adapter
// (no existence oracle). ValidateTooling decides allow ONLY against the
// caller-supplied (A's) allowed-adapter set -- a B-scoped adapter not in A's set
// is indistinguishable from a wholly-unknown adapter.
func TestWrongTenant_AdapterScopeNonLeaking(t *testing.T) {
	// A's allowlist does NOT include B's tenant-scoped adapter.
	allowedForA := fixtures.AllowedAdapters()

	bScopedAdapter := "adapter.tenantB.private_export"
	unknownAdapter := "adapter.does_not_exist_anywhere"

	toolB := fixtures.NewTool(fixtures.WithAdapter(bScopedAdapter))
	toolUnknown := fixtures.NewTool(fixtures.WithAdapter(unknownAdapter))

	errsB := experience.ValidateTooling([]*experiencev1.ToolDefinition{toolB}, allowedForA)
	errsUnknown := experience.ValidateTooling([]*experiencev1.ToolDefinition{toolUnknown}, allowedForA)

	if len(errsB) == 0 || len(errsUnknown) == 0 {
		t.Fatal("expected both B-scoped and unknown adapters to be rejected")
	}
	if errsB[0].Code != errsUnknown[0].Code {
		t.Errorf("oracle leak: B-scoped code %q != unknown code %q (must be identical)",
			errsB[0].Code, errsUnknown[0].Code)
	}
	if errsB[0].Code != experience.ToolingAdapterNotAllowed {
		t.Errorf("B-scoped adapter code = %q, want %q", errsB[0].Code, experience.ToolingAdapterNotAllowed)
	}
	// Messages must be identical too -- no oracle in the human-readable text.
	if errsB[0].Message != errsUnknown[0].Message {
		t.Errorf("oracle leak in message: %q != %q", errsB[0].Message, errsUnknown[0].Message)
	}
}
