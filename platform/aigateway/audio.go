package aigateway

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"time"
)

// AudioTranscribePath is the gateway endpoint for speech-to-text.
const AudioTranscribePath = "/v1/audio/transcriptions"

// TranscriptionSegment is a timed portion of a transcription.
type TranscriptionSegment struct {
	Start float64 `json:"start"` // seconds
	End   float64 `json:"end"`   // seconds
	Text  string  `json:"text"`
}

// TranscriptionResult holds the output of an audio transcription.
type TranscriptionResult struct {
	Text     string                 `json:"text"`
	Language string                 `json:"language,omitempty"`
	Duration float64                `json:"duration,omitempty"` // seconds
	Segments []TranscriptionSegment `json:"segments,omitempty"`
}

// TranscribeAudio uploads audio data via multipart/form-data and returns the
// transcription. filename is used for the Content-Disposition header (the
// gateway/provider may use the extension to identify the audio format).
// model is forwarded as the form's "model" field; an empty model defers to
// the gateway's default.
func (c *Client) TranscribeAudio(ctx context.Context, audio io.Reader, filename, model string) (*TranscriptionResult, error) {
	if filename == "" {
		filename = "audio.bin"
	}
	started := time.Now()
	var out TranscriptionResult
	err := c.doMultipart(ctx, AudioTranscribePath, func(w *multipart.Writer) error {
		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			return fmt.Errorf("create file part: %w", err)
		}
		if _, err := io.Copy(fw, audio); err != nil {
			return fmt.Errorf("copy audio: %w", err)
		}
		if model != "" {
			if err := w.WriteField("model", model); err != nil {
				return fmt.Errorf("write model field: %w", err)
			}
		}
		return nil
	}, &out)
	c.observe(ctx, OpAudioTranscribe, model, Usage{}, started, err)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
