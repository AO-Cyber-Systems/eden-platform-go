package aigateway

import (
	"context"
	"net/http"
	"time"
)

// GuardrailsCheckPath is the gateway endpoint for the policy-driven safety
// classifier (distinct from /v1/moderations which is OpenAI-style).
const GuardrailsCheckPath = "/v1/guardrails/check"

// GuardrailsCheckRequest is the body of POST /v1/guardrails/check.
type GuardrailsCheckRequest struct {
	Input    string            `json:"input"`
	Policy   string            `json:"policy,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	// ExtraHeaders are applied as HTTP request headers on every outbound call
	// to the AOCore gateway. They are never serialised into the JSON body.
	// Ranging over a nil map is safe (zero iterations).
	ExtraHeaders map[string]string `json:"-"`
}

// extraHeaderMap satisfies the extraHeaders interface used by transport.doJSON
// to thread per-request HTTP headers into do() without changing its signature.
func (r GuardrailsCheckRequest) extraHeaderMap() map[string]string { return r.ExtraHeaders }

// GuardrailsCheckResponse is the body returned by the guardrails endpoint.
type GuardrailsCheckResponse struct {
	Safe              bool     `json:"safe"`
	FlaggedCategories []string `json:"flagged_categories,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	Confidence        float64  `json:"confidence"`
}

// CheckGuardrails sends content through AOSentry's policy-aware safety check.
//
// policy is a named policy id configured server-side (e.g. "family-safe",
// "enterprise-strict"); empty string applies the gateway's default. metadata
// is forwarded verbatim and used by the gateway for audit logging.
//
// CheckGuardrails delegates to CheckGuardrailsWithHeaders with nil extraHeaders
// (backward-compatible; existing callers are unaffected).
func (c *Client) CheckGuardrails(ctx context.Context, input, policy string, metadata map[string]string) (*GuardrailsCheckResponse, error) {
	return c.CheckGuardrailsWithHeaders(ctx, input, policy, metadata, nil)
}

// CheckGuardrailsWithHeaders is identical to CheckGuardrails but also accepts
// extraHeaders, which are applied as HTTP request headers on the outbound call
// to the AOCore gateway (e.g. X-Household-ID, X-Member-ID, X-Child-Mode).
// extraHeaders is never serialised into the JSON body. Passing nil is safe and
// produces identical behavior to CheckGuardrails.
func (c *Client) CheckGuardrailsWithHeaders(ctx context.Context, input, policy string, metadata, extraHeaders map[string]string) (*GuardrailsCheckResponse, error) {
	started := time.Now()
	var resp GuardrailsCheckResponse
	err := c.doJSON(ctx, http.MethodPost, GuardrailsCheckPath, GuardrailsCheckRequest{
		Input:        input,
		Policy:       policy,
		Metadata:     metadata,
		ExtraHeaders: extraHeaders,
	}, &resp)
	c.observe(ctx, OpGuardrails, "", Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
