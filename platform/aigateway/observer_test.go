package aigateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// concurrentObserver verifies thread-safety expectations: Observe may be
// invoked from multiple goroutines (notably from the streaming path).
type concurrentObserver struct {
	mu     sync.Mutex
	events []Event
}

func (c *concurrentObserver) Observe(_ context.Context, ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *concurrentObserver) snapshot() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

func TestObserverInvokedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"ok"}}],"usage":{"total_tokens":42}}`))
	}))
	defer srv.Close()

	obs := &concurrentObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	if _, err := c.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	events := obs.snapshot()
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	ev := events[0]
	if ev.Operation != OpChatCompletion {
		t.Errorf("Operation=%s", ev.Operation)
	}
	if ev.Model != "m" {
		t.Errorf("Model=%q", ev.Model)
	}
	if ev.TotalTokens != 42 {
		t.Errorf("TotalTokens=%d", ev.TotalTokens)
	}
	if !ev.Success || ev.Err != nil {
		t.Errorf("expected success, got %+v", ev)
	}
	if ev.Duration <= 0 {
		t.Errorf("Duration=%s should be positive", ev.Duration)
	}
}

func TestObserverInvokedOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	obs := &concurrentObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	_, err := c.Embeddings(context.Background(), EmbeddingRequest{Input: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	events := obs.snapshot()
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	if events[0].Success {
		t.Error("Success should be false")
	}
	if !errors.Is(events[0].Err, ErrUnauthorized) {
		t.Errorf("Err=%v want ErrUnauthorized", events[0].Err)
	}
}

func TestNoopObserverDefault(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.observer.(NoopObserver); !ok {
		t.Errorf("default observer = %T want NoopObserver", c.observer)
	}
	// Calling Observe must not panic.
	c.observer.Observe(context.Background(), Event{Operation: OpChatCompletion})
}

func TestObserverOnEveryEndpoint(t *testing.T) {
	// Each endpoint must dispatch exactly one Event per call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ChatPath:
			_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"ok"}}],"usage":{}}`))
		case EmbeddingsPath:
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
		case ModerationsPath:
			_, _ = w.Write([]byte(`{"results":[{"flagged":false,"categories":{},"category_scores":{}}]}`))
		case GuardrailsCheckPath:
			_, _ = w.Write([]byte(`{"safe":true}`))
		case PIIRedactPath:
			_, _ = w.Write([]byte(`{"redacted_text":"x","entities":[]}`))
		case PIIRehydratePath:
			_, _ = w.Write([]byte(`{"original_text":"x"}`))
		case ImagesGeneratePath:
			_, _ = w.Write([]byte(`{"data":[{"url":"u"}]}`))
		case PromptsRefinePath:
			_, _ = w.Write([]byte(`{"refined_prompt":"r","model_hint":"m"}`))
		case DocumentsSummarizePath:
			_, _ = w.Write([]byte(`{"result":"s"}`))
		case GeneratePath:
			_, _ = w.Write([]byte(`{"content":"g","usage":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	obs := &concurrentObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	ctx := context.Background()
	_, _ = c.ChatCompletion(ctx, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	_, _ = c.Embeddings(ctx, EmbeddingRequest{Input: "x"})
	_, _ = c.Moderate(ctx, "x")
	_, _ = c.CheckGuardrails(ctx, "x", "p", nil)
	_, _ = c.RedactPII(ctx, "x", nil)
	_, _ = c.RehydratePII(ctx, "x", nil)
	_, _ = c.GenerateImage(ctx, ImageGenerateRequest{Prompt: "x"})
	_, _ = c.RefinePrompt(ctx, RefineRequest{Domain: "d", Intent: "i", RoughPrompt: "x"})
	_, _ = c.SummarizeDocument(ctx, "x", "d-1")
	_, _ = c.Generate(ctx, GenerateRequest{Model: "m", SystemPrompt: "s", UserPrompt: "u"})

	wantOps := []Operation{
		OpChatCompletion, OpEmbedding, OpModeration, OpGuardrails,
		OpPIIRedact, OpPIIRehydrate, OpImageGenerate, OpPromptRefine,
		OpDocumentSummarize, OpGenerate,
	}
	events := obs.snapshot()
	if len(events) != len(wantOps) {
		t.Fatalf("events=%d want %d (%v)", len(events), len(wantOps), opsList(events))
	}
	for i, want := range wantOps {
		if events[i].Operation != want {
			t.Errorf("event[%d] op=%s want %s", i, events[i].Operation, want)
		}
		if !events[i].Success {
			t.Errorf("event[%d] success=false err=%v", i, events[i].Err)
		}
	}
}

func opsList(events []Event) []Operation {
	out := make([]Operation, len(events))
	for i, ev := range events {
		out[i] = ev.Operation
	}
	return out
}

func TestObserverDurationMonotonic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	obs := &concurrentObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	_, _ = c.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	events := obs.snapshot()
	if len(events) != 1 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].Duration < 10*time.Millisecond {
		t.Errorf("Duration=%s should reflect handler delay (>=10ms)", events[0].Duration)
	}
}
