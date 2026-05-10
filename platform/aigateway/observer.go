package aigateway

import (
	"context"
	"time"
)

// Operation enumerates the call surfaces dispatched by the client. Used as
// the Operation field in Event values delivered to an Observer.
type Operation string

const (
	OpChatCompletion    Operation = "chat_completion"
	OpChatStream        Operation = "chat_stream"
	OpEmbedding         Operation = "embedding"
	OpModeration        Operation = "moderation"
	OpGuardrails        Operation = "guardrails_check"
	OpPIIRedact         Operation = "pii_redact"
	OpPIIRehydrate      Operation = "pii_rehydrate"
	OpImageGenerate     Operation = "image_generate"
	OpAudioTranscribe   Operation = "audio_transcribe"
	OpPromptRefine      Operation = "prompt_refine"
	OpDocumentSummarize Operation = "document_summarize"
	OpDocumentExtract   Operation = "document_extract"
	OpGenerate          Operation = "generate"
)

// Event is the structured record an Observer receives for each call.
//
// All fields are best-effort: PromptTokens/CompletionTokens/TotalTokens are
// populated from response usage when present (chat, embeddings, image gen),
// and zero otherwise (moderation, guardrails, doc ops). Err is non-nil on
// failure; Success mirrors Err == nil for ergonomic counter labeling.
type Event struct {
	Operation        Operation
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Duration         time.Duration
	Success          bool
	Err              error
}

// Observer receives an Event for every outbound gateway call. Implementers
// must be safe for concurrent use; the client may invoke Observe from
// multiple goroutines (notably the streaming path).
type Observer interface {
	Observe(ctx context.Context, ev Event)
}

// NoopObserver is the default Observer when none is provided to NewClient.
// Its Observe method does nothing.
type NoopObserver struct{}

// Observe implements Observer.
func (NoopObserver) Observe(context.Context, Event) {}

// observe delivers an Event to the configured observer. Always safe to call
// (Client.observer is set to NoopObserver by NewClient).
func (c *Client) observe(ctx context.Context, op Operation, model string, usage Usage, started time.Time, err error) {
	if c == nil || c.observer == nil {
		return
	}
	c.observer.Observe(ctx, Event{
		Operation:        op,
		Model:            model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Duration:         time.Since(started),
		Success:          err == nil,
		Err:              err,
	})
}
