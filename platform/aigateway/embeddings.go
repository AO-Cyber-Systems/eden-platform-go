package aigateway

import (
	"context"
	"net/http"
	"time"
)

// EmbeddingsPath is the gateway endpoint for vector embeddings.
const EmbeddingsPath = "/v1/embeddings"

// DefaultEmbeddingModel is used by EmbedText when the caller passes "".
const DefaultEmbeddingModel = "text-embedding-3-small"

// EmbeddingRequest is the body of POST /v1/embeddings.
//
// Input may be a string (single text) or []string (batch). The gateway
// forwards the union shape to the underlying provider.
type EmbeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
	User  string `json:"user,omitempty"`
}

// EmbeddingData is one embedding vector in an EmbeddingResponse.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// EmbeddingResponse is the body of POST /v1/embeddings.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// Embeddings sends an embeddings request and returns the decoded response.
//
// If req.Model is empty, DefaultEmbeddingModel ("text-embedding-3-small") is
// used (this is the same default the existing forks settled on).
func (c *Client) Embeddings(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	if req.Model == "" {
		req.Model = DefaultEmbeddingModel
	}
	started := time.Now()
	var resp EmbeddingResponse
	if err := c.doJSON(ctx, http.MethodPost, EmbeddingsPath, req, &resp); err != nil {
		c.observe(ctx, OpEmbedding, req.Model, Usage{}, started, err)
		return nil, err
	}
	c.observe(ctx, OpEmbedding, resp.Model, resp.Usage, started, nil)
	return &resp, nil
}

// EmbedText is a convenience wrapper that returns the first embedding as a
// []float32 slice — the canonical shape for pgvector storage and the form
// every fork that uses embeddings ultimately wanted.
//
// model may be empty to use DefaultEmbeddingModel.
func (c *Client) EmbedText(ctx context.Context, text, model string) ([]float32, error) {
	resp, err := c.Embeddings(ctx, EmbeddingRequest{Model: model, Input: text})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, &HTTPError{Status: http.StatusInternalServerError, Body: "no embedding data", Path: EmbeddingsPath}
	}
	raw := resp.Data[0].Embedding
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
	}
	return out, nil
}
