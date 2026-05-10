package aigateway

import (
	"context"
	"net/http"
	"time"
)

// ModerationsPath is the gateway endpoint for content moderation.
const ModerationsPath = "/v1/moderations"

// ModerationRequest is the body of POST /v1/moderations.
type ModerationRequest struct {
	Input string `json:"input"`
	Model string `json:"model,omitempty"`
}

// ModerationResult holds the moderation output for a single input. Categories
// and CategoryScores are open-ended maps because the upstream gateway adds
// new categories over time and the existing forks each enumerated different
// subsets.
type ModerationResult struct {
	Flagged        bool               `json:"flagged"`
	Categories     map[string]bool    `json:"categories"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

// ModerationResponse is the body of POST /v1/moderations.
type ModerationResponse struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Results []ModerationResult `json:"results"`
}

// Moderate sends content to the moderation endpoint for safety classification.
//
// Returns a *ModerationResponse with one Result per input. The first Result
// is what most callers want; .Flagged signals whether the gateway considers
// the content to violate policy.
func (c *Client) Moderate(ctx context.Context, input string) (*ModerationResponse, error) {
	started := time.Now()
	var resp ModerationResponse
	err := c.doJSON(ctx, http.MethodPost, ModerationsPath, ModerationRequest{Input: input}, &resp)
	c.observe(ctx, OpModeration, resp.Model, Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
