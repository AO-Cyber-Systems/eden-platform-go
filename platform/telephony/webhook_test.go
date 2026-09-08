package telephony

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// --- fakes shared by this file ---------------------------------------------

// fakeConfigStore resolves configs by (provider, sending_number), mirroring
// PostgresConfigStore.LookupBySendingNumber's role as the sole
// tenant-resolution chokepoint for inbound webhooks.
type fakeConfigStore struct {
	byNumber map[string]Config
}

func newFakeConfigStore(configs ...Config) *fakeConfigStore {
	m := map[string]Config{}
	for _, c := range configs {
		m[string(c.Provider)+"|"+c.SendingNumber] = c
	}
	return &fakeConfigStore{byNumber: m}
}

func (f *fakeConfigStore) Get(_ context.Context, companyID uuid.UUID) (Config, error) {
	for _, c := range f.byNumber {
		if c.CompanyID == companyID {
			return c, nil
		}
	}
	return Config{}, ErrTenantNotConfigured
}

func (f *fakeConfigStore) LookupBySendingNumber(_ context.Context, provider ProviderType, number string) (Config, error) {
	c, ok := f.byNumber[string(provider)+"|"+number]
	if !ok {
		return Config{}, ErrTenantNotConfigured
	}
	return c, nil
}

func (f *fakeConfigStore) Upsert(_ context.Context, c Config) error {
	f.byNumber[string(c.Provider)+"|"+c.SendingNumber] = c
	return nil
}
func (f *fakeConfigStore) Deactivate(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeConfigStore) List(_ context.Context) ([]Config, error) {
	out := []Config{}
	for _, c := range f.byNumber {
		out = append(out, c)
	}
	return out, nil
}

// spyProvider wraps a real Provider (built through the package's own
// factories) and counts calls to the methods this TRD's security property
// hinges on. Embedding Provider promotes SendSMS/InitiateCall/etc
// unchanged; only the three methods below are intercepted.
type spyProvider struct {
	Provider
	mu          sync.Mutex
	verifyCalls int
	parseCalls  int
}

func (s *spyProvider) VerifyWebhookSignature(r *http.Request, body []byte) error {
	s.mu.Lock()
	s.verifyCalls++
	s.mu.Unlock()
	return s.Provider.VerifyWebhookSignature(r, body)
}

func (s *spyProvider) ParseStatusWebhook(r *http.Request, body []byte) (StatusEvent, error) {
	s.mu.Lock()
	s.parseCalls++
	s.mu.Unlock()
	return s.Provider.ParseStatusWebhook(r, body)
}

func (s *spyProvider) ParseInboundSMS(r *http.Request, body []byte) (InboundSMS, error) {
	s.mu.Lock()
	s.parseCalls++
	s.mu.Unlock()
	return s.Provider.ParseInboundSMS(r, body)
}

// spyTwilioFactory registers a Twilio-backed spyProvider per Config,
// recording each built instance into spies (keyed by CompanyID) so a test
// can inspect call counts after the request completes.
func spyTwilioFactory(spies map[uuid.UUID]*spyProvider) ProviderFactory {
	inner := NewTwilioFactory()
	return func(c Config) (Provider, error) {
		p, err := inner(c)
		if err != nil {
			return nil, err
		}
		sp := &spyProvider{Provider: p}
		spies[c.CompanyID] = sp
		return sp, nil
	}
}

type fakeSMSStatusSink struct{ calls []StatusEvent }

func (f *fakeSMSStatusSink) HandleSMSStatus(_ context.Context, _ Config, ev StatusEvent) error {
	f.calls = append(f.calls, ev)
	return nil
}

type fakeCallStatusSink struct{ calls []StatusEvent }

func (f *fakeCallStatusSink) HandleCallStatus(_ context.Context, _ Config, ev StatusEvent) error {
	f.calls = append(f.calls, ev)
	return nil
}

type fakeInboundSMSSink struct{ calls []InboundSMS }

func (f *fakeInboundSMSSink) HandleInboundSMS(_ context.Context, _ Config, msg InboundSMS) error {
	f.calls = append(f.calls, msg)
	return nil
}

type fakeAuditWriter struct {
	writes   []string
	failNext bool
}

func (f *fakeAuditWriter) Write(_ context.Context, source string, _ []byte) error {
	f.writes = append(f.writes, source)
	if f.failNext {
		return errors.New("audit backend unavailable")
	}
	return nil
}

// newSignedTwilioRequest builds a POST request over form, signed for reqURL
// with token, with the forwarded-proto/host headers reconstructURL expects
// (mirrors a request that crossed a reverse proxy/CDN edge).
func newSignedTwilioRequest(reqURL string, form url.Values, token string) *http.Request {
	sig := signTwilio(reqURL, form, token)
	r := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", sig)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")
	r.Host = "internal"
	return r
}

const webhookTestURL = "https://api.example.com/webhooks/telephony/twilio/status"

// --- ReadWebhookBody ---------------------------------------------------

func TestReadWebhookBody_TooLarge(t *testing.T) {
	big := strings.Repeat("a", MaxWebhookBodyBytes+1)
	r := httptest.NewRequest(http.MethodPost, "/webhooks/telephony/twilio/status", strings.NewReader(big))

	_, err := ReadWebhookBody(r)
	if !errors.Is(err, ErrWebhookBodyTooLarge) {
		t.Fatalf("err = %v; want ErrWebhookBodyTooLarge", err)
	}
}

func TestReadWebhookBody_RestoresExactBytesForRereading(t *testing.T) {
	form := url.Values{}
	form.Set("To", "+15555550100")
	want := form.Encode()

	r := httptest.NewRequest(http.MethodPost, "/webhooks/telephony/twilio/status", strings.NewReader(want))
	got, err := ReadWebhookBody(r)
	if err != nil {
		t.Fatalf("ReadWebhookBody: %v", err)
	}
	if string(got) != want {
		t.Fatalf("returned body = %q; want %q", got, want)
	}

	// r.Body must still be readable afterwards, with the identical bytes.
	reread, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reread r.Body: %v", err)
	}
	if string(reread) != want {
		t.Fatalf("r.Body after ReadWebhookBody = %q; want %q", reread, want)
	}
}

// --- the security property: tamper -> rejected -> never parsed ------------

func TestWebhookHandler_TamperedBody_RejectedBeforeParse(t *testing.T) {
	token := "wh-secret"
	cfg := Config{
		CompanyID: uuid.New(), Provider: ProviderTwilio,
		AccountSID: "AC1", AuthToken: token, SendingNumber: "+15555550100",
	}
	store := newFakeConfigStore(cfg)
	spies := map[uuid.UUID]*spyProvider{}
	reg := NewRegistry()
	reg.Register(ProviderTwilio, spyTwilioFactory(spies))

	smsSink := &fakeSMSStatusSink{}
	callSink := &fakeCallStatusSink{}
	h := &WebhookHandler{Store: store, Registry: reg, SMSStatus: smsSink, CallStatus: callSink}

	// The signature covers this exact form...
	signedForm := url.Values{}
	signedForm.Set("MessageSid", "SM1")
	signedForm.Set("MessageStatus", "delivered")
	signedForm.Set("From", "+15555550199")
	signedForm.Set("To", cfg.SendingNumber)
	sig := signTwilio(webhookTestURL, signedForm, token)

	// ...but the bytes actually delivered on the wire differ (MessageStatus
	// flipped delivered -> failed) while replaying the original signature
	// header, simulating a body tampered in transit.
	tamperedForm := url.Values{}
	tamperedForm.Set("MessageSid", "SM1")
	tamperedForm.Set("MessageStatus", "failed")
	tamperedForm.Set("From", "+15555550199")
	tamperedForm.Set("To", cfg.SendingNumber)

	r := httptest.NewRequest(http.MethodPost, webhookTestURL, strings.NewReader(tamperedForm.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", sig)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")
	r.Host = "internal"

	w := httptest.NewRecorder()
	h.StatusHandler(ProviderTwilio)(w, r)

	if w.Code >= 200 && w.Code < 300 {
		t.Fatalf("tampered body: status = %d; want a non-2xx rejection", w.Code)
	}
	if len(smsSink.calls) != 0 || len(callSink.calls) != 0 {
		t.Fatalf("a sink was invoked for an unverified payload: sms=%d call=%d",
			len(smsSink.calls), len(callSink.calls))
	}
	sp, ok := spies[cfg.CompanyID]
	if !ok {
		t.Fatal("provider was never constructed for the resolved tenant")
	}
	if sp.verifyCalls == 0 {
		t.Error("VerifyWebhookSignature was never called")
	}
	if sp.parseCalls != 0 {
		t.Errorf("ParseStatusWebhook called %d times against a tampered body; want 0 -- "+
			"an unverified payload must never be parsed", sp.parseCalls)
	}
}

func TestWebhookHandler_InboundSMS_TamperedBody_RejectedBeforeParse(t *testing.T) {
	token := "wh-secret"
	cfg := Config{
		CompanyID: uuid.New(), Provider: ProviderTwilio,
		AccountSID: "AC1", AuthToken: token, SendingNumber: "+15555550100",
	}
	store := newFakeConfigStore(cfg)
	spies := map[uuid.UUID]*spyProvider{}
	reg := NewRegistry()
	reg.Register(ProviderTwilio, spyTwilioFactory(spies))

	inboundSink := &fakeInboundSMSSink{}
	h := &WebhookHandler{Store: store, Registry: reg, Inbound: inboundSink}

	inboundURL := "https://api.example.com/webhooks/telephony/twilio/sms_inbound"
	signedForm := url.Values{}
	signedForm.Set("MessageSid", "SM9")
	signedForm.Set("From", "+15555550199")
	signedForm.Set("To", cfg.SendingNumber)
	signedForm.Set("Body", "hello")
	sig := signTwilio(inboundURL, signedForm, token)

	tamperedForm := url.Values{}
	tamperedForm.Set("MessageSid", "SM9")
	tamperedForm.Set("From", "+15555550199")
	tamperedForm.Set("To", cfg.SendingNumber)
	tamperedForm.Set("Body", "STOP") // attacker-flipped payload

	r := httptest.NewRequest(http.MethodPost, inboundURL, strings.NewReader(tamperedForm.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", sig)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")
	r.Host = "internal"

	w := httptest.NewRecorder()
	h.InboundSMSHandler(ProviderTwilio)(w, r)

	if w.Code >= 200 && w.Code < 300 {
		t.Fatalf("tampered inbound body: status = %d; want non-2xx", w.Code)
	}
	if len(inboundSink.calls) != 0 {
		t.Fatalf("InboundSMSSink invoked for an unverified payload: %d calls", len(inboundSink.calls))
	}
	sp := spies[cfg.CompanyID]
	if sp == nil || sp.parseCalls != 0 {
		t.Errorf("ParseInboundSMS was called on a tampered body; want 0 calls")
	}
}

// --- the security property: resolution uses the receiving number ----------

func TestVerifyAndResolve_ResolvesByReceivingNumberNotBodyField(t *testing.T) {
	tokenA := "secret-A"
	tokenB := "secret-B"
	cfgA := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "ACA", AuthToken: tokenA, SendingNumber: "+15550001111"}
	cfgB := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "ACB", AuthToken: tokenB, SendingNumber: "+15550002222"}
	store := newFakeConfigStore(cfgA, cfgB)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())

	// A genuine request from tenant A's number, signed with tenant A's own
	// secret, but carrying a SPURIOUS "CompanyID" field pointing at tenant
	// B -- simulating an attempt (bug or attacker) to steer resolution
	// through an arbitrary body field instead of the receiving number.
	form := url.Values{}
	form.Set("MessageSid", "SM1")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+15555550199")
	form.Set("To", cfgA.SendingNumber)
	form.Set("CompanyID", cfgB.CompanyID.String())

	r := newSignedTwilioRequest(webhookTestURL, form, tokenA)
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	_, resolved, err := VerifyAndResolve(context.Background(), store, reg, ProviderTwilio, r, body)
	if err != nil {
		t.Fatalf("VerifyAndResolve: %v", err)
	}
	if resolved.CompanyID != cfgA.CompanyID {
		t.Fatalf("resolved company = %s; want tenant A (%s) -- resolution must key off "+
			"the receiving number, not the spurious CompanyID field", resolved.CompanyID, cfgA.CompanyID)
	}

	// Cross-tenant attack: "To" still names tenant A's number, but the
	// request is signed with tenant B's secret -- the only credential an
	// attacker who is really tenant B actually possesses. Resolution still
	// targets tenant A (by receiving number); verification against tenant
	// A's real secret then fails, so the forged request is rejected.
	forged := url.Values{}
	forged.Set("MessageSid", "SM2")
	forged.Set("MessageStatus", "delivered")
	forged.Set("From", "+15555550199")
	forged.Set("To", cfgA.SendingNumber)

	r2 := newSignedTwilioRequest(webhookTestURL, forged, tokenB)
	body2, _ := io.ReadAll(r2.Body)
	r2.Body = io.NopCloser(bytes.NewReader(body2))

	_, _, err = VerifyAndResolve(context.Background(), store, reg, ProviderTwilio, r2, body2)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("cross-tenant forged request: err = %v; want ErrInvalidSignature", err)
	}
}

// --- StatusEvent.Kind disambiguation: both SMS and call status ------------

func TestWebhookHandler_StatusHandler_SMSKind(t *testing.T) {
	token := "wh-secret"
	cfg := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "AC1", AuthToken: token, SendingNumber: "+15555550100"}
	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())

	smsSink := &fakeSMSStatusSink{}
	callSink := &fakeCallStatusSink{}
	audit := &fakeAuditWriter{}
	h := &WebhookHandler{Store: store, Registry: reg, SMSStatus: smsSink, CallStatus: callSink, Audit: audit}

	form := url.Values{}
	form.Set("MessageSid", "SM-st")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+15555550199")
	form.Set("To", cfg.SendingNumber)

	r := newSignedTwilioRequest(webhookTestURL, form, token)
	w := httptest.NewRecorder()
	h.StatusHandler(ProviderTwilio)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if len(callSink.calls) != 0 {
		t.Errorf("CallStatusSink invoked for an SMS status event")
	}
	if len(smsSink.calls) != 1 {
		t.Fatalf("SMSStatusSink calls = %d; want 1", len(smsSink.calls))
	}
	ev := smsSink.calls[0]
	if ev.Kind != "sms" || ev.SID != "SM-st" || ev.Status != "delivered" {
		t.Errorf("unexpected event: %+v", ev)
	}
	if len(audit.writes) != 1 || audit.writes[0] != "status:twilio" {
		t.Errorf("audit writes = %v; want one write tagged status:twilio", audit.writes)
	}
}

func TestWebhookHandler_StatusHandler_CallStatusTransitions(t *testing.T) {
	token := "wh-secret"
	cfg := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "AC1", AuthToken: token, SendingNumber: "+15555550100"}
	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())

	// Reconciled against justinforme's call_status_handler.go, which
	// exercises a wider set of carrier CallStatus transitions
	// (busy/no-answer/canceled) than politihub's ported test suite, which
	// only covered "completed". mapStatus (models.go) already maps each of
	// these distinctly; this proves the webhook dispatch path carries every
	// transition through to the CallStatusSink unchanged.
	cases := []struct {
		name         string
		callStatus   string
		callDuration string
		wantStatus   string
		wantDuration int
	}{
		{"completed_with_duration", "completed", "42", "completed", 42},
		{"busy", "busy", "", "busy", 0},
		{"no_answer", "no-answer", "", "no_answer", 0},
		{"canceled", "canceled", "", "canceled", 0},
		{"ringing", "ringing", "", "ringing", 0},
		{"in_progress", "in-progress", "", "in_progress", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smsSink := &fakeSMSStatusSink{}
			callSink := &fakeCallStatusSink{}
			h := &WebhookHandler{Store: store, Registry: reg, SMSStatus: smsSink, CallStatus: callSink}

			form := url.Values{}
			form.Set("CallSid", "CA-"+tc.name)
			form.Set("CallStatus", tc.callStatus)
			if tc.callDuration != "" {
				form.Set("CallDuration", tc.callDuration)
			}
			form.Set("From", "+15555550199")
			form.Set("To", cfg.SendingNumber)

			r := newSignedTwilioRequest(webhookTestURL, form, token)
			w := httptest.NewRecorder()
			h.StatusHandler(ProviderTwilio)(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
			}
			if len(smsSink.calls) != 0 {
				t.Errorf("SMSStatusSink invoked for a call status event")
			}
			if len(callSink.calls) != 1 {
				t.Fatalf("CallStatusSink calls = %d; want 1", len(callSink.calls))
			}
			ev := callSink.calls[0]
			if ev.Kind != "call" {
				t.Errorf("Kind = %q; want call", ev.Kind)
			}
			if ev.Status != tc.wantStatus {
				t.Errorf("Status = %q; want %q", ev.Status, tc.wantStatus)
			}
			if ev.DurationS != tc.wantDuration {
				t.Errorf("DurationS = %d; want %d", ev.DurationS, tc.wantDuration)
			}
		})
	}
}

// --- inbound SMS happy path -------------------------------------------

func TestWebhookHandler_InboundSMS_OK(t *testing.T) {
	token := "wh-secret"
	cfg := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "AC1", AuthToken: token, SendingNumber: "+15555550100"}
	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())

	inboundSink := &fakeInboundSMSSink{}
	audit := &fakeAuditWriter{}
	h := &WebhookHandler{Store: store, Registry: reg, Inbound: inboundSink, Audit: audit}

	inboundURL := "https://api.example.com/webhooks/telephony/twilio/sms_inbound"
	form := url.Values{}
	form.Set("MessageSid", "SM-in")
	form.Set("From", "+15555550199")
	form.Set("To", cfg.SendingNumber)
	form.Set("Body", "STOP")

	r := newSignedTwilioRequest(inboundURL, form, token)
	w := httptest.NewRecorder()
	h.InboundSMSHandler(ProviderTwilio)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<Response>") {
		t.Errorf("body = %q; want TwiML response", w.Body.String())
	}
	if len(inboundSink.calls) != 1 {
		t.Fatalf("InboundSMSSink calls = %d; want 1", len(inboundSink.calls))
	}
	msg := inboundSink.calls[0]
	if msg.MessageSID != "SM-in" || msg.Body != "STOP" {
		t.Errorf("unexpected message: %+v", msg)
	}
	if len(audit.writes) != 1 || audit.writes[0] != "sms_inbound:twilio" {
		t.Errorf("audit writes = %v", audit.writes)
	}

	// A failing audit writer must never block the response or the sink.
	audit.failNext = true
	inboundSink2 := &fakeInboundSMSSink{}
	h2 := &WebhookHandler{Store: store, Registry: reg, Inbound: inboundSink2, Audit: audit}
	form.Set("MessageSid", "SM-in-2")
	r2 := newSignedTwilioRequest(inboundURL, form, token)
	w2 := httptest.NewRecorder()
	h2.InboundSMSHandler(ProviderTwilio)(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status after audit failure = %d; want 200 (audit is best-effort)", w2.Code)
	}
	if len(inboundSink2.calls) != 1 {
		t.Fatalf("sink still expected to run after an audit failure; calls = %d", len(inboundSink2.calls))
	}
}

// --- rejection paths ----------------------------------------------------

func TestWebhookHandler_UnknownReceivingNumber_404(t *testing.T) {
	cfg := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "AC1", AuthToken: "tok", SendingNumber: "+15555550100"}
	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())

	smsSink := &fakeSMSStatusSink{}
	h := &WebhookHandler{Store: store, Registry: reg, SMSStatus: smsSink}

	form := url.Values{}
	form.Set("MessageSid", "SM1")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+15555550199")
	form.Set("To", "+1900NOTCONFIGURED")

	// Note: no valid signature is even obtainable here -- there is no
	// tenant secret for an unconfigured number -- so this also proves an
	// attacker gets nothing more useful than "unknown number" for a number
	// nobody owns.
	r := newSignedTwilioRequest(webhookTestURL, form, "whatever")
	w := httptest.NewRecorder()
	h.StatusHandler(ProviderTwilio)(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", w.Code)
	}
	if len(smsSink.calls) != 0 {
		t.Errorf("sink invoked despite unresolved tenant")
	}
}

func TestWebhookHandler_BodyTooLarge_413(t *testing.T) {
	store := newFakeConfigStore()
	reg := NewRegistry()
	h := &WebhookHandler{Store: store, Registry: reg}

	big := strings.Repeat("a", MaxWebhookBodyBytes+1)
	r := httptest.NewRequest(http.MethodPost, webhookTestURL, strings.NewReader(big))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.StatusHandler(ProviderTwilio)(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want 413", w.Code)
	}
}

func TestWebhookHandler_BadSignature_Forbidden(t *testing.T) {
	cfg := Config{CompanyID: uuid.New(), Provider: ProviderTwilio, AccountSID: "AC1", AuthToken: "real-secret", SendingNumber: "+15555550100"}
	store := newFakeConfigStore(cfg)
	reg := NewRegistry()
	reg.Register(ProviderTwilio, NewTwilioFactory())
	h := &WebhookHandler{Store: store, Registry: reg}

	form := url.Values{}
	form.Set("MessageSid", "SM1")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+15555550199")
	form.Set("To", cfg.SendingNumber)

	r := httptest.NewRequest(http.MethodPost, webhookTestURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", "not-a-real-signature")

	w := httptest.NewRecorder()
	h.StatusHandler(ProviderTwilio)(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", w.Code)
	}
}
