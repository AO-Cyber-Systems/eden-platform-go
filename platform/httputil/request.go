package httputil

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/platform/apierror"
)

// MaxBodySize caps the request body size accepted by DecodeJSON and ReadBody
// at 100 MiB. This is a defensive default; callers that legitimately need
// larger uploads should not use these helpers (use http.MaxBytesReader with
// the limit they actually want).
const MaxBodySize = 100 * 1024 * 1024 // 100MB

// DecodeJSON reads the request body (up to MaxBodySize) and unmarshals it into
// dst. Returns a *apierror.ValidationError ready to hand to WriteError on any
// failure — empty body, oversized body, malformed JSON.
//
// Callers should still call defer r.Body.Close() at the call site if they
// follow http.Server's standard contract; this helper closes its limited
// reader internally.
func DecodeJSON(r *http.Request, dst interface{}) *apierror.APIError {
	body := http.MaxBytesReader(nil, r.Body, MaxBodySize)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return apierror.ValidationError("request body too large or unreadable")
	}
	if len(data) == 0 {
		return apierror.ValidationError("request body is empty")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return apierror.ValidationError("invalid JSON: " + err.Error())
	}
	return nil
}

// ReadBody returns the raw request body bytes (capped at MaxBodySize) plus a
// validation error if reading exceeds the cap or fails. Use when the body
// shape is opaque to the handler — e.g. SSE proxying.
func ReadBody(r *http.Request) ([]byte, *apierror.APIError) {
	body := http.MaxBytesReader(nil, r.Body, MaxBodySize)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, apierror.ValidationError("request body too large or unreadable")
	}
	return data, nil
}

// GetClientIP returns the caller's IP from X-Forwarded-For (proxy chain),
// falling back to X-Real-IP, then to r.RemoteAddr. Trust headers only when
// you control the upstream proxy.
func GetClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
