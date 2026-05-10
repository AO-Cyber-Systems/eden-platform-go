package aigateway

import (
	"context"
	"net/http"
	"time"
)

// PromptsRefinePath is the gateway endpoint for prompt refinement.
const PromptsRefinePath = "/v1/prompts/refine"

// RefineRequest is the body of POST /v1/prompts/refine. The endpoint accepts
// a rough natural-language prompt and returns a polished version optimized
// for the named domain + intent.
type RefineRequest struct {
	Domain      string         `json:"domain"`
	Intent      string         `json:"intent"`
	Context     map[string]any `json:"context,omitempty"`
	RoughPrompt string         `json:"rough_prompt"`
}

// RefineResponse is the body returned by the refine endpoint.
type RefineResponse struct {
	RefinedPrompt string `json:"refined_prompt"`
	ModelHint     string `json:"model_hint"`
	CacheTTLHours int    `json:"cache_ttl_hours"`
}

// RefinePrompt asks the gateway's prompt-refinement service to polish a rough
// prompt for a specific domain + intent.
func (c *Client) RefinePrompt(ctx context.Context, req RefineRequest) (*RefineResponse, error) {
	started := time.Now()
	var resp RefineResponse
	err := c.doJSON(ctx, http.MethodPost, PromptsRefinePath, req, &resp)
	c.observe(ctx, OpPromptRefine, resp.ModelHint, Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
