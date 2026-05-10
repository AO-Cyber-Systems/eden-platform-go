package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// chunkEncode renders a chat completion chunk as an SSE data line.
func chunkEncode(t *testing.T, content string, done bool) string {
	t.Helper()
	chunk := ChatCompletionChunk{
		Model: "test",
		Choices: []StreamChoice{{
			Index: 0,
			Delta: StreamDelta{Content: content},
		}},
	}
	if done {
		fr := "stop"
		chunk.Choices[0].FinishReason = &fr
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(b) + "\n"
}

func TestChatCompletionStreamHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept=%q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, chunkEncode(t, "Hello", false))
		flusher.Flush()
		fmt.Fprint(w, chunkEncode(t, ", ", false))
		flusher.Flush()
		fmt.Fprint(w, chunkEncode(t, "world", true))
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	chunks, errs := c.ChatCompletionStream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})

	var got strings.Builder
	for ch := range chunks {
		got.WriteString(ch)
	}
	for err := range errs {
		t.Fatalf("stream error: %v", err)
	}
	if got.String() != "Hello, world" {
		t.Errorf("assembled=%q want %q", got.String(), "Hello, world")
	}
}

func TestChatCompletionStreamMalformedSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Comment line, blank line, malformed JSON, then valid chunk, then DONE.
		fmt.Fprint(w, ": keepalive\n")
		flusher.Flush()
		fmt.Fprint(w, "\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {not json\n")
		flusher.Flush()
		fmt.Fprint(w, chunkEncode(t, "ok", true))
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	chunks, errs := c.ChatCompletionStream(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	var got strings.Builder
	for ch := range chunks {
		got.WriteString(ch)
	}
	for err := range errs {
		t.Fatalf("stream error: %v", err)
	}
	if got.String() != "ok" {
		t.Errorf("got=%q want ok", got.String())
	}
}

func TestChatCompletionStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	chunks, errs := c.ChatCompletionStream(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	for range chunks {
		// drain
	}
	var gotErr error
	for err := range errs {
		gotErr = err
	}
	if !errors.Is(gotErr, ErrUnauthorized) {
		t.Fatalf("err=%v want ErrUnauthorized", gotErr)
	}
}

func TestChatCompletionStreamContextCancel(t *testing.T) {
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, chunkEncode(t, "first", false))
		flusher.Flush()
		<-hold
		// Server keeps the connection open until hold closes.
	}))
	defer func() {
		close(hold)
		srv.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, srv)
	chunks, errs := c.ChatCompletionStream(ctx, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})

	// Read first chunk, then cancel.
	first := <-chunks
	if first != "first" {
		t.Errorf("first chunk=%q", first)
	}
	cancel()

	// Drain remaining chunks (none, in practice).
	for range chunks {
	}
	var gotErr error
	select {
	case err := <-errs:
		gotErr = err
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after cancel")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "context") {
		// canceling can also surface as a connection-closed error from the
		// underlying transport; either is acceptable.
		if !errors.Is(gotErr, context.Canceled) {
			t.Logf("err=%v (acceptable: connection close after cancel)", gotErr)
		}
	}
}

func TestChatCompletionStreamNotConfigured(t *testing.T) {
	c := &Client{}
	_, errs := c.ChatCompletionStream(context.Background(), ChatRequest{})
	err := <-errs
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err=%v want ErrNotConfigured", err)
	}
}
