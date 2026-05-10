package aigateway

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"
)

// Document and generic generate endpoints exposed by the gateway.
const (
	DocumentsSummarizePath = "/v1/documents/summarize"
	DocumentsExtractPath   = "/v1/documents/extract"
	GeneratePath           = "/v1/generate"
)

// SummarizeDocumentRequest is the body of POST /v1/documents/summarize.
type SummarizeDocumentRequest struct {
	Text       string `json:"text"`
	DocumentID string `json:"document_id,omitempty"`
}

// SummarizeDocumentResponse holds the summarization output.
type SummarizeDocumentResponse struct {
	Result string `json:"result"`
	Hash   string `json:"hash,omitempty"`
}

// SummarizeDocument sends raw text to the document summarization endpoint
// and returns the summary string. documentID is optional but recommended for
// audit / cache-keying server-side.
func (c *Client) SummarizeDocument(ctx context.Context, text, documentID string) (string, error) {
	started := time.Now()
	var resp SummarizeDocumentResponse
	err := c.doJSON(ctx, http.MethodPost, DocumentsSummarizePath, SummarizeDocumentRequest{
		Text:       text,
		DocumentID: documentID,
	}, &resp)
	c.observe(ctx, OpDocumentSummarize, "", Usage{}, started, err)
	if err != nil {
		return "", err
	}
	return resp.Result, nil
}

// ExtractText uploads a binary document (typically a PDF) via multipart and
// returns the extracted text. filename is used for the Content-Disposition
// header. documentID is optional.
func (c *Client) ExtractText(ctx context.Context, fileBytes []byte, filename, documentID string) (string, error) {
	if filename == "" {
		filename = "document.bin"
	}
	started := time.Now()
	var resp SummarizeDocumentResponse
	err := c.doMultipart(ctx, DocumentsExtractPath, func(w *multipart.Writer) error {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			return fmt.Errorf("create file part: %w", err)
		}
		if _, err := fw.Write(fileBytes); err != nil {
			return fmt.Errorf("copy file: %w", err)
		}
		if documentID != "" {
			if err := w.WriteField("document_id", documentID); err != nil {
				return fmt.Errorf("write document_id field: %w", err)
			}
		}
		return nil
	}, &resp)
	c.observe(ctx, OpDocumentExtract, "", Usage{}, started, err)
	if err != nil {
		return "", err
	}
	return resp.Result, nil
}

// GenerateRequest is the body of POST /v1/generate — a "system + user
// prompt" surface used by justinforme/app/ai. Modeled separately from
// ChatRequest because the schema is flatter and the gateway returns a
// different response shape.
type GenerateRequest struct {
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	UserPrompt   string `json:"user_prompt"`
	MaxTokens    int    `json:"max_tokens"`
}

// GenerateUsage carries token-accounting figures for /v1/generate responses.
type GenerateUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// GenerateResponse is the body returned by /v1/generate. Flagged=true means
// the gateway's safety layer rejected the request; FlagReason carries the
// human-readable reason if present.
type GenerateResponse struct {
	Content    string        `json:"content"`
	Flagged    bool          `json:"flagged"`
	FlagReason string        `json:"flag_reason,omitempty"`
	Usage      GenerateUsage `json:"usage"`
}

// Generate sends a request to /v1/generate. This is the legacy "rough"
// surface used by justinforme; new code should prefer ChatCompletion.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if req.Model == "" {
		req.Model = c.DefaultModel()
	}
	started := time.Now()
	var resp GenerateResponse
	err := c.doJSON(ctx, http.MethodPost, GeneratePath, req, &resp)
	usage := Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	c.observe(ctx, OpGenerate, req.Model, usage, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

