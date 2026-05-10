package aigateway

import (
	"context"
	"net/http"
	"time"
)

// PII endpoints on the gateway.
const (
	PIIRedactPath    = "/v1/guardrails/pii/redact"
	PIIRehydratePath = "/v1/guardrails/pii/rehydrate"
)

// PIIEntity describes a detected PII entity. Returned by RedactPII and
// supplied back to RehydratePII for round-trip restoration.
type PIIEntity struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

// PIIRedactRequest is the body of POST /v1/guardrails/pii/redact.
type PIIRedactRequest struct {
	Input       string   `json:"input"`
	EntityTypes []string `json:"entity_types,omitempty"`
}

// PIIRedactResponse holds the redaction result.
type PIIRedactResponse struct {
	RedactedText string      `json:"redacted_text"`
	Entities     []PIIEntity `json:"entities,omitempty"`
	HasPII       bool        `json:"has_pii"`
}

// PIIRehydrateRequest is the body of POST /v1/guardrails/pii/rehydrate.
type PIIRehydrateRequest struct {
	RedactedText string      `json:"redacted_text"`
	Entities     []PIIEntity `json:"entities"`
}

// PIIRehydrateResponse is the body returned by the rehydrate endpoint.
type PIIRehydrateResponse struct {
	OriginalText string `json:"original_text"`
}

// RedactPII sends content through the PII redaction endpoint. entityTypes
// optionally constrains which entity types are detected (empty = all types
// supported by the configured policy).
func (c *Client) RedactPII(ctx context.Context, input string, entityTypes []string) (*PIIRedactResponse, error) {
	started := time.Now()
	var resp PIIRedactResponse
	err := c.doJSON(ctx, http.MethodPost, PIIRedactPath, PIIRedactRequest{
		Input:       input,
		EntityTypes: entityTypes,
	}, &resp)
	c.observe(ctx, OpPIIRedact, "", Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// RehydratePII restores original text from a previously redacted version.
// entities must be the entities slice that came back from RedactPII (or an
// equivalent persisted copy).
func (c *Client) RehydratePII(ctx context.Context, redactedText string, entities []PIIEntity) (*PIIRehydrateResponse, error) {
	started := time.Now()
	var resp PIIRehydrateResponse
	err := c.doJSON(ctx, http.MethodPost, PIIRehydratePath, PIIRehydrateRequest{
		RedactedText: redactedText,
		Entities:     entities,
	}, &resp)
	c.observe(ctx, OpPIIRehydrate, "", Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
