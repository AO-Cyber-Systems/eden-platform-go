package aigateway

// ChatMessage is one message in a chat-completion conversation.
//
// Content is intentionally typed as any to support the two shapes the AOSentry
// gateway forwards to providers:
//
//   - string — a plain text message (the common case).
//   - []ContentPart — a multimodal message where individual parts can be
//     text or image_url (vision tasks).
//
// json.Marshal handles either shape correctly. On responses, the gateway
// always returns content as a string for chat completions, so callers can
// type-assert to string when reading from a Choice.
type ChatMessage struct {
	Role    string `json:"role"`              // "system" | "user" | "assistant"
	Content any    `json:"content"`           // string or []ContentPart
	Name    string `json:"name,omitempty"`    // optional speaker name (OpenAI extension)
}

// ContentPart is one entry inside a multimodal ChatMessage.Content array.
// Only the field corresponding to Type is populated.
type ContentPart struct {
	Type     string        `json:"type"`               // "text" | "image_url"
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// ImageURLPart carries the image URL (or data: URL) and detail hint for a
// vision content part.
type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto" | "low" | "high"
}

// ResponseFormat asks for structured output. Type "json_object" instructs
// the model to emit JSON; the gateway forwards the hint to providers that
// support it natively.
type ResponseFormat struct {
	Type string `json:"type"` // "text" | "json_object"
}

// ChatRequest is the body of POST /v1/chat/completions.
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	User           string          `json:"user,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// Usage holds token consumption returned in chat / embedding responses.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice is a single completion choice in a chat response.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// ChatResponse is the body of POST /v1/chat/completions for non-streaming
// requests.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// FirstContent returns the text content of the first choice, or "" if the
// response carried no choices. This matches the helper that several forks
// implemented inline.
func (r *ChatResponse) FirstContent() string {
	if r == nil || len(r.Choices) == 0 {
		return ""
	}
	switch v := r.Choices[0].Message.Content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		// Provider returned a non-string shape; surface it stringified.
		// Callers wanting structured access should walk Choices directly.
		return ""
	}
}

// Float64Ptr returns a pointer to v. Convenience for callers building
// ChatRequest values inline.
func Float64Ptr(v float64) *float64 { return &v }

// IntPtr returns a pointer to v. Convenience for callers building
// ChatRequest values inline.
func IntPtr(v int) *int { return &v }
