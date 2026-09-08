package telephony

// integration_test.go proves the package's own seams — Registry, Resolver,
// WebhookHandler, TCPAService, PhoneOptOutStore — compose into the flow a
// real consumer wires: send -> status webhook -> opt-out -> refused send.
//
// It runs against a FAKE Provider, deliberately not signalwireProvider or
// twilioProvider. Vendor-specific wire format (Twilio-compatible form
// fields, HMAC-SHA1-over-reconstructed-URL) is already exhaustively covered
// by twilio_test.go and signalwire_test.go; this file's job is to prove the
// package composes correctly for ANY Provider implementation, which is the
// entire point of the Provider seam existing in the first place.
//
// The fake's signature scheme (HMAC-SHA256 over the raw body, hex-encoded,
// in an X-Fake-Signature header) is intentionally simple but exercises the
// same raw-bytes-in / per-tenant-secret-out shape every real adapter uses,
// so the security property VerifyAndResolve documents — resolve tenant by
// receiving number, then verify with THAT tenant's own secret, and never
// parse an unverified payload — is exercised for real, not asserted away.
import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// integrationSendLog is shared by every fakeIntegrationProvider instance the
// test's registered factory builds. This matters: Registry.For (and
// therefore VerifyAndResolve, which calls it directly rather than going
// through the Resolver's cache) invokes the factory fresh on every call —
// factories are documented as pure/stateless — so a send recorded on the
// instance built for the outbound SendSMS call would be invisible to an
// assertion made against a later instance built for a webhook call, unless
// the send history lives in something all instances share. A real vendor
// (Twilio/SignalWire) plays this same role in production: "was this number
// already sent to" is a fact the REMOTE system holds, not the local adapter
// value.
type integrationSendLog struct {
	mu   sync.Mutex
	sent []sentSMS
}

type sentSMS struct{ to, body, from string }

func (l *integrationSendLog) record(to, body, from string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sent = append(l.sent, sentSMS{to: to, body: body, from: from})
	return fmt.Sprintf("FAKESM%d", len(l.sent))
}

func (l *integrationSendLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sent)
}

// fakeIntegrationProvider is a minimal Provider implementation used only by
// this test.
type fakeIntegrationProvider struct {
	cfg Config
	log *integrationSendLog
}

func (p *fakeIntegrationProvider) Type() ProviderType { return p.cfg.Provider }

func (p *fakeIntegrationProvider) SendSMS(_ context.Context, to, body, fromOverride string) (SMSResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	sid := p.log.record(to, body, from)
	return SMSResult{MessageSID: sid, Status: "queued"}, nil
}

func (p *fakeIntegrationProvider) InitiateCall(_ context.Context, _, _, _ string) (CallResult, error) {
	return CallResult{}, errors.New("fakeIntegrationProvider: InitiateCall not exercised by this test")
}

func (p *fakeIntegrationProvider) InitiateBridge(_ context.Context, _, _, _, _ string) (CallResult, error) {
	return CallResult{}, errors.New("fakeIntegrationProvider: InitiateBridge not exercised by this test")
}

// VerifyWebhookSignature implements the fake's HMAC-SHA256-over-raw-body
// scheme against the tenant's own SignatureToken() — the same "the secret
// to check against depends on which tenant was just resolved" shape every
// real adapter has.
func (p *fakeIntegrationProvider) VerifyWebhookSignature(r *http.Request, body []byte) error {
	sig := r.Header.Get("X-Fake-Signature")
	if sig == "" {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(p.cfg.SignatureToken()))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return ErrInvalidSignature
	}
	return nil
}

func (p *fakeIntegrationProvider) ParseStatusWebhook(r *http.Request, _ []byte) (StatusEvent, error) {
	if err := r.ParseForm(); err != nil {
		return StatusEvent{}, fmt.Errorf("fake parse status form: %w", err)
	}
	sid := r.PostFormValue("MessageSid")
	if sid == "" {
		return StatusEvent{}, fmt.Errorf("fake status: missing MessageSid")
	}
	return StatusEvent{
		Kind:   "sms",
		SID:    sid,
		Status: r.PostFormValue("MessageStatus"),
		From:   r.PostFormValue("From"),
		To:     r.PostFormValue("To"),
	}, nil
}

func (p *fakeIntegrationProvider) ParseInboundSMS(r *http.Request, body []byte) (InboundSMS, error) {
	if err := r.ParseForm(); err != nil {
		return InboundSMS{}, fmt.Errorf("fake parse inbound form: %w", err)
	}
	sid := r.PostFormValue("MessageSid")
	if sid == "" {
		return InboundSMS{}, fmt.Errorf("fake inbound: missing MessageSid")
	}
	return InboundSMS{
		MessageSID: sid,
		From:       r.PostFormValue("From"),
		To:         r.PostFormValue("To"),
		Body:       r.PostFormValue("Body"),
		Raw:        body,
	}, nil
}

// fakeSign computes the same HMAC-SHA256-hex signature
// fakeIntegrationProvider.VerifyWebhookSignature expects.
func fakeSign(body []byte, token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// tcpaInboundSink is the InboundSMSSink a real consumer wires to feed every
// verified inbound SMS through TCPAService.HandleInboundBody, so a STOP/
// START reply is recognized and persisted without the webhook layer itself
// needing to know anything about TCPA. This is the intended production
// wiring — webhook.go defines InboundSMSSink as a small package-local
// interface for exactly this reason (see 40-06-SUMMARY.md).
type tcpaInboundSink struct {
	tcpa         *TCPAService
	providerType ProviderType
}

func (s *tcpaInboundSink) HandleInboundSMS(ctx context.Context, cfg Config, msg InboundSMS) error {
	_, err := s.tcpa.HandleInboundBody(ctx, cfg.CompanyID, s.providerType, msg)
	return err
}

// TestIntegration_SendStatusWebhookOptOutRefusedSend drives the full
// consumer-facing lifecycle against a fake Provider:
//
//  1. Send — TCPAService.CheckSendAllowed passes (no opt-out on file), then
//     Resolver.For builds the tenant's Provider and SendSMS is called.
//  2. Status webhook — a signed delivery-status callback for that message
//     is verified, resolved to the tenant, parsed, and reaches the
//     SMSStatusSink.
//  3. Opt-out — the recipient replies STOP; the signed inbound-SMS webhook
//     is verified, resolved, parsed, and reaches TCPAService via
//     InboundSMSSink, which persists the opt-out to PhoneOptOutStore.
//  4. Refused send — CheckSendAllowed now returns ErrRecipientOptedOut
//     BEFORE any Provider method is invoked again; the send log proves the
//     Provider was never touched a second time.
func TestIntegration_SendStatusWebhookOptOutRefusedSend(t *testing.T) {
	ctx := context.Background()

	const (
		fakeProviderType = ProviderType("fake")
		sendingNumber    = "+15555550100" // the tenant's own number
		recipient        = "+15555550199" // the person being texted
		webhookToken     = "integration-test-webhook-secret"
	)
	companyID := uuid.New()

	cfg := Config{
		CompanyID:     companyID,
		Provider:      fakeProviderType,
		AccountSID:    "ACINTEGRATIONTEST",
		AuthToken:     webhookToken,
		SendingNumber: sendingNumber,
		IsActive:      true,
	}

	// --- wire the package's own seams, the way a real consumer would ---
	store := newFakeConfigStore(cfg) // ConfigStore fake shared with webhook_test.go
	reg := NewRegistry()
	sendLog := &integrationSendLog{}
	reg.Register(fakeProviderType, func(c Config) (Provider, error) {
		return &fakeIntegrationProvider{cfg: c, log: sendLog}, nil
	})
	resolver := NewResolver(store, reg, nil, nil)

	phoneStore := &fakePhoneStore{} // PhoneOptOutStore fake shared with tcpa_test.go
	tcpa := NewTCPAService(nil, nil, phoneStore)

	smsStatusSink := &fakeSMSStatusSink{} // shared with webhook_test.go
	inboundSink := &tcpaInboundSink{tcpa: tcpa, providerType: fakeProviderType}

	handler := &WebhookHandler{
		Store:     store,
		Registry:  reg,
		SMSStatus: smsStatusSink,
		Inbound:   inboundSink,
	}

	var messageSID string

	t.Run("send", func(t *testing.T) {
		// The precondition gate every outbound call site must run BEFORE
		// touching a Provider — see TCPAService.CheckSendAllowed's doc
		// comment. No opt-out exists yet, so this must pass.
		if err := tcpa.CheckSendAllowed(ctx, companyID, recipient); err != nil {
			t.Fatalf("CheckSendAllowed before any opt-out: %v", err)
		}

		p, err := resolver.For(ctx, companyID)
		if err != nil {
			t.Fatalf("resolver.For: %v", err)
		}
		result, err := p.SendSMS(ctx, recipient, "Reminder: meeting at 6pm", "")
		if err != nil {
			t.Fatalf("SendSMS: %v", err)
		}
		if result.MessageSID == "" {
			t.Fatal("SendSMS returned an empty MessageSID")
		}
		if got := sendLog.count(); got != 1 {
			t.Fatalf("send log has %d entries; want 1", got)
		}
		messageSID = result.MessageSID
	})

	t.Run("status_webhook", func(t *testing.T) {
		statusURL := "https://api.example.com/webhooks/telephony/fake/status"
		form := url.Values{}
		form.Set("MessageSid", messageSID)
		form.Set("MessageStatus", "delivered")
		form.Set("From", sendingNumber)
		// VerifyAndResolve keys tenant resolution off the form's "To"
		// field via ConfigStore.LookupBySendingNumber(provider, To) — the
		// tenant's own number, never a self-asserted body field. See
		// webhook.go's VerifyAndResolve doc comment.
		form.Set("To", sendingNumber)
		body := []byte(form.Encode())

		r := httptest.NewRequest(http.MethodPost, statusURL, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Fake-Signature", fakeSign(body, webhookToken))

		w := httptest.NewRecorder()
		handler.StatusHandler(fakeProviderType)(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status webhook: code = %d, body = %s", w.Code, w.Body.String())
		}
		if len(smsStatusSink.calls) != 1 {
			t.Fatalf("SMSStatusSink calls = %d; want 1", len(smsStatusSink.calls))
		}
		if smsStatusSink.calls[0].SID != messageSID {
			t.Errorf("status event SID = %q; want %q", smsStatusSink.calls[0].SID, messageSID)
		}
		if smsStatusSink.calls[0].Status != "delivered" {
			t.Errorf("status event Status = %q; want delivered", smsStatusSink.calls[0].Status)
		}
	})

	t.Run("stop_optout", func(t *testing.T) {
		inboundURL := "https://api.example.com/webhooks/telephony/fake/sms_inbound"
		form := url.Values{}
		form.Set("MessageSid", "FAKEIN1")
		form.Set("From", recipient)
		form.Set("To", sendingNumber)
		form.Set("Body", "STOP")
		body := []byte(form.Encode())

		r := httptest.NewRequest(http.MethodPost, inboundURL, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Fake-Signature", fakeSign(body, webhookToken))

		w := httptest.NewRecorder()
		handler.InboundSMSHandler(fakeProviderType)(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("inbound sms webhook: code = %d, body = %s", w.Code, w.Body.String())
		}

		optedOut, err := phoneStore.IsOptedOut(ctx, companyID, recipient)
		if err != nil {
			t.Fatalf("IsOptedOut: %v", err)
		}
		if !optedOut {
			t.Fatal("recipient should be opted out after a verified STOP reply, but is not")
		}
	})

	t.Run("refused_send", func(t *testing.T) {
		err := tcpa.CheckSendAllowed(ctx, companyID, recipient)
		if !errors.Is(err, ErrRecipientOptedOut) {
			t.Fatalf("CheckSendAllowed after opt-out: err = %v; want ErrRecipientOptedOut", err)
		}
		// The refusal must happen BEFORE the Provider is ever touched
		// again -- CheckSendAllowed is a precondition a call site consults
		// prior to calling SendSMS, not a post-hoc filter. Confirm no
		// second send reached the (shared, cross-instance) send log.
		if got := sendLog.count(); got != 1 {
			t.Fatalf("send log has %d entries after a refused send; want 1 (the provider must never be invoked)", got)
		}
	})
}

// TestIntegration_TamperedStatusWebhook_NeverReachesSink extends the same
// composed stack with the package's core security property: a status
// webhook whose body was tampered with in transit (replaying a signature
// computed over different bytes) is rejected before ParseStatusWebhook ever
// runs, so the tampered event never reaches a sink.
func TestIntegration_TamperedStatusWebhook_NeverReachesSink(t *testing.T) {
	const (
		fakeProviderType = ProviderType("fake")
		sendingNumber    = "+15555550200"
		webhookToken     = "integration-test-tamper-secret"
	)
	companyID := uuid.New()
	cfg := Config{
		CompanyID: companyID, Provider: fakeProviderType,
		AccountSID: "ACTAMPER", AuthToken: webhookToken, SendingNumber: sendingNumber,
	}

	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	sendLog := &integrationSendLog{}
	reg.Register(fakeProviderType, func(c Config) (Provider, error) {
		return &fakeIntegrationProvider{cfg: c, log: sendLog}, nil
	})
	smsStatusSink := &fakeSMSStatusSink{}
	handler := &WebhookHandler{Store: store, Registry: reg, SMSStatus: smsStatusSink}

	statusURL := "https://api.example.com/webhooks/telephony/fake/status"

	signedForm := url.Values{}
	signedForm.Set("MessageSid", "SM-real")
	signedForm.Set("MessageStatus", "delivered")
	signedForm.Set("To", sendingNumber)
	sig := fakeSign([]byte(signedForm.Encode()), webhookToken)

	// Bytes actually sent differ from what was signed (status flipped).
	tamperedForm := url.Values{}
	tamperedForm.Set("MessageSid", "SM-real")
	tamperedForm.Set("MessageStatus", "failed")
	tamperedForm.Set("To", sendingNumber)
	tamperedBody := []byte(tamperedForm.Encode())

	r := httptest.NewRequest(http.MethodPost, statusURL, strings.NewReader(string(tamperedBody)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Fake-Signature", sig)

	w := httptest.NewRecorder()
	handler.StatusHandler(fakeProviderType)(w, r)

	if w.Code >= 200 && w.Code < 300 {
		t.Fatalf("tampered status webhook: code = %d; want a non-2xx rejection", w.Code)
	}
	if len(smsStatusSink.calls) != 0 {
		t.Fatalf("SMSStatusSink invoked for an unverified payload: %d calls", len(smsStatusSink.calls))
	}
}
