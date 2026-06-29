// tool_factory.go -- TRD 140-07, must-haves #7..#10 (the LAST proto-writer).
//
// 140-02 left this file a DOCUMENTED STUB because the ToolDefinition message did
// not yet exist on the experience.v1 contract. 140-07 lands the typed
// tool/agent/signing/telemetry surface + every reserved-now-cheap seam, so this
// file is now FLESHED OUT with the functional-options builders the test-list
// draws from.
//
// SCOPE DISCIPLINE (still honored): these builders reference ONLY fields that the
// generated contract carries once 140-07's proto lands -- ToolDefinition,
// AgentNode, SigningSpec/CredentialRef (on AppDefinition), TelemetryEnvelope, and
// the reserved-cheap seams on ExperienceSpec.
//
// SECURITY MODEL (locked): a tool binds ONLY to a curated allowlist of pre-built
// tenant-safe adapters -- adapter_id is an allowlist FK, never an arbitrary
// RPC/SQL target. input_schema/output_schema are JSON-Schema STRINGS (a typed
// envelope), NEVER a free config_json blob. side_effect gates the dispatcher.
// AgentNode carries an io_envelope_schema so the stub->real LLM dispatcher swap
// (obj 144) is envelope-preserving. SigningSpec stores CredentialRefs (ref +
// custody) -- NEVER inline cert material.
package fixtures

import (
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// --- Adapter allowlist -----------------------------------------------------
//
// The curated set of pre-built, tenant-safe adapters a ToolDefinition may bind
// to. adapter_id is an allowlist FK: a tool whose adapter_id is NOT in this set
// is rejected by ValidateTooling (no arbitrary binding). The set is the fixture
// baseline the tests build from; the real allowlist is a runtime registry.
const (
	AdapterSearchContacts = "adapter.search_contacts" // READ adapter
	AdapterCreateInvoice  = "adapter.create_invoice"  // WRITE adapter
	AdapterSendWebhook    = "adapter.send_webhook"     // EXTERNAL adapter (deferred)
)

// AllowedAdapters is the curated allowlist a ToolDefinition.adapter_id must be a
// member of. Returned as a fresh set per call so a test can mutate it safely.
func AllowedAdapters() map[string]struct{} {
	return map[string]struct{}{
		AdapterSearchContacts: {},
		AdapterCreateInvoice:  {},
		AdapterSendWebhook:    {},
	}
}

// ToolOpt mutates a ToolDefinition in place. Compose them in NewTool.
type ToolOpt func(*experiencev1.ToolDefinition)

// NewTool returns a valid ToolDefinition with a default allowlisted adapter_id
// and typed JSON-Schema input/output envelopes, then applies opts. The defaults
// model a READ tool so a happy-path fixture is self-consistent without
// per-call boilerplate.
func NewTool(opts ...ToolOpt) *experiencev1.ToolDefinition {
	tool := &experiencev1.ToolDefinition{
		AdapterId:    AdapterSearchContacts,
		InputSchema:  `{"type":"object","properties":{"query":{"type":"string"}}}`,
		OutputSchema: `{"type":"object","properties":{"results":{"type":"array"}}}`,
		SideEffect:   experiencev1.SideEffect_SIDE_EFFECT_READ,
	}
	for _, opt := range opts {
		opt(tool)
	}
	return tool
}

// WithAdapter overrides the tool's adapter_id (the allowlist FK). Pass an id NOT
// in AllowedAdapters() to build the rejection fixture.
func WithAdapter(id string) ToolOpt {
	return func(t *experiencev1.ToolDefinition) { t.AdapterId = id }
}

// WithSchemas overrides the typed input/output JSON-Schema envelopes.
func WithSchemas(input, output string) ToolOpt {
	return func(t *experiencev1.ToolDefinition) {
		t.InputSchema = input
		t.OutputSchema = output
	}
}

// WithSideEffect overrides the side_effect gate (READ/WRITE/EXTERNAL).
func WithSideEffect(se experiencev1.SideEffect) ToolOpt {
	return func(t *experiencev1.ToolDefinition) { t.SideEffect = se }
}

// WithIdempotencyKey sets the idempotency_key (load-bearing for WRITE/EXTERNAL
// replays; reserved-but-typed).
func WithIdempotencyKey(key string) ToolOpt {
	return func(t *experiencev1.ToolDefinition) { t.IdempotencyKey = key }
}

// WithTool appends a ToolDefinition to the spec's tools (field 60).
func WithTool(tool *experiencev1.ToolDefinition) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.Tools = append(s.Tools, tool)
	}
}

// --- AgentNode -------------------------------------------------------------
//
// AgentNode carries the tool ids the agent may call + a TYPED io_envelope_schema
// (a JSON-Schema string). The io-envelope is the swap-stable seam: the stub
// dispatcher and the real LLM dispatcher (obj 144) both read/write THIS envelope,
// so the swap is envelope-preserving with NO contract change.

// NewAgentNode builds an AgentNode binding the given tool ids under a typed
// io_envelope_schema. The envelope defaults to a coherent object schema so a
// happy-path fixture round-trips an io-envelope without per-call boilerplate.
func NewAgentNode(ioEnvelopeSchema string, toolIDs ...string) *experiencev1.AgentNode {
	if ioEnvelopeSchema == "" {
		ioEnvelopeSchema = `{"type":"object","properties":{"input":{},"output":{}}}`
	}
	return &experiencev1.AgentNode{
		ToolIds:          toolIDs,
		IoEnvelopeSchema: ioEnvelopeSchema,
	}
}

// WithAgentNode appends an AgentNode to the spec's agent_nodes (field 61).
func WithAgentNode(node *experiencev1.AgentNode) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.AgentNodes = append(s.AgentNodes, node)
	}
}

// --- SigningSpec (AppDefinition) -------------------------------------------
//
// SigningSpec = map<platform -> CredentialRef{ref, custody}>. It stores a
// REFERENCE (a ref pointer + custody label), NEVER inline cert/key MATERIAL --
// the actual signing material lives in a custody system (1Password / KMS), out
// of the proto. The map carries BOTH ios and android refs.

// Signing platform keys for the SigningSpec.by_platform map.
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
)

// NewCredentialRef builds a CredentialRef: a ref pointer + a custody label. It
// is a REFERENCE only -- never cert/key bytes.
func NewCredentialRef(ref, custody string) *experiencev1.CredentialRef {
	return &experiencev1.CredentialRef{
		Ref:     ref,
		Custody: custody,
	}
}

// NewSigningSpec builds a SigningSpec carrying the given per-platform credential
// refs. Pass an ios AND an android ref to build the both-platforms fixture.
func NewSigningSpec(byPlatform map[string]*experiencev1.CredentialRef) *experiencev1.SigningSpec {
	return &experiencev1.SigningSpec{ByPlatform: byPlatform}
}

// --- TelemetryEnvelope -----------------------------------------------------
//
// The telemetry envelope carries every observability ID in-schema (not a free
// blob). tenant_id is asserted to equal the REQUESTING tenant only -- it is
// never an other-tenant id (no cross-tenant telemetry bleed).

// NewTelemetryEnvelope builds a fully-populated TelemetryEnvelope. tenantID is
// the requesting tenant -- callers pass DefaultTenantID for a baseline and
// WrongTenantID never appears here unless the request itself is wrong-tenant.
func NewTelemetryEnvelope(
	specID, specVersion, surfaceID, bindingID, entitlementSetHash,
	brand, themeProfile, formFactor, buildSHA, complianceProfile, tenantID string,
) *experiencev1.TelemetryEnvelope {
	return &experiencev1.TelemetryEnvelope{
		SpecId:             specID,
		SpecVersion:        specVersion,
		SurfaceId:          surfaceID,
		BindingId:          bindingID,
		EntitlementSetHash: entitlementSetHash,
		Brand:              brand,
		ThemeProfile:       themeProfile,
		FormFactor:         formFactor,
		BuildSha:           buildSHA,
		ComplianceProfile:  complianceProfile,
		TenantId:           tenantID,
	}
}

// --- Reserved-now-cheap seams ----------------------------------------------
//
// These seams are TYPED-but-empty-friendly: they round-trip whether empty or
// populated, reserving the field number now so a frozen-proto-forever message
// never has to migrate to add them later.

// NewActionGate builds an ActionGate: an action_id gated by an entitlement_key.
func NewActionGate(actionID, entitlementKey string) *experiencev1.ActionGate {
	return &experiencev1.ActionGate{
		ActionId:       actionID,
		EntitlementKey: entitlementKey,
	}
}

// WithActionGate appends an ActionGate to the spec's action_gates (field 80).
func WithActionGate(gate *experiencev1.ActionGate) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.ActionGates = append(s.ActionGates, gate)
	}
}

// WithFlagOverrides sets the spec's flag_overrides map (field 81).
func WithFlagOverrides(overrides map[string]string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.FlagOverrides = overrides }
}

// WithVariant sets the spec's variant (field 82).
func WithVariant(variant string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.Variant = variant }
}

// WithDeclaredStates sets the spec's declared_states (field 83) -- the
// enumerated render states (populated/empty/error/loading/...) for visual eval.
func WithDeclaredStates(states ...string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.DeclaredStates = states }
}

// WithCustomFields sets the spec's custom_fields map (field 84).
func WithCustomFields(fields map[string]string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.CustomFields = fields }
}

// WithRulePolicy sets the spec's rule_policy map (field 85).
func WithRulePolicy(policy map[string]string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.RulePolicy = policy }
}

// WithResolutionContext sets the spec's resolution_context (field 70): the
// tenant/org + resolved_at + resolver_version provenance of THIS resolution.
func WithResolutionContext(tenantID, orgID, resolvedAt, resolverVersion string) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.ResolutionContext = &experiencev1.ResolutionContext{
			TenantId:        tenantID,
			OrgId:           orgID,
			ResolvedAt:      resolvedAt,
			ResolverVersion: resolverVersion,
		}
	}
}

// NewLockedSurface builds a LockedSurface: a surface_id locked behind an upsell,
// carrying the upsell_reason shown to the user.
func NewLockedSurface(surfaceID, upsellReason string) *experiencev1.LockedSurface {
	return &experiencev1.LockedSurface{
		SurfaceId:    surfaceID,
		UpsellReason: upsellReason,
	}
}

// WithLockedSurface appends a LockedSurface to the spec's locked_surfaces
// (field 71).
func WithLockedSurface(locked *experiencev1.LockedSurface) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) {
		s.LockedSurfaces = append(s.LockedSurfaces, locked)
	}
}

// WithServerKillable sets the spec's server_killable kill flag (field 86) -- the
// REQUIRED fast-rollback lever for the generated fleet (a customer-own store
// account otherwise gives no kill switch you control).
func WithServerKillable(killable bool) SpecOpt {
	return func(s *experiencev1.ExperienceSpec) { s.ServerKillable = killable }
}

// --- SigningSpec on AppDefinition + app-service-slots ----------------------
//
// SigningSpec lives on AppDefinition (BUILD-time, like DeepLinkSpec) -- a store
// binary commits to its signing identity at submit time. app_service_slots are
// the build-time service-slot refs (search/notify/export/attach/print/audit).

// AppDefOpt mutates an AppDefinition in place. Compose them in NewAppDef.
type AppDefOpt func(*experiencev1.AppDefinition)

// NewAppDef returns a minimal AppDefinition with sane meta, then applies opts.
// Used by 140-07 tests to round-trip SigningSpec + app_service_slots, which live
// on AppDefinition (BUILD-time), not on the resolved ExperienceSpec.
func NewAppDef(opts ...AppDefOpt) *experiencev1.AppDefinition {
	def := &experiencev1.AppDefinition{
		Id: "appdef-fixture-0001",
		Meta: &experiencev1.AppMeta{
			Name:     "Fixture App",
			BundleId: "ai.aocyber.fixture",
		},
		MinBinaryVersion: defaultMinBinaryVersion,
		ContractVersion:  defaultContractVersion,
	}
	for _, opt := range opts {
		opt(def)
	}
	return def
}

// WithSigning sets the AppDefinition's SigningSpec (field 20).
func WithSigning(signing *experiencev1.SigningSpec) AppDefOpt {
	return func(d *experiencev1.AppDefinition) { d.Signing = signing }
}

// WithAppServiceSlots sets the AppDefinition's app_service_slots (field 30) --
// the build-time service-slot refs (search/notify/export/attach/print/audit).
func WithAppServiceSlots(slots ...string) AppDefOpt {
	return func(d *experiencev1.AppDefinition) { d.AppServiceSlots = slots }
}
