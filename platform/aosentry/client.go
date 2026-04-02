package aosentry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTimeout = 90 * time.Second

// Client is an HTTP client for the AOSentry LLM gateway.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// Option configures the client.
type Option func(*Client)

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates an AOSentry client.
func NewClient(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chat sends a non-streaming chat completion request.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("aosentry: marshal request: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, MapHTTPError(resp.StatusCode, respBody)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("aosentry: decode response: %w", err)
	}

	return &chatResp, nil
}

// Embeddings generates vector embeddings for the given texts.
func (c *Client) Embeddings(ctx context.Context, req EmbeddingsRequest) (*EmbeddingsResponse, error) {
	if req.Model == "" {
		req.Model = "text-embedding-3-small"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("aosentry: marshal embeddings request: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/v1/embeddings", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, MapHTTPError(resp.StatusCode, respBody)
	}

	var embResp EmbeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("aosentry: decode embeddings response: %w", err)
	}

	return &embResp, nil
}

// ListSkills fetches the skill catalog from AOSentry.
func (c *Client) ListSkills(ctx context.Context) ([]json.RawMessage, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/skills", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, MapHTTPError(resp.StatusCode, respBody)
	}

	var result struct {
		Skills []json.RawMessage `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("aosentry: decode skills: %w", err)
	}

	return result.Skills, nil
}

// SubmitFeedback sends explicit user feedback for a request.
func (c *Client) SubmitFeedback(ctx context.Context, requestID, feedbackType string) error {
	body, err := json.Marshal(map[string]string{
		"request_id":    requestID,
		"feedback_type": feedbackType,
	})
	if err != nil {
		return fmt.Errorf("aosentry: marshal feedback: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/v1/intelligence/feedback", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return MapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("aosentry: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &TimeoutError{Err: Error{Message: "request timed out", StatusCode: 0, Cause: err}}
		}
		return nil, fmt.Errorf("aosentry: request failed: %w", err)
	}

	return resp, nil
}
