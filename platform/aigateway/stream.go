package aigateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamDelta carries the partial content for a single SSE chunk.
type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// StreamChoice is one choice within a streaming chunk.
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// ChatCompletionChunk is a single SSE chunk from a streaming chat response.
type ChatCompletionChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// ChatCompletionStream sends a streaming chat completion request and returns
// two channels: chunks emits content-only string deltas as they arrive; errs
// receives at most one terminal error (or nothing if the stream completed
// cleanly). Both channels are closed when the stream ends.
//
// The implementation honors ctx cancellation: if ctx is canceled the
// underlying request is aborted and ctx.Err() is sent on errs before close.
func (c *Client) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan string, <-chan error) {
	chunks := make(chan string, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		if !c.IsConfigured() {
			errs <- ErrNotConfigured
			return
		}

		started := time.Now()
		if req.Model == "" {
			req.Model = c.DefaultModel()
		}
		req.Stream = true

		body, err := json.Marshal(req)
		if err != nil {
			err = fmt.Errorf("aigateway: marshal stream request: %w", err)
			errs <- err
			c.observe(ctx, OpChatStream, req.Model, Usage{}, started, err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+ChatPath, bytes.NewReader(body))
		if err != nil {
			err = fmt.Errorf("aigateway: build stream request: %w", err)
			errs <- err
			c.observe(ctx, OpChatStream, req.Model, Usage{}, started, err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}

		resp, err := c.http.Do(httpReq)
		if err != nil {
			err = fmt.Errorf("aigateway: stream request: %w", err)
			errs <- err
			c.observe(ctx, OpChatStream, req.Model, Usage{}, started, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body := readLimited(resp.Body, errorBodyLimit)
			var herr error
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				herr = fmt.Errorf("%w: %s", ErrUnauthorized, body)
			case http.StatusPaymentRequired:
				herr = fmt.Errorf("%w: %s", ErrBudgetExceeded, body)
			default:
				herr = &HTTPError{Status: resp.StatusCode, Body: body, Path: ChatPath}
			}
			errs <- herr
			c.observe(ctx, OpChatStream, req.Model, Usage{}, started, herr)
			return
		}

		reader := bufio.NewReader(resp.Body)
		var streamErr error
		for {
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				streamErr = fmt.Errorf("aigateway: read stream: %w", err)
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var chunk ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// Malformed chunks are skipped; the gateway occasionally
				// emits keepalive comments or partial frames during outages.
				continue
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content == "" {
					continue
				}
				select {
				case chunks <- choice.Delta.Content:
				case <-ctx.Done():
					streamErr = ctx.Err()
					goto done
				}
			}
		}
	done:
		if streamErr != nil {
			errs <- streamErr
		}
		c.observe(ctx, OpChatStream, req.Model, Usage{}, started, streamErr)
	}()

	return chunks, errs
}
