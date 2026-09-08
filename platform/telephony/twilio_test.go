package telephony

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio/SignalWire use SHA-1 HMAC by spec.
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func TestTwilio_NewProvider_Unconfigured(t *testing.T) {
	_, err := newTwilioProvider(Config{Provider: ProviderTwilio})
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("expected ErrProviderNotConfigured, got %v", err)
	}
}

func TestTwilio_VerifyWebhookSignature(t *testing.T) {
	authToken := "secret-token"
	prov, err := newTwilioProvider(Config{
		Provider:      ProviderTwilio,
		AccountSID:    "AC1234",
		AuthToken:     authToken,
		SendingNumber: "+15555550100",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	form := url.Values{}
	form.Set("MessageSid", "SM123")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+15555550199")
	form.Set("To", "+15555550100")

	bodyStr := form.Encode()
	reqURL := "https://api.example.com/webhooks/telephony/twilio/sms_status"

	sig := signTwilio(reqURL, form, authToken)

	r := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Twilio-Signature", sig)
	// Force scheme/host through forwarded headers (typical reverse-proxy/CDN path).
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")
	r.Host = "internal" // would otherwise be "example.com"
	r.URL, _ = url.Parse(reqURL)
	r.RequestURI = "/webhooks/telephony/twilio/sms_status"

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	if err := prov.VerifyWebhookSignature(r, body); err != nil {
		t.Fatalf("VerifyWebhookSignature: %v", err)
	}

	// Bad signature → error.
	r2 := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	r2.Header.Set("X-Twilio-Signature", "deadbeef")
	r2.Header.Set("X-Forwarded-Proto", "https")
	r2.Header.Set("X-Forwarded-Host", "api.example.com")
	r2.Host = "internal"
	if err := prov.VerifyWebhookSignature(r2, body); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("bad sig: err = %v", err)
	}

	// Missing header → error.
	r3 := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	if err := prov.VerifyWebhookSignature(r3, body); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("missing sig: err = %v", err)
	}
}

func TestTwilio_ParseStatusWebhook_SMS(t *testing.T) {
	prov := makeTwilioForTest(t)
	form := url.Values{}
	form.Set("MessageSid", "SM-abc")
	form.Set("MessageStatus", "delivered")
	form.Set("From", "+1")
	form.Set("To", "+2")

	r := newPostFormRequest(t, "/webhooks/telephony/twilio/sms_status", form)
	ev, err := prov.ParseStatusWebhook(r, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.Kind != "sms" {
		t.Errorf("kind = %s; want sms", ev.Kind)
	}
	if ev.SID != "SM-abc" || ev.Status != "delivered" {
		t.Errorf("event = %+v", ev)
	}
}

func TestTwilio_ParseStatusWebhook_Call(t *testing.T) {
	prov := makeTwilioForTest(t)
	form := url.Values{}
	form.Set("CallSid", "CA-xyz")
	form.Set("CallStatus", "completed")
	form.Set("CallDuration", "42")
	form.Set("From", "+1")
	form.Set("To", "+2")

	r := newPostFormRequest(t, "/webhooks/telephony/twilio/call_status", form)
	ev, err := prov.ParseStatusWebhook(r, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.Kind != "call" || ev.SID != "CA-xyz" || ev.Status != "completed" || ev.DurationS != 42 {
		t.Errorf("event = %+v", ev)
	}
}

func TestTwilio_ParseStatusWebhook_FailedMaps(t *testing.T) {
	prov := makeTwilioForTest(t)
	for _, raw := range []string{"failed", "undelivered"} {
		form := url.Values{"MessageSid": {"SM"}, "MessageStatus": {raw}}
		r := newPostFormRequest(t, "/x", form)
		ev, err := prov.ParseStatusWebhook(r, nil)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if ev.Status != "failed" {
			t.Errorf("status %q -> %s; want failed", raw, ev.Status)
		}
	}
}

func TestTwilio_ParseInboundSMS(t *testing.T) {
	prov := makeTwilioForTest(t)
	form := url.Values{}
	form.Set("MessageSid", "SM-in")
	form.Set("From", "+15555550111")
	form.Set("To", "+15555550100")
	form.Set("Body", "STOP")

	r := newPostFormRequest(t, "/webhooks/telephony/twilio/sms_inbound", form)
	rawBody := []byte(form.Encode())
	in, err := prov.ParseInboundSMS(r, rawBody)
	if err != nil {
		t.Fatalf("parse inbound: %v", err)
	}
	if in.MessageSID != "SM-in" || in.From != "+15555550111" || in.To != "+15555550100" || in.Body != "STOP" {
		t.Errorf("inbound = %+v", in)
	}
	if !bytes.Equal(in.Raw, rawBody) {
		t.Errorf("raw body not preserved")
	}
}

func TestTwilio_ParseStatusWebhook_MissingSIDs(t *testing.T) {
	prov := makeTwilioForTest(t)
	form := url.Values{"Foo": {"bar"}}
	r := newPostFormRequest(t, "/x", form)
	if _, err := prov.ParseStatusWebhook(r, nil); err == nil {
		t.Error("expected error for missing MessageSid + CallSid")
	}
}

// TestRegistry_ThirdProviderExtendsWithoutRegistryChanges proves the
// must_haves.truths claim for this TRD: a third Provider implementation
// plugs into the same Registry the two shipped adapters use, requiring no
// change to Provider, Registry, or any resolver — only an implementation of
// the interface plus a Register call.
func TestRegistry_ThirdProviderExtendsWithoutRegistryChanges(t *testing.T) {
	r := NewRegistry()
	r.Register(ProviderSignalWire, NewSignalWireFactory())
	r.Register(ProviderTwilio, NewTwilioFactory())

	const thirdProviderType ProviderType = "fake-third"
	r.Register(thirdProviderType, func(c Config) (Provider, error) {
		return &fakeThirdProvider{cfg: c}, nil
	})

	// The two shipped adapters are untouched by registering a third.
	if !r.Has(ProviderSignalWire) || !r.Has(ProviderTwilio) {
		t.Fatal("registering a third provider disturbed the two shipped adapters")
	}

	p, err := r.For(Config{Provider: thirdProviderType, AccountSID: "whatever"})
	if err != nil {
		t.Fatalf("For(third): %v", err)
	}
	if p.Type() != thirdProviderType {
		t.Errorf("third provider type = %s, want %s", p.Type(), thirdProviderType)
	}
	if _, err := p.SendSMS(context.Background(), "+1", "hi", ""); err != nil {
		t.Errorf("third provider SendSMS: %v", err)
	}
}

// fakeThirdProvider is an in-test-only Provider implementation used solely
// to prove the seam extends without touching registry/resolver code. It is
// not shipped as a real backend.
type fakeThirdProvider struct{ cfg Config }

func (f *fakeThirdProvider) Type() ProviderType { return ProviderType("fake-third") }
func (f *fakeThirdProvider) SendSMS(_ context.Context, _, _, _ string) (SMSResult, error) {
	return SMSResult{MessageSID: "fake-sms", Status: "queued"}, nil
}
func (f *fakeThirdProvider) InitiateCall(_ context.Context, _, _, _ string) (CallResult, error) {
	return CallResult{CallSID: "fake-call", Status: "queued"}, nil
}
func (f *fakeThirdProvider) InitiateBridge(_ context.Context, _, _, _, _ string) (CallResult, error) {
	return CallResult{CallSID: "fake-bridge", Status: "queued"}, nil
}
func (f *fakeThirdProvider) VerifyWebhookSignature(_ *http.Request, _ []byte) error { return nil }
func (f *fakeThirdProvider) ParseStatusWebhook(_ *http.Request, _ []byte) (StatusEvent, error) {
	return StatusEvent{}, nil
}
func (f *fakeThirdProvider) ParseInboundSMS(_ *http.Request, _ []byte) (InboundSMS, error) {
	return InboundSMS{}, nil
}

// helpers ---

func makeTwilioForTest(t *testing.T) Provider {
	t.Helper()
	prov, err := newTwilioProvider(Config{
		Provider: ProviderTwilio, AccountSID: "AC", AuthToken: "tok", SendingNumber: "+1",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return prov
}

func newPostFormRequest(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// signTwilio computes the Twilio HMAC-SHA1 signature for the given URL +
// form. Mirrors the algorithm twilio-go's RequestValidator uses; kept here
// only to construct valid test fixtures — production signature verification
// always goes through twilio-go's own RequestValidator, never this helper.
func signTwilio(reqURL string, form url.Values, token string) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(reqURL)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
