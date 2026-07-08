// agentspec.go -- TRD 160-01 (GREEN): the AgentSpec contract validator.
//
// AgentSpec (proto/experience/v1, additive 160-01 block) is the reusable,
// versioned authored-agent contract. It COMPOSES the frozen 140 AgentNode +
// ToolDefinition -- this file never touches those shapes; it is the COHERENCE
// logic for the authored-agent surface, mirroring tooling.go::ValidateTooling
// (a typed Code + accumulated, never-panics findings).
//
// TENANCY MODEL (locked, mirrors the StoreSpecRequest precedent + memories
// feedback_rest_authority_field_body_binding): company_id is the owning tenant
// scope, OVERWRITTEN by the authenticated principal at the store sink -- a body
// value can never plant a spec under another tenant. This validator is the
// read-side chokepoint: a spec whose declared scope diverges from the
// principal's scope is denied with a SINGLE permission-denied finding that is
// BYTE-IDENTICAL to the missing-spec case (no existence oracle), and NO content
// validation runs on a foreign spec (a content finding would itself leak).
package experience

import (
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// AgentSpecCode is the typed kind of an agent-spec coherence finding. Stable
// codes let callers (tests, the builder ProblemsPanel, the eden-biz sink)
// branch without string-matching -- mirroring ToolingCode / CoherenceCode.
type AgentSpecCode string

const (
	// AgentSpecPermissionDenied -- the principal scope diverges from the spec's
	// declared company scope, OR the spec does not exist. ONE code, byte-equal
	// message for both cases (no cross-tenant existence oracle).
	AgentSpecPermissionDenied AgentSpecCode = "agentspec.permission_denied"
	// AgentSpecMissingPersona -- the grounding persona is empty. A persona-less
	// agent has no behavioral contract; fail-closed.
	AgentSpecMissingPersona AgentSpecCode = "agentspec.missing_persona"
	// AgentSpecMissingModelRef -- model_ref (the AOCore /v1/models catalog
	// asset id) is empty. Model selection is first-class; fail-closed.
	AgentSpecMissingModelRef AgentSpecCode = "agentspec.missing_model_ref"
	// AgentSpecNoTools -- the spec binds zero tools. An agent with no typed
	// tool contract is not dispatchable; fail-closed.
	AgentSpecNoTools AgentSpecCode = "agentspec.no_tools"
	// AgentSpecBudgetOutOfRange -- budget.max_steps is outside (0, ceiling].
	// An unset/zero budget is NOT an unlimited budget; fail-closed.
	AgentSpecBudgetOutOfRange AgentSpecCode = "agentspec.budget_out_of_range"
	// AgentSpecMissingAdapterID -- a bound tool has an empty adapter_id. The
	// curated-allowlist membership check itself is the eden-biz sink's job
	// (per-scope registry); the CONTRACT requires the FK to be present.
	AgentSpecMissingAdapterID AgentSpecCode = "agentspec.missing_adapter_id"
)

// AgentSpecMaxStepsCeiling is the upper bound ValidateAgentSpec allows for
// budget.max_steps. A runaway budget is a cost + blast-radius hazard; the
// ceiling is the contract-level guardrail (the runtime may enforce lower).
const AgentSpecMaxStepsCeiling int32 = 50

// AgentSpecError is one machine-checked agent-spec finding. Message is
// INTENTIONALLY non-leaking for the permission-denied code: it never echoes
// either scope id or any spec content, so a foreign spec and a missing spec
// yield byte-identical findings.
type AgentSpecError struct {
	Code    AgentSpecCode
	Message string
}

func (e AgentSpecError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// deniedAgentSpec is the SINGLE non-leaking denial returned for BOTH the
// diverged-scope and the missing-spec case. One shared constructor guarantees
// the two paths can never drift into distinguishable findings.
func deniedAgentSpec() []AgentSpecError {
	return []AgentSpecError{{
		Code:    AgentSpecPermissionDenied,
		Message: "agent spec is not available in this scope",
	}}
}

// ValidateAgentSpec machine-checks an AgentSpec against the requesting
// principal's scope, mirroring ValidateTooling's accumulated fail-closed
// posture. principalScope is the AUTHENTICATED principal's company scope --
// derived from identity, never from a request body.
//
// Rule 0 (tenancy, checked FIRST and alone): a nil spec OR a spec whose
// company_id diverges from principalScope returns the single non-leaking
// permission-denied finding -- byte-identical in both cases, with NO content
// validation performed (a content finding on a foreign spec would leak).
//
// Content rules (all accumulated, never short-circuited, so the builder sees
// every problem at once):
//  1. persona is non-empty (AgentSpecMissingPersona).
//  2. model_ref is non-empty (AgentSpecMissingModelRef).
//  3. at least one tool is bound (AgentSpecNoTools).
//  4. budget.max_steps is in (0, AgentSpecMaxStepsCeiling] (AgentSpecBudgetOutOfRange).
//  5. every bound tool carries a non-empty adapter_id (AgentSpecMissingAdapterID);
//     allowlist MEMBERSHIP is deferred to the eden-biz sink's per-scope registry.
func ValidateAgentSpec(spec *experiencev1.AgentSpec, principalScope string) []AgentSpecError {
	// (0) Tenancy chokepoint: missing spec and foreign spec are the SAME denial.
	if spec == nil || spec.GetCompanyId() != principalScope {
		return deniedAgentSpec()
	}

	var errs []AgentSpecError

	// (1) grounding persona present.
	if spec.GetPersona() == "" {
		errs = append(errs, AgentSpecError{
			Code:    AgentSpecMissingPersona,
			Message: "persona must be non-empty (the agent's behavioral contract)",
		})
	}

	// (2) first-class model selection present.
	if spec.GetModelRef() == "" {
		errs = append(errs, AgentSpecError{
			Code:    AgentSpecMissingModelRef,
			Message: "model_ref must reference an AOCore /v1/models catalog asset id",
		})
	}

	// (3) at least one typed tool contract bound.
	if len(spec.GetTools()) == 0 {
		errs = append(errs, AgentSpecError{
			Code:    AgentSpecNoTools,
			Message: "an agent spec must bind at least one tool",
		})
	}

	// (4) budget fail-closed: (0, ceiling]. A nil budget reads as max_steps 0.
	if steps := spec.GetBudget().GetMaxSteps(); steps <= 0 || steps > AgentSpecMaxStepsCeiling {
		errs = append(errs, AgentSpecError{
			Code: AgentSpecBudgetOutOfRange,
			Message: fmt.Sprintf(
				"budget.max_steps must be in (0, %d]", AgentSpecMaxStepsCeiling),
		})
	}

	// (5) every bound tool names its adapter FK (allowlist membership is the
	// eden-biz sink's per-scope decision -- the contract requires presence).
	for _, tool := range spec.GetTools() {
		if tool.GetAdapterId() == "" {
			errs = append(errs, AgentSpecError{
				Code:    AgentSpecMissingAdapterID,
				Message: "every bound tool must name its curated-allowlist adapter_id",
			})
		}
	}

	return errs
}
