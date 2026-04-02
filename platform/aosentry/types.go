// Package aosentry provides a shared HTTP client for the AOSentry LLM gateway.
// Used by all apps (AODex, AOHealth, Trades) to call AOSentry with intelligence
// context metadata.
package aosentry

import "encoding/json"

// Message is a chat message.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// TextMessage creates a Message with string content.
func TextMessage(role, content string) Message {
	c, _ := json.Marshal(content)
	return Message{Role: role, Content: c}
}

// ChatRequest is a chat completion request to AOSentry.
type ChatRequest struct {
	Messages         []Message       `json:"messages"`
	Model            string          `json:"model"`
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	Tools            json.RawMessage `json:"tools,omitempty"`
	ToolChoice       json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat   json.RawMessage `json:"response_format,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Stop             []string        `json:"stop,omitempty"`
	User             string          `json:"user,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// ChatResponse is the response from a non-streaming chat completion.
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"`
}

// ChatChoice is a single completion choice.
type ChatChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ChatChunk is a streaming chunk.
type ChatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice is a streaming choice delta.
type ChunkChoice struct {
	Index        int      `json:"index"`
	Delta        *Message `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// EmbeddingsRequest is the request payload for vector embeddings.
type EmbeddingsRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// EmbeddingsResponse is the response from the embeddings endpoint.
type EmbeddingsResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`
}

// EmbeddingData contains a single embedding vector.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// FirstContent returns the text content of the first choice, or empty string.
func (r *ChatResponse) FirstContent() string {
	if r == nil || len(r.Choices) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(r.Choices[0].Message.Content, &s); err == nil {
		return s
	}
	return string(r.Choices[0].Message.Content)
}
