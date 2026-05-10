// Package aigateway is the canonical Go client for the AOSentry AI gateway.
//
// AOSentry is the AO Cyber AI gateway / LLM proxy that exposes an
// OpenAI-compatible HTTP surface. It routes requests across multiple model
// providers (OpenAI, Anthropic, Google, xAI, local Ollama), enforces per-key
// budget + rate limits, records spend, and provides safety primitives
// (moderation, guardrails, PII redact/rehydrate) on top of model traffic.
//
// This package consolidates seven prior forks of an AOSentry HTTP client that
// grew up independently across the portfolio:
//
//   - eden-circle/internal/ai (gateway_aosentry.go)
//   - eden-biz-go/internal/docprocessing (aosentry_client.go)
//   - aohealth-go/internal/aosentry
//   - justinforme/app/ai (aosentry.go)
//   - smartWellness/internal/aosentry
//   - aofamily-ai/go/internal/aosentry
//   - aofamily-browser/go/internal/aosentry
//
// The package is the union of those forks' real surface, no more: chat
// completions (text + vision + JSON-mode), streaming chat, embeddings,
// moderation, guardrails, PII redact/rehydrate, image generation, audio
// transcription, prompt refinement, document summarize/extract, and the
// legacy /v1/generate "system+user prompt" surface used by justinforme.
//
// New surfaces (function calling / tool use, etc.) are intentionally out of
// scope until a real consumer needs them.
//
// # Configuration
//
// Use ConfigFromEnv to load configuration from AOSENTRY_BASE_URL,
// AOSENTRY_API_KEY, AOSENTRY_DEFAULT_MODEL, AOSENTRY_TIMEOUT, and
// AOSENTRY_MAX_RETRIES. Or build a Config struct directly.
//
// # Construction
//
// NewClient returns ErrNotConfigured when BaseURL is empty so consumers can
// gracefully degrade ("AI unavailable") in environments where AOSentry is not
// provisioned.
//
// # Observability
//
// Pass WithObserver to NewClient to receive a structured Event for every
// outbound call (operation, model, token counts, duration, success/error).
// The Observer interface keeps the package free of a hard dependency on any
// specific metrics or tracing library.
package aigateway
