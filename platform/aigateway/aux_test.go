package aigateway

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Moderation ---

func TestModerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ModerationRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Input != "bad words" {
			t.Errorf("Input=%q", body.Input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"omni-moderation-latest","results":[{"flagged":true,"categories":{"hate":true,"violence":false},"category_scores":{"hate":0.9}}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.Moderate(context.Background(), "bad words")
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if !resp.Results[0].Flagged {
		t.Error("expected flagged")
	}
	if !resp.Results[0].Categories["hate"] {
		t.Error("hate category missing")
	}
	if resp.Results[0].CategoryScores["hate"] != 0.9 {
		t.Errorf("hate score=%v", resp.Results[0].CategoryScores["hate"])
	}
}

// --- Guardrails ---

func TestCheckGuardrails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body GuardrailsCheckRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Policy != "family-safe" {
			t.Errorf("Policy=%q", body.Policy)
		}
		if body.Metadata["user"] != "u-1" {
			t.Errorf("Metadata=%v", body.Metadata)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"safe":false,"flagged_categories":["violence"],"reason":"violent imagery","confidence":0.92}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.CheckGuardrails(context.Background(), "scary content", "family-safe", map[string]string{"user": "u-1"})
	if err != nil {
		t.Fatalf("CheckGuardrails: %v", err)
	}
	if resp.Safe {
		t.Error("Safe should be false")
	}
	if len(resp.FlaggedCategories) != 1 || resp.FlaggedCategories[0] != "violence" {
		t.Errorf("FlaggedCategories=%v", resp.FlaggedCategories)
	}
	if resp.Confidence != 0.92 {
		t.Errorf("Confidence=%v", resp.Confidence)
	}
}

// --- PII redact / rehydrate ---

func TestRedactPIIAndRehydrate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PIIRedactPath:
			var body PIIRedactRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.EntityTypes) != 1 || body.EntityTypes[0] != "EMAIL" {
				t.Errorf("EntityTypes=%v", body.EntityTypes)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"redacted_text":"hello [EMAIL_0]","entities":[{"type":"EMAIL","value":"x@y.com","start_index":6,"end_index":13}],"has_pii":true}`))
		case PIIRehydratePath:
			var body PIIRehydrateRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Entities) != 1 || body.Entities[0].Value != "x@y.com" {
				t.Errorf("Entities=%v", body.Entities)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"original_text":"hello x@y.com"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	red, err := c.RedactPII(context.Background(), "hello x@y.com", []string{"EMAIL"})
	if err != nil {
		t.Fatalf("RedactPII: %v", err)
	}
	if !red.HasPII || red.RedactedText != "hello [EMAIL_0]" {
		t.Errorf("redact result=%+v", red)
	}
	rehydrated, err := c.RehydratePII(context.Background(), red.RedactedText, red.Entities)
	if err != nil {
		t.Fatalf("RehydratePII: %v", err)
	}
	if rehydrated.OriginalText != "hello x@y.com" {
		t.Errorf("OriginalText=%q", rehydrated.OriginalText)
	}
}

// --- Image generation ---

func TestGenerateImageDefaultsN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ImageGenerateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.N != 1 {
			t.Errorf("N=%d want 1 (default)", body.N)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/img.png"}],"model":"dall-e-3"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.GenerateImage(context.Background(), ImageGenerateRequest{Prompt: "a robot"})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL == "" {
		t.Errorf("response shape: %+v", resp)
	}
}

// --- Audio transcription ---

func TestTranscribeAudioMultipartUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Fatalf("Content-Type=%q", ct)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader: %v", err)
		}
		var sawFile, sawModel bool
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			switch part.FormName() {
			case "file":
				sawFile = true
				if part.FileName() != "clip.wav" {
					t.Errorf("filename=%q", part.FileName())
				}
				body, _ := io.ReadAll(part)
				if string(body) != "audiobytes" {
					t.Errorf("audio bytes=%q", body)
				}
			case "model":
				sawModel = true
				body, _ := io.ReadAll(part)
				if string(body) != "whisper-1" {
					t.Errorf("model=%q", body)
				}
			}
		}
		if !sawFile || !sawModel {
			t.Errorf("missing parts: file=%v model=%v", sawFile, sawModel)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world","language":"en","duration":1.2}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	res, err := c.TranscribeAudio(context.Background(), strings.NewReader("audiobytes"), "clip.wav", "whisper-1")
	if err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if res.Text != "hello world" {
		t.Errorf("Text=%q", res.Text)
	}
	if res.Language != "en" {
		t.Errorf("Language=%q", res.Language)
	}
}

// --- Prompt refinement ---

func TestRefinePrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body RefineRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Domain != "health" || body.Intent != "meal_plan" {
			t.Errorf("domain/intent=%q/%q", body.Domain, body.Intent)
		}
		if body.Context["age"] != float64(34) {
			t.Errorf("Context=%v", body.Context)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refined_prompt":"polished","model_hint":"gpt-4o-mini","cache_ttl_hours":24}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.RefinePrompt(context.Background(), RefineRequest{
		Domain:      "health",
		Intent:      "meal_plan",
		Context:     map[string]any{"age": 34},
		RoughPrompt: "quick meal plan",
	})
	if err != nil {
		t.Fatalf("RefinePrompt: %v", err)
	}
	if resp.RefinedPrompt != "polished" || resp.ModelHint != "gpt-4o-mini" || resp.CacheTTLHours != 24 {
		t.Errorf("response=%+v", resp)
	}
}

// --- Document summarize / extract / generate ---

func TestSummarizeDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body SummarizeDocumentRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Text == "" || body.DocumentID == "" {
			t.Errorf("missing fields: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"a summary","hash":"abc"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.SummarizeDocument(context.Background(), "long text", "doc-1")
	if err != nil {
		t.Fatalf("SummarizeDocument: %v", err)
	}
	if got != "a summary" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTextMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader: %v", err)
		}
		var sawFile bool
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "file" {
				sawFile = true
				body, _ := io.ReadAll(part)
				if string(body) != "PDFBYTES" {
					t.Errorf("body=%q", body)
				}
			}
		}
		if !sawFile {
			t.Error("missing file part")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"extracted text"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ExtractText(context.Background(), []byte("PDFBYTES"), "doc.pdf", "doc-1")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got != "extracted text" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateLegacySurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body GenerateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.SystemPrompt == "" || body.UserPrompt == "" {
			t.Errorf("missing prompts: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"draft","flagged":true,"flag_reason":"policy","usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer srv.Close()

	obs := &captureObserver{}
	c := newTestClient(t, srv, WithObserver(obs))
	resp, err := c.Generate(context.Background(), GenerateRequest{
		Model:        "x",
		SystemPrompt: "be helpful",
		UserPrompt:   "draft",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !resp.Flagged || resp.FlagReason != "policy" {
		t.Errorf("Flagged=%v reason=%q", resp.Flagged, resp.FlagReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage=%+v", resp.Usage)
	}
	if len(obs.events) != 1 || obs.events[0].PromptTokens != 10 || obs.events[0].CompletionTokens != 5 || obs.events[0].TotalTokens != 15 {
		t.Errorf("observer event=%+v", obs.events)
	}
}

// Sanity check: doMultipart helper rejects nil response decoding when out is
// nil — should not error.
func TestDoMultipartDiscardsBodyWhenOutNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`junk-not-json`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.doMultipart(context.Background(), "/", func(w *multipart.Writer) error {
		return w.WriteField("x", "y")
	}, nil)
	if err != nil {
		t.Fatalf("doMultipart: %v", err)
	}
}
