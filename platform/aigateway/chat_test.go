package aigateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestChatCompletionDefaultsModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Model != defaultDefaultModel {
			t.Errorf("Model=%q want %q", body.Model, defaultDefaultModel)
		}
		if body.Stream {
			t.Error("Stream should be false for ChatCompletion")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer srv.Close()

	obs := &captureObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	resp, err := c.ChatCompletion(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.FirstContent() != "hi" {
		t.Errorf("FirstContent=%q want hi", resp.FirstContent())
	}
	if len(obs.events) != 1 || obs.events[0].Operation != OpChatCompletion {
		t.Errorf("observer events=%+v", obs.events)
	}
	if obs.events[0].TotalTokens != 4 {
		t.Errorf("observer TotalTokens=%d", obs.events[0].TotalTokens)
	}
	if !obs.events[0].Success {
		t.Errorf("observer success=false")
	}
}

func TestChatCompletionVisionContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode raw to inspect the multipart content shape.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		messages := body["messages"].([]any)
		first := messages[0].(map[string]any)
		content := first["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("content parts=%d want 2", len(content))
		}
		if content[0].(map[string]any)["type"] != "text" {
			t.Errorf("part 0 type=%v", content[0])
		}
		if content[1].(map[string]any)["type"] != "image_url" {
			t.Errorf("part 1 type=%v", content[1])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"a cat"}}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.ChatCompletion(context.Background(), ChatRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{{
			Role: "user",
			Content: []ContentPart{
				{Type: "text", Text: "what's in this image?"},
				{Type: "image_url", ImageURL: &ImageURLPart{URL: "https://example.com/cat.jpg", Detail: "auto"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.FirstContent() != "a cat" {
		t.Errorf("content=%q", resp.FirstContent())
	}
}

func TestChatCompletionJSONMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("ResponseFormat=%+v", req.ResponseFormat)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"{\"k\":1}"}}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ChatCompletion(context.Background(), ChatRequest{
		Messages:       []ChatMessage{{Role: "user", Content: "give me JSON"}},
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}

func TestChatCompletionUserField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.User != "user-42" {
			t.Errorf("User=%q want user-42", req.User)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ChatCompletion(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		User:     "user-42",
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
}

func TestFirstContentVariants(t *testing.T) {
	cases := []struct {
		name string
		resp *ChatResponse
		want string
	}{
		{"nil", nil, ""},
		{"empty choices", &ChatResponse{}, ""},
		{"string content", &ChatResponse{Choices: []Choice{{Message: ChatMessage{Content: "hello"}}}}, "hello"},
		{"nil content", &ChatResponse{Choices: []Choice{{Message: ChatMessage{Content: nil}}}}, ""},
		{"non-string content", &ChatResponse{Choices: []Choice{{Message: ChatMessage{Content: []ContentPart{{Type: "text", Text: "x"}}}}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.resp.FirstContent(); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestChatCompletionObservesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	obs := &captureObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	_, err := c.ChatCompletion(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(obs.events) != 1 || obs.events[0].Success {
		t.Errorf("observer should have recorded a failure event: %+v", obs.events)
	}
	if obs.events[0].Err == nil {
		t.Error("observer Err should be set")
	}
}

// TestChatRequestMarshalRoundTrip verifies our types serialize cleanly.
func TestChatRequestMarshalRoundTrip(t *testing.T) {
	in := ChatRequest{
		Model: "x",
		Messages: []ChatMessage{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: "hi"},
		},
		Temperature: Float64Ptr(0.5),
		MaxTokens:   IntPtr(100),
		User:        "u1",
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ChatRequest
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*in.Temperature, *out.Temperature) {
		t.Errorf("temperature mismatch")
	}
	if !reflect.DeepEqual(*in.MaxTokens, *out.MaxTokens) {
		t.Errorf("max_tokens mismatch")
	}
	if out.User != "u1" {
		t.Errorf("user=%q", out.User)
	}
}
