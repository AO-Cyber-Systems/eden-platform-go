package telephony

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// MaxWebhookBodyBytes bounds the webhook body accepted before any
// signature/parse work runs. Twilio and SignalWire status callbacks and
// inbound SMS payloads are comfortably under this ceiling; ported from
// politihub's handler_webhook.go and justinforme's webhook_middleware.go,
// which independently arrived at the same 8 KiB limit.
const MaxWebhookBodyBytes = 8192

// ErrWebhookBodyTooLarge is returned by ReadWebhookBody when the request
// body is at or above MaxWebhookBodyBytes. Handlers map this to HTTP 413
// before any signature or parse work runs.
var ErrWebhookBodyTooLarge = errors.New("telephony: webhook body exceeds size limit")

// ReadWebhookBody reads up to MaxWebhookBodyBytes+1 bytes from r.Body and
// restores r.Body with a fresh reader over the exact bytes it read, so every
// downstream consumer -- VerifyAndResolve's form peek, a Provider's
// VerifyWebhookSignature, and its ParseStatusWebhook / ParseInboundSMS --
// sees the untouched original body, never a partially-drained one.
//
// This MUST be the first thing any webhook handler does. Any middleware
// upstream of it that reads r.Body first -- request logging, tracing,
// generic body-dumpers -- drains the reader, and every signature check below
// silently fails against a truncated or empty body.
func ReadWebhookBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("telephony: read webhook body: %w", err)
	}
	if len(body) > MaxWebhookBodyBytes {
		return nil, ErrWebhookBodyTooLarge
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// VerifyAndResolve is the mandatory security gate every webhook handler in
// this package runs before the payload is parsed into a StatusEvent /
// InboundSMS or acted on in any way. Given the raw bytes ReadWebhookBody
// captured, it:
//
//  1. Restores r.Body to those exact bytes and reads the form's "To" field --
//     the number the carrier dialed. This is the ONLY body field ever
//     consulted for tenant identity. A self-asserted tenant/company id
//     elsewhere in the body is never read for this purpose: an attacker
//     fully controls every body field before verification has run, so
//     trusting one to pick a tenant would let the attacker pick which
//     tenant's data their forged request lands in.
//  2. Resolves the owning tenant via ConfigStore.LookupBySendingNumber --
//     the same chokepoint migration 016's partial unique index enforces (a
//     sending number is owned by at most one active tenant per provider).
//  3. Builds that tenant's Provider from the Registry and calls
//     VerifyWebhookSignature with the untouched raw body bytes, restoring
//     r.Body once more first since the form read above drained it.
//
// Resolution necessarily happens before the HMAC check, not after: each
// tenant's Config carries its OWN signing secret (Config.SignatureToken()),
// so the secret to verify against cannot be known until a tenant has been
// identified. What VerifyWebhookSignature then guarantees is that the
// request could only have been produced by someone holding THAT tenant's
// secret. An attacker who addresses a forged request at a victim tenant's
// number still fails verification here, because they do not hold that
// tenant's secret -- the receiving number is a resolution key, the
// signature is the actual authority. Either failure (unknown number, bad
// signature) returns before ParseStatusWebhook / ParseInboundSMS ever runs,
// so an unverified payload is never parsed or acted on.
func VerifyAndResolve(ctx context.Context, store ConfigStore, registry *Registry, provider ProviderType, r *http.Request, body []byte) (Provider, Config, error) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := r.ParseForm(); err != nil {
		return nil, Config{}, fmt.Errorf("telephony: parse webhook form: %w", err)
	}
	to := r.PostFormValue("To")
	if to == "" {
		return nil, Config{}, errors.New("telephony: webhook missing To field")
	}

	cfg, err := store.LookupBySendingNumber(ctx, provider, to)
	if err != nil {
		return nil, Config{}, err
	}
	prov, err := registry.For(cfg)
	if err != nil {
		return nil, Config{}, err
	}

	// r.ParseForm above drained r.Body; restore the exact bytes
	// ReadWebhookBody captured before either the signature check or a later
	// Parse* call touches the request again.
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := prov.VerifyWebhookSignature(r, body); err != nil {
		return nil, Config{}, err
	}
	return prov, cfg, nil
}

// SMSStatusSink receives a verified SMS delivery-status StatusEvent
// (StatusEvent.Kind == "sms") for durable persistence or downstream
// fan-out. Implemented by a consumer's SMS status store.
type SMSStatusSink interface {
	HandleSMSStatus(ctx context.Context, cfg Config, ev StatusEvent) error
}

// CallStatusSink receives a verified call delivery-status StatusEvent
// (StatusEvent.Kind == "call") for durable persistence or downstream
// fan-out. Implemented by a consumer's call status store.
type CallStatusSink interface {
	HandleCallStatus(ctx context.Context, cfg Config, ev StatusEvent) error
}

// InboundSMSSink receives a verified InboundSMS for durable persistence or
// downstream routing (opt-out matching, conversation threading, etc).
// Implemented by a consumer's inbound message store/router.
type InboundSMSSink interface {
	HandleInboundSMS(ctx context.Context, cfg Config, msg InboundSMS) error
}

// WebhookAuditWriter persists the raw webhook payload before it is acted on.
// Best effort: WebhookHandler logs and swallows the writer's own errors
// rather than block the provider-facing response -- an audit failure must
// never turn into a retry storm. A nil Audit on WebhookHandler skips
// auditing entirely.
type WebhookAuditWriter interface {
	Write(ctx context.Context, source string, raw []byte) error
}

// WebhookHandler wires a ConfigStore + Registry, plus optional audit and
// sink dependencies, into the inbound telephony webhook endpoints. Every
// handler method enforces the same order: capture raw body -> verify
// signature -> resolve tenant by receiving number -> parse -> act. Sinks are
// all optional (nil is a valid no-op) so a caller can wire only the parts it
// needs.
type WebhookHandler struct {
	Store    ConfigStore
	Registry *Registry
	Audit    WebhookAuditWriter

	SMSStatus  SMSStatusSink
	CallStatus CallStatusSink
	Inbound    InboundSMSSink
}

// StatusHandler returns an http.HandlerFunc for the given provider's status
// callback endpoint. A single endpoint serves BOTH SMS status
// (MessageSid/MessageStatus) and call status (CallSid/CallStatus) callbacks
// -- ParseStatusWebhook already disambiguates via StatusEvent.Kind, so
// dispatch to the matching sink happens after a single parse rather than
// running two separate endpoints that each parse-then-filter the same form.
func (h *WebhookHandler) StatusHandler(provider ProviderType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ReadWebhookBody(r)
		if !h.rejectBadBody(w, err) {
			return
		}
		h.audit(r.Context(), "status:"+string(provider), body)

		prov, cfg, err := VerifyAndResolve(r.Context(), h.Store, h.Registry, provider, r, body)
		if err != nil {
			writeVerifyFailure(w, err)
			return
		}

		ev, err := prov.ParseStatusWebhook(r, body)
		if err != nil {
			slog.Warn("telephony webhook status: parse failed", "provider", provider, "err", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		switch ev.Kind {
		case "sms":
			if h.SMSStatus != nil {
				if err := h.SMSStatus.HandleSMSStatus(r.Context(), cfg, ev); err != nil {
					slog.Warn("telephony webhook sms status: sink failed",
						"provider", provider, "sid", ev.SID, "err", err)
				}
			}
		case "call":
			if h.CallStatus != nil {
				if err := h.CallStatus.HandleCallStatus(r.Context(), cfg, ev); err != nil {
					slog.Warn("telephony webhook call status: sink failed",
						"provider", provider, "sid", ev.SID, "err", err)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}
}

// InboundSMSHandler returns an http.HandlerFunc for the given provider's
// inbound SMS endpoint.
func (h *WebhookHandler) InboundSMSHandler(provider ProviderType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ReadWebhookBody(r)
		if !h.rejectBadBody(w, err) {
			return
		}
		h.audit(r.Context(), "sms_inbound:"+string(provider), body)

		prov, cfg, err := VerifyAndResolve(r.Context(), h.Store, h.Registry, provider, r, body)
		if err != nil {
			writeVerifyFailure(w, err)
			return
		}

		msg, err := prov.ParseInboundSMS(r, body)
		if err != nil {
			slog.Warn("telephony webhook inbound sms: parse failed", "provider", provider, "err", err)
			writeTwiML(w)
			return
		}

		if h.Inbound != nil {
			if err := h.Inbound.HandleInboundSMS(r.Context(), cfg, msg); err != nil {
				slog.Warn("telephony webhook inbound sms: sink failed",
					"provider", provider, "sid", msg.MessageSID, "err", err)
			}
		}
		writeTwiML(w)
	}
}

// rejectBadBody writes the appropriate error response for a ReadWebhookBody
// failure and reports whether the caller should continue processing.
func (h *WebhookHandler) rejectBadBody(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrWebhookBodyTooLarge):
		http.Error(w, "request entity too large", http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "bad request", http.StatusBadRequest)
	}
	return false
}

// audit best-effort writes the raw payload. Never logs the payload bytes
// themselves -- only the outcome -- so an audit-write failure never persists
// unverified attacker input into application logs.
func (h *WebhookHandler) audit(ctx context.Context, source string, body []byte) {
	if h.Audit == nil {
		return
	}
	if err := h.Audit.Write(ctx, source, body); err != nil {
		slog.Warn("telephony webhook: audit write failed", "source", source, "err", err)
	}
}

// writeVerifyFailure maps a VerifyAndResolve error to the response a
// provider should see. Every branch is a non-2xx status, and none of them
// echo the request body back.
func writeVerifyFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTenantNotConfigured):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidSignature):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		http.Error(w, "bad request", http.StatusBadRequest)
	}
}

const emptyTwiML = `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`

// writeTwiML writes an empty TwiML response. Twilio and SignalWire both
// expect 200 with a TwiML body, even for a no-op inbound SMS.
func writeTwiML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(emptyTwiML))
}
