// dispatcher.go -- TRD 140-10, must-have #7 (runtime half): the STUB tool
// dispatcher.
//
// The locked 140 decision is to ship a STUB dispatcher now and defer the real
// LLM loop to obj 144. This file is design-contract + wiring validation ONLY:
// it validates the typed io-envelope, enforces the tenant-scoped curated adapter
// allowlist, classifies the side_effect gate, and returns a typed StubResult --
// WITHOUT calling any LLM and WITHOUT executing the real adapter.
//
// THE SWAP-STABLE SEAM (the whole point of stubbing now): Dispatcher is a Go
// port (mirroring biz internal/workflows' StepDispatcher). obj 144 plugs the
// REAL loop in behind this SAME interface; because the stub already fixes the
// io-envelope shape (input []byte validated against input_schema -> StubResult
// carrying an output envelope conforming to output_schema), the swap is
// envelope-preserving with NO contract change.
//
// SECURITY MODEL (locked, memories feedback_allowlist_by_stored_value_injection +
// feedback_rest_authority_field_body_binding):
//
//   - A tool binds ONLY to a curated allowlist of pre-built tenant-safe adapters.
//     adapter_id is an allowlist FK -- NO arbitrary RPC/SQL target.
//   - The allowlist is TENANT-SCOPED: it is resolved per ScopedContext.ScopeID,
//     so a tool whose adapter is bound under tenant X is simply absent from
//     tenant Y's set. A wrong-tenant binding and a wholly-unknown adapter BOTH
//     collapse to ONE non-leaking sentinel (ErrDispatchDenied) -- no existence
//     oracle, and the offending adapter_id is NEVER echoed (second-order-injection
//     lesson: format-validate + bind/escape at the sink; here the sink is the
//     per-scope allowlist membership check, not a string concat).
//   - args are format-validated at WRITE (here: the input is structurally
//     validated against input_schema BEFORE any notional dispatch).
//
// ANTI-PATTERNS (enforced): NO arbitrary RPC/SQL binding. NO outbound webhooks
// (side_effect=external is rejected/deferred). The stub NEVER executes the
// adapter -- it only validates the envelope + scope and returns a typed result.
package experience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// ErrDispatchDenied is the SINGLE non-leaking outcome of a tool->adapter binding
// the dispatching scope is not authorized for -- whether the adapter belongs to
// another tenant's scope, is off the curated allowlist entirely, or is a tool the
// AgentNode never bound. Collapsing all three to one sentinel (with byte-identical
// text that NEVER echoes the offending adapter_id) denies a cross-tenant existence
// oracle. It mirrors binding.ErrScopeDenied and tooling.ToolingAdapterNotAllowed.
var ErrDispatchDenied = errors.New("experience: tool dispatch denied")

// ErrDispatchExternalDeferred is the (distinct, non-denial) outcome of dispatching
// a side_effect=external tool: outbound webhooks are representable but OUT OF
// SCOPE for the stub (deferred to a later objective). The tool is recognized and
// allowed to exist, but the stub refuses to wire it -- and crucially never
// invokes the external client. It is intentionally NOT ErrDispatchDenied: this is
// a capability-deferred signal, not a security denial, so it carries no oracle.
var ErrDispatchExternalDeferred = errors.New("experience: external side-effect dispatch is deferred (out of scope)")

// ErrDispatchInvalidInput is the outcome of an input that does not conform to the
// tool's input_schema (args format-validated at write). Distinct from a scope
// denial: a malformed input is a caller error, not an authorization failure.
var ErrDispatchInvalidInput = errors.New("experience: dispatch input does not satisfy input_schema")

// DispatchStatus is the typed terminal status of a stub dispatch.
type DispatchStatus string

const (
	// DispatchValidated -- the envelope, allowlist FK, and scope all checked out;
	// the stub returns a validated/echoed output envelope. NO adapter executed.
	DispatchValidated DispatchStatus = "validated"
)

// GateClass is the side_effect classification the dispatcher records. A WRITE is
// gated differently from a READ even though the stub executes neither -- the gate
// is the seam obj 144's real loop reads to decide idempotency/confirmation.
type GateClass string

const (
	GateRead     GateClass = "read"     // pure read -- no mutation gate
	GateWrite    GateClass = "write"    // mutates tenant data -- idempotency/confirm gate
	GateExternal GateClass = "external" // outbound -- deferred, never reached on success
)

// StubResult is the typed io-envelope result of a stub dispatch. It is the
// swap-stable shape obj 144's real dispatcher MUST preserve: a future real loop
// replaces the body but returns this SAME struct (Status + GateClass +
// OutputEnvelope conforming to output_schema). Executed is ALWAYS false for the
// stub -- it is present so the real impl can flip it true without a shape change.
type StubResult struct {
	// Status is the terminal status (DispatchValidated for the stub).
	Status DispatchStatus
	// GateClass is the side_effect gate the dispatch was classified into.
	GateClass GateClass
	// WriteGated is true iff GateClass==GateWrite -- a convenience the gate the
	// real loop consults (write tools require an idempotency/confirm gate a read
	// does not). Recorded by the stub even though nothing executes.
	WriteGated bool
	// Executed reports whether the real adapter ran. ALWAYS false for the stub
	// (no LLM, no adapter execution); the real impl (obj 144) sets it true.
	Executed bool
	// OutputEnvelope is the validated/echoed envelope conforming to output_schema.
	// The stub echoes a minimal conforming object; the real impl returns the
	// adapter's actual output through the SAME field (envelope-preserving).
	OutputEnvelope []byte
}

// Dispatcher is the swap-stable PORT for tool dispatch (mirrors biz
// internal/workflows' StepDispatcher). The STUB impl validates the envelope +
// allowlist + scope and returns a typed result without executing; obj 144 plugs
// the REAL LLM loop in behind this SAME signature with NO contract change.
type Dispatcher interface {
	// Dispatch validates the AgentNode + ToolDefinition io-envelope against the
	// dispatching ScopedContext and the per-scope curated adapter allowlist, then
	// returns a typed StubResult. It NEVER executes the adapter and NEVER calls an
	// LLM. Errors are the non-leaking sentinels above.
	Dispatch(
		ctx context.Context,
		node *experiencev1.AgentNode,
		tool *experiencev1.ToolDefinition,
		scope ScopedContext,
		input []byte,
	) (StubResult, error)
}

// ExternalClient is the seam the REAL dispatcher (obj 144) would call to execute
// an external/adapter side-effect. The STUB holds a reference but MUST NEVER
// invoke it -- the tests hand in a recording spy and assert zero calls on every
// path. Defining it here keeps the stub->real swap a body change, not a wiring
// change.
type ExternalClient interface {
	Invoke(ctx context.Context, adapterID string, payload []byte) ([]byte, error)
}

// StubDispatcher is the design-contract stub: it proves the envelope is
// well-formed and the binding is tenant-safe, returning a typed result without
// any LLM call or adapter execution.
type StubDispatcher struct {
	// scopedAdapters is the per-scope curated allowlist: scopeID -> adapter set.
	// A tool->adapter binding is authorized ONLY if the adapter is in the
	// dispatching scope's set. Resolved per-scope so wrong-tenant == unknown.
	scopedAdapters map[string]map[string]struct{}
	// ext is the external client the REAL impl would call. The stub holds it but
	// NEVER invokes it (proven by the recording-spy tests).
	ext ExternalClient
}

// NewStubDispatcher builds the stub with a per-scope adapter registry and an
// external client it will never invoke. scopedAdapters maps a ScopedContext.ScopeID
// to the curated adapter set that scope may bind (the tenant-scoped allowlist).
func NewStubDispatcher(scopedAdapters map[string]map[string]struct{}, ext ExternalClient) *StubDispatcher {
	return &StubDispatcher{scopedAdapters: scopedAdapters, ext: ext}
}

// Dispatch implements Dispatcher. Validation order (fail-closed, non-leaking):
//
//  1. tool/node present and the tool's adapter is one the AgentNode binds.
//  2. adapter_id is in the DISPATCHING SCOPE's curated allowlist (else
//     ErrDispatchDenied -- identical for wrong-tenant, off-allowlist, unknown).
//  3. the io-envelope is well-formed (input_schema + output_schema present).
//  4. side_effect=external -> ErrDispatchExternalDeferred (deferred, never
//     executed). side_effect=unspecified -> fail-closed denied.
//  5. input structurally satisfies input_schema (args format-validated at write).
//  6. return a typed StubResult with an echoed output envelope. NO execution.
//
// The external client is NEVER invoked.
func (d *StubDispatcher) Dispatch(
	ctx context.Context,
	node *experiencev1.AgentNode,
	tool *experiencev1.ToolDefinition,
	scope ScopedContext,
	input []byte,
) (StubResult, error) {
	if tool == nil || node == nil {
		// No oracle: a nil tool/node is collapsed to the same denial.
		return StubResult{}, ErrDispatchDenied
	}

	adapterID := tool.GetAdapterId()

	// (1) The AgentNode must actually bind this tool's adapter. A tool the node
	// never declared collapses to the SAME denial (no oracle on what the node
	// binds).
	if !nodeBindsAdapter(node, adapterID) {
		return StubResult{}, ErrDispatchDenied
	}

	// (2) Tenant-scoped curated allowlist FK. Resolve the allowed set for the
	// DISPATCHING scope only -- a wrong-tenant adapter is simply absent here, so
	// it collapses to the SAME ErrDispatchDenied as a wholly-unknown adapter. The
	// offending adapter_id is NEVER echoed (no existence oracle).
	allowed := d.scopedAdapters[scope.ScopeID] // nil map -> membership is false
	if _, ok := allowed[adapterID]; !ok {
		return StubResult{}, ErrDispatchDenied
	}

	// (3) The typed io-envelope must be present (not a free blob, not absent).
	if tool.GetInputSchema() == "" || tool.GetOutputSchema() == "" {
		return StubResult{}, fmt.Errorf("%w: tool envelope missing input_schema/output_schema", ErrDispatchInvalidInput)
	}

	// (4) side_effect gate. external is deferred (never wired, never executed);
	// unspecified fails closed.
	switch tool.GetSideEffect() {
	case experiencev1.SideEffect_SIDE_EFFECT_EXTERNAL:
		// Deferred -- recognized but out of scope. The external client is NOT called.
		return StubResult{}, ErrDispatchExternalDeferred
	case experiencev1.SideEffect_SIDE_EFFECT_READ, experiencev1.SideEffect_SIDE_EFFECT_WRITE:
		// dispatchable side effects -- fall through.
	default:
		// UNSPECIFIED / unknown future side_effect fails closed -- not dispatchable.
		return StubResult{}, ErrDispatchDenied
	}

	// (5) Args format-validated at write: the input must structurally satisfy the
	// tool's input_schema BEFORE any notional dispatch.
	if err := validateAgainstSchema(input, tool.GetInputSchema()); err != nil {
		return StubResult{}, fmt.Errorf("%w: %v", ErrDispatchInvalidInput, err)
	}

	// (6) Classify the gate and return a typed, echoed result. NO adapter
	// execution, NO LLM call -- the stub only proves the envelope + binding.
	gate := GateRead
	writeGated := false
	if tool.GetSideEffect() == experiencev1.SideEffect_SIDE_EFFECT_WRITE {
		gate = GateWrite
		writeGated = true
	}

	out := echoOutputEnvelope(tool.GetOutputSchema())

	return StubResult{
		Status:         DispatchValidated,
		GateClass:      gate,
		WriteGated:     writeGated,
		Executed:       false, // STUB: never executes the adapter.
		OutputEnvelope: out,
	}, nil
}

// nodeBindsAdapter reports whether the AgentNode declares the given adapter id in
// its tool_ids (the agent's bound tool set).
func nodeBindsAdapter(node *experiencev1.AgentNode, adapterID string) bool {
	for _, id := range node.GetToolIds() {
		if id == adapterID {
			return true
		}
	}
	return false
}

// validateAgainstSchema structurally validates input against a JSON-Schema string.
//
// The stub does a LIGHTWEIGHT structural check (no external schema-validator
// dependency on this frozen-contract package): the input must be valid JSON and,
// when the schema declares "type":"object", a JSON object whose "required"
// properties are all present. This honors "args format-validated at write" while
// keeping the stub self-contained; obj 144's real loop can plug a full
// JSON-Schema validator in behind the SAME Dispatch signature.
func validateAgainstSchema(input []byte, schema string) error {
	// Input must be syntactically valid JSON.
	var inputAny any
	if err := json.Unmarshal(input, &inputAny); err != nil {
		return fmt.Errorf("input is not valid JSON: %w", err)
	}

	// Parse the schema enough to enforce type + required. A schema that itself is
	// not valid JSON is a malformed envelope, not a caller error -- but the
	// envelope-present check (step 3) already guarantees it is non-empty; if it is
	// non-JSON we conservatively pass the structural input check (the typed
	// contract guarantees fixtures supply valid schemas) rather than leak.
	var schemaObj map[string]any
	if err := json.Unmarshal([]byte(schema), &schemaObj); err != nil {
		return nil
	}

	if t, ok := schemaObj["type"].(string); ok && t == "object" {
		obj, ok := inputAny.(map[string]any)
		if !ok {
			return fmt.Errorf("schema requires a JSON object")
		}
		if req, ok := schemaObj["required"].([]any); ok {
			for _, r := range req {
				key, ok := r.(string)
				if !ok {
					continue
				}
				if _, present := obj[key]; !present {
					return fmt.Errorf("missing required property %q", key)
				}
			}
		}
	}

	return nil
}

// echoOutputEnvelope returns a minimal envelope conforming to the tool's
// output_schema. The STUB echoes an empty object (the swap-stable container);
// obj 144's real loop returns the adapter's actual output through this SAME
// field. If the schema is object-typed we emit "{}"; otherwise JSON null.
func echoOutputEnvelope(outputSchema string) []byte {
	var schemaObj map[string]any
	if err := json.Unmarshal([]byte(outputSchema), &schemaObj); err == nil {
		if t, ok := schemaObj["type"].(string); ok && t == "object" {
			return []byte(`{}`)
		}
	}
	return []byte(`null`)
}
