// dispatcher_test.go -- TRD 140-10, must-have #7 (runtime half): the STUB tool
// dispatcher. RED FIRST.
//
// The locked 140 decision ships a STUB dispatcher now (no LLM, no real adapter
// execution) and defers the real loop to obj 144. This test pins the contract
// that obj 144 must preserve when it swaps the real impl in behind the SAME
// Dispatcher port:
//
//   - Dispatch validates a well-formed AgentNode + ToolDefinition io-envelope and
//     returns a typed StubResult WITHOUT executing the adapter or calling an LLM.
//   - The adapter_id is resolved against the SCOPE's curated allowlist (a
//     tool->adapter binding is tenant-scoped). An off-allowlist adapter AND a
//     wrong-tenant adapter collapse to the SAME non-leaking ErrDispatchDenied
//     (no existence oracle; adapter_id never echoed) -- the
//     second-order-injection / allowlist-by-stored-value lesson.
//   - A WRITE-side_effect tool is DISTINGUISHABLE/gated vs a READ (the stub
//     records the gate class even though it executes nothing).
//   - side_effect=external is rejected/deferred (webhooks out of scope), never
//     executed.
//   - An input that violates input_schema is rejected (args format-validated at
//     write).
//   - The Dispatcher INTERFACE is satisfiable by a SECOND impl (compile-time port
//     test) -- proving the envelope-preserving stub->real swap seam.
//   - NO LLM / network call happens: a recording ExternalClient handed to the
//     stub is asserted to have been invoked ZERO times across every path.
//
// Fixtures only: NewTool / NewAgentNode / NewAgentNodeForTool / WithAdapter /
// WithSchemas / WithSideEffect + ScopedAdapters / AllowedAdaptersForScope +
// the binding-layer ScopedContext + WrongTenantID. No hand-built proto literals.
package experience_test

import (
	"context"
	"errors"
	"testing"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
	"github.com/aocybersystems/eden-platform-go/platform/experience"
	"github.com/aocybersystems/eden-platform-go/platform/experience/fixtures"
)

// recordingExternalClient is the spy proving the STUB never reaches out: every
// dispatch path must leave Calls at 0 (no LLM, no adapter execution, no network).
type recordingExternalClient struct {
	Calls int
}

func (r *recordingExternalClient) Invoke(ctx context.Context, adapterID string, payload []byte) ([]byte, error) {
	r.Calls++
	return nil, nil
}

// defaultScope is a ScopedContext resolved to the DefaultTenantID company scope --
// the scope under which the curated default-tenant adapters are bindable.
func defaultScope() experience.ScopedContext {
	return experience.ScopedContext{
		Subject:   "aoid-subject-fixture",
		Authority: experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		ScopeID:   fixtures.DefaultTenantID,
	}
}

// wrongScope is a ScopedContext resolved to WrongTenantID -- a DIFFERENT tenant
// whose adapter set is disjoint from the default-tenant adapters the happy-path
// tools bind. Dispatching a default-tenant tool under this scope must be denied
// identically to an unknown adapter.
func wrongScope() experience.ScopedContext {
	return experience.ScopedContext{
		Subject:   "aoid-subject-fixture",
		Authority: experiencev1.ScopeAuthority_SCOPE_AUTHORITY_COMPANY,
		ScopeID:   fixtures.WrongTenantID,
	}
}

func newStub(ext *recordingExternalClient) experience.Dispatcher {
	return experience.NewStubDispatcher(fixtures.ScopedAdapters(), ext)
}

// --- Happy path ------------------------------------------------------------

func TestStubDispatch_WellFormedReadTool_ValidatedNoSideEffect(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	tool := fixtures.NewTool() // READ tool, allowlisted adapter, typed envelopes
	node := fixtures.NewAgentNodeForTool(tool)
	input := []byte(`{"query":"acme"}`)

	res, err := d.Dispatch(context.Background(), node, tool, defaultScope(), input)
	if err != nil {
		t.Fatalf("happy-path dispatch: unexpected err: %v", err)
	}
	if res.Status != experience.DispatchValidated {
		t.Fatalf("status: want DispatchValidated, got %q", res.Status)
	}
	if res.GateClass != experience.GateRead {
		t.Fatalf("gate class: want GateRead, got %q", res.GateClass)
	}
	if res.Executed {
		t.Fatalf("stub must NOT execute the adapter: Executed=true")
	}
	if len(res.OutputEnvelope) == 0 {
		t.Fatalf("stub must return a validated/echoed output envelope conforming to output_schema")
	}
	if ext.Calls != 0 {
		t.Fatalf("STUB must not call any external client/LLM: Calls=%d", ext.Calls)
	}
}

func TestStubDispatch_WriteTool_GatedDistinctFromRead(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	// A WRITE tool (create_invoice adapter) is gated differently from a READ:
	// the stub records GateWrite and flags WriteGated, but still executes nothing.
	tool := fixtures.NewTool(
		fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
		fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
	)
	node := fixtures.NewAgentNodeForTool(tool)
	input := []byte(`{"query":"acme"}`)

	res, err := d.Dispatch(context.Background(), node, tool, defaultScope(), input)
	if err != nil {
		t.Fatalf("write dispatch: unexpected err: %v", err)
	}
	if res.GateClass != experience.GateWrite {
		t.Fatalf("write tool must be gated GateWrite, got %q", res.GateClass)
	}
	if !res.WriteGated {
		t.Fatalf("write tool must record WriteGated=true (gated differently from read)")
	}
	if res.Executed {
		t.Fatalf("stub must NOT execute the write adapter")
	}
	if ext.Calls != 0 {
		t.Fatalf("STUB must not call any external client/LLM: Calls=%d", ext.Calls)
	}

	// And a READ tool must NOT be write-gated -- the two are distinguishable.
	readRes, err := d.Dispatch(context.Background(), node /*reuse*/, fixtures.NewTool(), defaultScope(), input)
	if err != nil {
		t.Fatalf("read dispatch for distinctness: %v", err)
	}
	if readRes.WriteGated || readRes.GateClass == experience.GateWrite {
		t.Fatalf("read tool must not be write-gated; got gate=%q writeGated=%v", readRes.GateClass, readRes.WriteGated)
	}
}

// --- Edge: off-allowlist adapter -------------------------------------------

func TestStubDispatch_OffAllowlistAdapter_Denied(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	tool := fixtures.NewTool(fixtures.WithAdapter("adapter.arbitrary_rpc_sql"))
	node := fixtures.NewAgentNodeForTool(tool)

	_, err := d.Dispatch(context.Background(), node, tool, defaultScope(), []byte(`{"query":"x"}`))
	if !errors.Is(err, experience.ErrDispatchDenied) {
		t.Fatalf("off-allowlist adapter must be ErrDispatchDenied, got %v", err)
	}
	// Non-leak: the offending adapter_id must NEVER appear in the error text.
	if err != nil && containsAdapterID(err.Error(), "adapter.arbitrary_rpc_sql") {
		t.Fatalf("error leaks the offending adapter_id (existence oracle): %q", err.Error())
	}
	if ext.Calls != 0 {
		t.Fatalf("STUB must not call any external client/LLM: Calls=%d", ext.Calls)
	}
}

// --- Edge: input violates input_schema -------------------------------------

func TestStubDispatch_InputViolatesSchema_Rejected(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	tool := fixtures.NewTool() // input_schema requires an object with "query"
	node := fixtures.NewAgentNodeForTool(tool)

	// Not even valid JSON -- args format-validated at write must reject it.
	_, err := d.Dispatch(context.Background(), node, tool, defaultScope(), []byte(`not-json`))
	if err == nil {
		t.Fatalf("malformed input must be rejected (args format-validated at write)")
	}
	if errors.Is(err, experience.ErrDispatchDenied) {
		t.Fatalf("a schema violation is NOT a scope denial -- it must be a distinct validation error, got ErrDispatchDenied")
	}
	if ext.Calls != 0 {
		t.Fatalf("STUB must not call any external client/LLM: Calls=%d", ext.Calls)
	}
}

// --- Edge: external side-effect deferred -----------------------------------

func TestStubDispatch_ExternalSideEffect_DeferredNotExecuted(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	tool := fixtures.NewTool(
		fixtures.WithAdapter(fixtures.AdapterSendWebhook),
		fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_EXTERNAL),
	)
	node := fixtures.NewAgentNodeForTool(tool)

	_, err := d.Dispatch(context.Background(), node, tool, defaultScope(), []byte(`{"query":"x"}`))
	if !errors.Is(err, experience.ErrDispatchExternalDeferred) {
		t.Fatalf("external side-effect must be ErrDispatchExternalDeferred (out of scope), got %v", err)
	}
	if ext.Calls != 0 {
		t.Fatalf("external adapter must NOT be invoked (webhooks deferred): Calls=%d", ext.Calls)
	}
}

// --- Edge: tool not bound by the AgentNode ---------------------------------

func TestStubDispatch_ToolNotBoundByNode_Denied(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	// Node binds the search adapter; we dispatch the create_invoice tool the node
	// never declared -> denied (collapsed, no oracle).
	boundTool := fixtures.NewTool()
	node := fixtures.NewAgentNodeForTool(boundTool)
	unboundTool := fixtures.NewTool(
		fixtures.WithAdapter(fixtures.AdapterCreateInvoice),
		fixtures.WithSideEffect(experiencev1.SideEffect_SIDE_EFFECT_WRITE),
	)

	_, err := d.Dispatch(context.Background(), node, unboundTool, defaultScope(), []byte(`{"query":"x"}`))
	if !errors.Is(err, experience.ErrDispatchDenied) {
		t.Fatalf("a tool the node does not bind must be ErrDispatchDenied, got %v", err)
	}
}

// --- Failure / wrong-tenant (PRIME) ----------------------------------------

func TestStubDispatch_WrongTenant_SameDenialAsUnknown(t *testing.T) {
	ext := &recordingExternalClient{}
	d := newStub(ext)

	tool := fixtures.NewTool() // bound to a DEFAULT-tenant adapter
	node := fixtures.NewAgentNodeForTool(tool)
	input := []byte(`{"query":"acme"}`)

	// Same tool+node, but dispatched under the WRONG tenant's scope. The adapter
	// is not in WrongTenantID's set -> ErrDispatchDenied, identical to off-allowlist.
	_, wrongErr := d.Dispatch(context.Background(), node, tool, wrongScope(), input)
	if !errors.Is(wrongErr, experience.ErrDispatchDenied) {
		t.Fatalf("wrong-tenant dispatch must be ErrDispatchDenied, got %v", wrongErr)
	}

	// An off-allowlist (unknown) adapter under the RIGHT scope.
	unknownTool := fixtures.NewTool(fixtures.WithAdapter("adapter.totally_unknown"))
	unknownNode := fixtures.NewAgentNodeForTool(unknownTool)
	_, unknownErr := d.Dispatch(context.Background(), unknownNode, unknownTool, defaultScope(), input)
	if !errors.Is(unknownErr, experience.ErrDispatchDenied) {
		t.Fatalf("unknown adapter must be ErrDispatchDenied, got %v", unknownErr)
	}

	// NON-LEAK: byte-identical error text. Wrong-tenant and unknown-adapter must
	// be indistinguishable (no existence oracle).
	if wrongErr.Error() != unknownErr.Error() {
		t.Fatalf("wrong-tenant and unknown-adapter denials must be byte-identical (no oracle):\n  wrong:   %q\n  unknown: %q",
			wrongErr.Error(), unknownErr.Error())
	}
	if containsAdapterID(wrongErr.Error(), fixtures.AdapterSearchContacts) {
		t.Fatalf("wrong-tenant denial leaks the bound adapter_id: %q", wrongErr.Error())
	}
	if ext.Calls != 0 {
		t.Fatalf("STUB must not call any external client/LLM: Calls=%d", ext.Calls)
	}
}

// --- Port-swap seam (compile-time) -----------------------------------------

// noopDispatcher is a SECOND impl of the Dispatcher port, proving the io-envelope
// seam is swappable: obj 144 plugs the real LLM loop in behind this SAME
// interface with NO contract change. It is the envelope-preserving-swap proof.
type noopDispatcher struct{}

func (noopDispatcher) Dispatch(
	ctx context.Context,
	node *experiencev1.AgentNode,
	tool *experiencev1.ToolDefinition,
	scope experience.ScopedContext,
	input []byte,
) (experience.StubResult, error) {
	return experience.StubResult{Status: experience.DispatchValidated}, nil
}

func TestDispatcherPort_SatisfiedBySecondImpl(t *testing.T) {
	// Compile-time + runtime proof: both the stub and a second impl satisfy the
	// SAME port. If the interface signature changes, this fails to compile.
	var impls = []experience.Dispatcher{
		experience.NewStubDispatcher(fixtures.ScopedAdapters(), &recordingExternalClient{}),
		noopDispatcher{},
	}
	for _, d := range impls {
		if d == nil {
			t.Fatalf("nil Dispatcher impl")
		}
	}
}

// containsAdapterID reports whether s contains sub -- used to assert the offending
// adapter id never leaks into an error string.
func containsAdapterID(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
