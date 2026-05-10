package aigateway

import (
	"context"
	"net/http"
	"time"
)

// ChatPath is the gateway endpoint for chat completions.
const ChatPath = "/v1/chat/completions"

// ChatCompletion sends a non-streaming chat completion request and returns
// the decoded response.
//
// If req.Model is empty the configured DefaultModel is used. The Stream
// field is forced to false; use ChatCompletionStream for streaming.
//
// req.User, when non-empty, is forwarded verbatim and used by the gateway
// for per-user spend attribution; consumers that want per-user spend logs
// should populate it (the seven forks variously stringified user IDs into
// this field).
func (c *Client) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.DefaultModel()
	}
	req.Stream = false

	started := time.Now()
	var resp ChatResponse
	err := c.doJSON(ctx, http.MethodPost, ChatPath, req, &resp)
	if err != nil {
		c.observe(ctx, OpChatCompletion, req.Model, Usage{}, started, err)
		return nil, err
	}
	c.observe(ctx, OpChatCompletion, resp.Model, resp.Usage, started, nil)
	return &resp, nil
}
