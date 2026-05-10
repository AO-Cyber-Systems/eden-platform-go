// Package httputil provides minimal HTTP response/request helpers for handlers
// in the Eden portfolio: JSON encoding, error rendering paired with
// platform/apierror, Server-Sent Events emission, and request decoding with
// a body-size cap.
//
// Promoted from aosentry/pkg/httputil per the standardization plan §3 Hidden
// Gems and Objective 10 (AOSentry pkg/ Promotion).
package httputil

import (
	"encoding/json"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/platform/apierror"
)

// WriteJSON encodes data as JSON and writes it with the given status code.
// If data is nil only the status code (and Content-Type header) is written —
// useful for 204 No Content responses.
//
// Encoding errors are intentionally swallowed: the response status has
// already been committed by the time encoding starts, so returning the error
// to the caller would not let them recover; logging belongs at a higher
// layer (a middleware that observes io.Writer failures).
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// WriteError renders an *apierror.APIError to the response: status code from
// err.StatusCode, body shape `{ "error": { ... } }`, Content-Type JSON.
//
// Pair with the constructors in platform/apierror; for example:
//
//	httputil.WriteError(w, apierror.NotFound("widget not found"))
func WriteError(w http.ResponseWriter, err *apierror.APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.StatusCode)
	_ = json.NewEncoder(w).Encode(apierror.ErrorResponse{Error: err})
}

// WriteSSEEvent writes a single Server-Sent Events `data:` line and flushes.
// The `event` parameter is currently ignored (kept in the signature so a
// future caller can emit `event: foo\n` ahead of the data line without a
// breaking change).
func WriteSSEEvent(w http.ResponseWriter, event string, data []byte) {
	_ = event
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// WriteSSEDone writes the OpenAI-style stream terminator `data: [DONE]` and
// flushes. Matches the wire shape used by streaming chat-completion clients.
func WriteSSEDone(w http.ResponseWriter) {
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
