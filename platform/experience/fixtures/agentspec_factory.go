// agentspec_factory.go -- TRD 160-01, the AgentSpec fixture factory.
//
// Playbook habit #4: fixture generators, not LLM-generated test data. This is
// the HAND-BUILT builder every 160-* contract test draws from -- a
// helpdesk-shaped, ValidateAgentSpec-passing default plus functional options.
// NO randomness, NO generated strings: every value is a deliberate constant.
//
// COMPOSITION DISCIPLINE: the default spec's node is built with the EXISTING
// frozen-140 NewAgentNode builder and its tools with the EXISTING NewTool
// builder -- the factory composes the frozen types exactly the way the contract
// does (AgentSpec.node IS an AgentNode; AgentSpec.tools ARE ToolDefinitions).
package fixtures

import (
	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// Company-scope constants for AgentSpec fixtures. AgentSpec is COMPANY-scoped
// (eden-biz tenancy), so these mirror Default/WrongTenantID for the company
// axis: WrongCompanyID MUST differ from DefaultCompanyID so a baseline built
// with NewAgentSpec() always diverges under the wrong-tenant principal.
const (
	DefaultCompanyID = "company-fixture-0001"
	WrongCompanyID   = "company-fixture-9999"
)

// Helpdesk tool ids -- the four tools the helpdesk-shaped default binds. The
// ticket-create write is the ONLY write the default HITL policy allows without
// escalation (escalate_on_write_beyond).
const (
	HelpdeskToolKBSearch     = "kb.article.search"
	HelpdeskToolContactRead  = "crm.contact.read"
	HelpdeskToolTicketCreate = "helpdesk.ticket.create"
	HelpdeskToolMessageSend  = "comms.message.send"
)

// Default authored-agent values for a freshly built AgentSpec. Named constants
// so downstream tests reference the baseline without re-deriving it.
const (
	DefaultAgentSpecID      = "agentspec-fixture-helpdesk-0001"
	DefaultAgentSpecVersion = "1.0.0"
	// DefaultPersona is the grounded helpdesk posture: answer ONLY from
	// retrieved KB articles, otherwise escalate to a human.
	DefaultPersona = "answer only from retrieved KB articles, else escalate"
	// DefaultModelRef is an AOCore /v1/models catalog asset id placeholder --
	// model selection is a FIRST-CLASS typed field, never a config blob.
	DefaultModelRef           = "aocore/models/gpt-4o-mini"
	DefaultKnowledgeSourceRef = "kb://helpdesk/articles"
	DefaultConfidenceFloor    = 0.7
	DefaultMaxSteps           = int32(8)
	DefaultLifecycle          = "draft"
)

// HelpdeskToolIDs returns the default node's tool ids as a fresh slice per call
// (safe to mutate). Order is the binding order the default AgentNode carries.
func HelpdeskToolIDs() []string {
	return []string{
		HelpdeskToolKBSearch,
		HelpdeskToolContactRead,
		HelpdeskToolTicketCreate,
		HelpdeskToolMessageSend,
	}
}

// helpdeskTools builds the four ToolDefinitions matching HelpdeskToolIDs, using
// the frozen-140 NewTool builder: two reads (KB search, contact read) and two
// writes (ticket create, message send -- writes carry an idempotency key).
func helpdeskTools() []*experiencev1.ToolDefinition {
	return []*experiencev1.ToolDefinition{
		NewTool(
			WithAdapter(HelpdeskToolKBSearch),
			WithSchemas(
				`{"type":"object","properties":{"query":{"type":"string"}}}`,
				`{"type":"object","properties":{"articles":{"type":"array"}}}`,
			),
			WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_READ),
		),
		NewTool(
			WithAdapter(HelpdeskToolContactRead),
			WithSchemas(
				`{"type":"object","properties":{"contactId":{"type":"string"}}}`,
				`{"type":"object","properties":{"contact":{"type":"object"}}}`,
			),
			WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_READ),
		),
		NewTool(
			WithAdapter(HelpdeskToolTicketCreate),
			WithSchemas(
				`{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"}}}`,
				`{"type":"object","properties":{"ticketId":{"type":"string"}}}`,
			),
			WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
			WithIdempotencyKey("idem-helpdesk-ticket-create"),
		),
		NewTool(
			WithAdapter(HelpdeskToolMessageSend),
			WithSchemas(
				`{"type":"object","properties":{"to":{"type":"string"},"body":{"type":"string"}}}`,
				`{"type":"object","properties":{"messageId":{"type":"string"}}}`,
			),
			WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
			WithIdempotencyKey("idem-helpdesk-message-send"),
		),
	}
}

// AgentSpecOption mutates an AgentSpec in place. Compose them in NewAgentSpec.
type AgentSpecOption func(*experiencev1.AgentSpec)

// NewAgentSpec returns a valid, ValidateAgentSpec-passing helpdesk-shaped
// AgentSpec, then applies the options in order. Each call returns a fresh,
// independent (non-aliased) struct: grounded persona, first-class model_ref,
// the frozen AgentNode composed at .node, four matching tools, grounded-only
// knowledge, low-confidence + write-beyond HITL escalation, an 8-step budget,
// draft lifecycle, version 1.0.0.
func NewAgentSpec(opts ...AgentSpecOption) *experiencev1.AgentSpec {
	spec := &experiencev1.AgentSpec{
		Id:        DefaultAgentSpecID,
		Version:   DefaultAgentSpecVersion,
		CompanyId: DefaultCompanyID,
		Persona:   DefaultPersona,
		ModelRef:  DefaultModelRef,
		// COMPOSES the frozen 140 AgentNode via the existing builder ("" takes
		// the builder's default coherent io_envelope_schema).
		Node:  NewAgentNode("", HelpdeskToolIDs()...),
		Tools: helpdeskTools(),
		Knowledge: &experiencev1.KnowledgePolicy{
			SourceRefs:   []string{DefaultKnowledgeSourceRef},
			GroundedOnly: true,
		},
		Hitl: &experiencev1.HitlPolicy{
			EscalateOnLowConfidence: true,
			ConfidenceFloor:         DefaultConfidenceFloor,
			// The ticket-create write is the only autonomous write; every other
			// write (e.g. comms.message.send) escalates to a human first.
			EscalateOnWriteBeyond: []string{HelpdeskToolTicketCreate},
		},
		Budget: &experiencev1.BudgetPolicy{
			MaxSteps: DefaultMaxSteps,
		},
		Lifecycle: DefaultLifecycle,
	}
	for _, opt := range opts {
		opt(spec)
	}
	return spec
}

// WithCompany overrides the owning company scope (the tenancy axis the
// wrong-tenant tests diverge).
func WithCompany(id string) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.CompanyId = id }
}

// WithModelRef overrides the AOCore /v1/models catalog asset id.
func WithModelRef(ref string) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.ModelRef = ref }
}

// WithPersona overrides the grounding persona. Pass "" to build the
// missing-persona rejection fixture.
func WithPersona(persona string) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.Persona = persona }
}

// WithTools REPLACES the spec's tools. Call with no args to build the
// zero-tools rejection fixture.
func WithTools(tools ...*experiencev1.ToolDefinition) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.Tools = tools }
}

// WithMaxSteps overrides the budget's max_steps, lazily creating the budget so
// the option composes on a budget-less spec. Pass 0 / negative / over-ceiling
// values to build the budget rejection fixtures.
func WithMaxSteps(n int32) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) {
		if s.Budget == nil {
			s.Budget = &experiencev1.BudgetPolicy{}
		}
		s.Budget.MaxSteps = n
	}
}

// WithVersion overrides the spec's authored contract version.
func WithVersion(v string) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.Version = v }
}

// --- 161-01: the AUDIENCE dimension ------------------------------------------

// External-audience binding defaults (161-01). Hand-built constants: the
// external helpdesk voice answers in brand voice, may only search the KB, and
// escalates to a human support rep.
const (
	ExternalBindingPersona   = "brand-voice helpdesk: answer warmly from the public KB only"
	ExternalEscalationTarget = "human-support"
)

// WithAudience sets the spec-level audience declaration (161-01 field 20).
func WithAudience(a experiencev1.AgentAudience) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) { s.Audience = a }
}

// WithAudienceBinding APPENDS a per-audience binding (161-01 field 21) --
// call once per audience to compose a BOTH-shaped spec.
func WithAudienceBinding(b *experiencev1.AudienceBinding) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) {
		s.AudienceBindings = append(s.AudienceBindings, b)
	}
}

// WithToolVisibility sets tools[toolIdx].visibility (161-01 ToolDefinition
// field 6). toolIdx follows the fixture's binding order (0 = kb.article.search,
// 1 = crm.contact.read, 2 = helpdesk.ticket.create, 3 = comms.message.send).
// Out-of-range indexes are ignored (compose-safe on WithTools-replaced specs).
func WithToolVisibility(toolIdx int, v experiencev1.ToolVisibility) AgentSpecOption {
	return func(s *experiencev1.AgentSpec) {
		if toolIdx < 0 || toolIdx >= len(s.Tools) {
			return
		}
		s.Tools[toolIdx].Visibility = v
	}
}

// NewExternalAudienceBinding returns the hand-built external-audience override
// set: KB-search only (the customer-safe read), grounded on the helpdesk KB,
// brand-voice persona, escalating to a human support rep. Fresh struct per call.
func NewExternalAudienceBinding() *experiencev1.AudienceBinding {
	return &experiencev1.AudienceBinding{
		Audience:         experiencev1.AgentAudience_AGENT_AUDIENCE_EXTERNAL,
		ToolIds:          []string{HelpdeskToolKBSearch},
		KnowledgeIds:     []string{DefaultKnowledgeSourceRef},
		Persona:          ExternalBindingPersona,
		EscalationTarget: ExternalEscalationTarget,
	}
}
