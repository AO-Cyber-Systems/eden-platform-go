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
}

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
func (c *Client) CheckGuardrails(ctx context.Context, input, policy string, metadata map[string]string) (*GuardrailsCheckResponse, error) {
	started := time.Now()
	var resp GuardrailsCheckResponse
	err := c.doJSON(ctx, http.MethodPost, GuardrailsCheckPath, GuardrailsCheckRequest{
		Input:    input,
		Policy:   policy,
		Metadata: metadata,
	}, &resp)
	c.observe(ctx, OpGuardrails, "", Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
