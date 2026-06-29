// tooling.go -- TRD 140-07, must-haves #7..#10 (the LAST proto-writer).
//
// The 140-07 contract adds the TYPED tool/agent/signing/telemetry surface +
// every reserved-now-cheap seam to experience.v1, CLOSING the message shape
// (frozen for handler work after this TRD). This file is the COHERENCE logic for
// the TOOLING half of that surface -- the machine-checked rules the M0 validator
// (140-09) composes. It mirrors the typed-error vocabulary of navgraph.go /
// presentation.go (a typed Code + accumulated, never-panics errors).
//
// SECURITY MODEL (locked, memories feedback_allowlist_by_stored_value_injection +
// feedback_rest_authority_field_body_binding): a ToolDefinition binds ONLY to a
// curated allowlist of pre-built tenant-safe adapters. adapter_id is an allowlist
// FK -- NO arbitrary RPC/SQL target. ValidateTooling enforces three rules:
//
//  1. ToolingAdapterNotAllowed -- adapter_id is not in the caller-supplied
//     allowed-adapter set. NON-LEAKING: this is the SAME outcome (same code, same
//     message) whether the adapter is another tenant's private adapter or a
//     wholly-unknown one. Allow is decided ONLY against the caller's set, denying
//     a cross-tenant existence oracle (mirrors binding.ResolveScope's single
//     ErrScopeDenied sentinel + navgraph's CoherenceSurfaceNotEntitled).
//
//  2. ToolingMalformedEnvelope -- a tool's input_schema or output_schema is
//     blank. The typed JSON-Schema envelope must be PRESENT (the whole point of
//     the typed contract is that a tool is not a free config_json blob and not an
//     untyped passthrough). Empty == malformed.
//
//  3. ToolingExternalDeferred -- side_effect=EXTERNAL is representable but OUT OF
//     SCOPE (outbound webhooks deferred). Warn-level: the tool is allowed to
//     exist and round-trip, but flagged so the dispatcher does not wire it yet.
package experience

import (
	"fmt"

	experiencev1 "github.com/aocybersystems/eden-platform-go/gen/go/experience/v1"
)

// ToolingCode is the typed kind of a tooling-coherence finding. Stable codes let
// callers (tests, the builder ProblemsPanel, the 140-09 M0 validator) branch
// without string-matching -- mirroring navgraph.CoherenceCode.
type ToolingCode string

const (
	// ToolingAdapterNotAllowed -- adapter_id is not in the caller-supplied
	// allowed-adapter set. SAME code+message whether the adapter belongs to
	// another tenant or does not exist at all (no existence oracle).
	ToolingAdapterNotAllowed ToolingCode = "tooling.adapter_not_allowed"
	// ToolingMalformedEnvelope -- a tool's input_schema or output_schema is
	// blank (the typed JSON-Schema envelope must be present).
	ToolingMalformedEnvelope ToolingCode = "tooling.malformed_envelope"
	// ToolingExternalDeferred -- side_effect=EXTERNAL (outbound webhooks) is
	// representable but deferred/out-of-scope. Warn-level, not a hard reject.
	ToolingExternalDeferred ToolingCode = "tooling.external_deferred"
)

// ToolingError is one machine-checked tooling finding. AdapterID is the offending
// adapter ("" for tool-level findings that are not adapter-scoped). Message is
// INTENTIONALLY non-leaking for the not-allowed code: it never echoes the
// offending adapter id (which could itself be an oracle), so a B-scoped adapter
// and an unknown adapter yield byte-identical findings.
type ToolingError struct {
	Code      ToolingCode
	AdapterID string
	Message   string
}

func (e ToolingError) Error() string {
	if e.AdapterID != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.AdapterID)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidateTooling machine-checks a set of ToolDefinitions against the requesting
// scope's allowed-adapter set. allowedAdapters is the curated allowlist the
// caller's scope may bind -- a tool whose adapter_id is outside it is
// ToolingAdapterNotAllowed, identical whether it belongs to another tenant or
// does not exist (no oracle: the offending id is NOT echoed in the message).
//
// Rules checked (all accumulated, not short-circuited, so the builder shows every
// problem at once):
//  1. adapter_id is in allowedAdapters (else ToolingAdapterNotAllowed).
//  2. input_schema AND output_schema are non-blank (else ToolingMalformedEnvelope).
//  3. side_effect=EXTERNAL is warn-flagged ToolingExternalDeferred (deferred).
//
// A nil/empty tool set is vacuously coherent. AdapterID on the not-allowed
// finding is left "" so two different offending adapters yield byte-identical
// findings (the non-leak guarantee); the malformed/deferred findings DO carry the
// adapter id since those are not existence oracles (the adapter is allowed).
func ValidateTooling(tools []*experiencev1.ToolDefinition, allowedAdapters map[string]struct{}) []ToolingError {
	var errs []ToolingError

	for _, tool := range tools {
		if tool == nil {
			continue
		}
		adapterID := tool.GetAdapterId()

		// (1) allowlist FK -- non-leaking. Decide ONLY against the caller's set.
		if _, ok := allowedAdapters[adapterID]; !ok {
			errs = append(errs, ToolingError{
				Code: ToolingAdapterNotAllowed,
				// AdapterID + Message deliberately omit the offending id: a
				// B-scoped private adapter and a wholly-unknown adapter must be
				// indistinguishable (no existence oracle).
				Message: "adapter is not in the allowed adapter set",
			})
			// Skip the remaining (allowed-adapter-only) checks for a denied tool:
			// running them on a not-allowed adapter could itself leak (e.g. a
			// "malformed envelope" finding only on a real-but-foreign adapter).
			continue
		}

		// (2) typed io-envelope must be present (not a free blob, not absent).
		if tool.GetInputSchema() == "" || tool.GetOutputSchema() == "" {
			errs = append(errs, ToolingError{
				Code:      ToolingMalformedEnvelope,
				AdapterID: adapterID,
				Message:   "tool input_schema/output_schema must be a non-empty JSON-Schema envelope",
			})
		}

		// (3) external side-effect is deferred (representable, warn-flagged).
		if tool.GetSideEffect() == experiencev1.SideEffect_SIDE_EFFECT_EXTERNAL {
			errs = append(errs, ToolingError{
				Code:      ToolingExternalDeferred,
				AdapterID: adapterID,
				Message:   "side_effect=external (outbound) is deferred -- representable but not dispatchable yet",
			})
		}
	}

	return errs
}

// AgentEnvelopePreserved reports whether an AgentNode carries a non-empty typed
// io_envelope_schema -- the swap-stable seam the real LLM dispatcher (obj 144)
// plugs into. A node WITHOUT an envelope cannot guarantee an envelope-preserving
// stub->real swap, so callers (and 140-09) can assert this before wiring an agent.
func AgentEnvelopePreserved(node *experiencev1.AgentNode) bool {
	return node.GetIoEnvelopeSchema() != ""
}

// SigningRefsOnly reports whether a SigningSpec carries ONLY references (every
// CredentialRef has a non-empty ref + custody) and NO inline material. It is a
// guard the build pipeline (and 140-09) can assert before submitting a binary:
// signing material must live in custody, never in the proto. An empty/nil spec is
// trivially ref-only (no material present).
func SigningRefsOnly(spec *experiencev1.SigningSpec) bool {
	for _, ref := range spec.GetByPlatform() {
		if ref.GetRef() == "" || ref.GetCustody() == "" {
			return false
		}
	}
	return true
}
