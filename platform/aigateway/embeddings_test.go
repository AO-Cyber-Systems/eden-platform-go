package aigateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddingsDefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Model != DefaultEmbeddingModel {
			t.Errorf("Model=%q want %q", body.Model, DefaultEmbeddingModel)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.Embeddings(context.Background(), EmbeddingRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Embeddings: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 3 {
		t.Errorf("unexpected response shape: %+v", resp)
	}
}

func TestEmbeddingsBatchInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		input, ok := body["input"].([]any)
		if !ok {
			t.Fatalf("input should marshal as array, got %T", body["input"])
		}
		if len(input) != 2 {
			t.Errorf("input length=%d want 2", len(input))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]},{"index":1,"embedding":[0.2]}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.Embeddings(context.Background(), EmbeddingRequest{Input: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Embeddings: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("data=%d want 2", len(resp.Data))
	}
}

func TestEmbedTextFloat32Conversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.5,1.5,-0.25]}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	v, err := c.EmbedText(context.Background(), "hi", "")
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	want := []float32{0.5, 1.5, -0.25}
	if len(v) != len(want) {
		t.Fatalf("len=%d want %d", len(v), len(want))
	}
	for i := range want {
		if v[i] != want[i] {
			t.Errorf("v[%d]=%v want %v", i, v[i], want[i])
		}
	}
}

func TestEmbedTextEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.EmbedText(context.Background(), "hi", ""); err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestEmbeddingsObserverEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[0.1]}],"usage":{"total_tokens":7}}`))
	}))
	defer srv.Close()

	obs := &captureObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	if _, err := c.Embeddings(context.Background(), EmbeddingRequest{Input: "x"}); err != nil {
		t.Fatalf("Embeddings: %v", err)
	}
	if len(obs.events) != 1 || obs.events[0].Operation != OpEmbedding {
		t.Errorf("events=%+v", obs.events)
	}
	if obs.events[0].TotalTokens != 7 {
		t.Errorf("TotalTokens=%d want 7", obs.events[0].TotalTokens)
	}
}
