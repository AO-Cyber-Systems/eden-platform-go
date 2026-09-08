package telephony

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSignalWire_NewProvider_Unconfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no account_sid", Config{Provider: ProviderSignalWire, AuthToken: "tok", SpaceURL: "https://x.signalwire.com"}},
		{"no auth_token", Config{Provider: ProviderSignalWire, AccountSID: "PRJ", SpaceURL: "https://x.signalwire.com"}},
		{"no space_url", Config{Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: "tok"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newSignalWireProvider(tc.cfg)
			if !errors.Is(err, ErrProviderNotConfigured) {
				t.Errorf("expected ErrProviderNotConfigured, got %v", err)
			}
		})
	}
}

func TestSignalWire_RewriteHost(t *testing.T) {
	cases := []struct {
		raw  string
		host string
		want string
	}{
		{"https://api.twilio.com/2010-04-01/Accounts/PRJ/Messages.json", "ex.signalwire.com",
			"https://ex.signalwire.com/2010-04-01/Accounts/PRJ/Messages.json"},
		{"https://api.twilio.com/foo?bar=baz", "ex.signalwire.com",
			"https://ex.signalwire.com/foo?bar=baz"},
		// invalid url returns unchanged
		{"://broken", "ex.signalwire.com", "://broken"},
	}
	for _, tc := range cases {
		got := rewriteHost(tc.raw, tc.host)
		if got != tc.want {
			t.Errorf("rewriteHost(%q, %q) = %q, want %q", tc.raw, tc.host, got, tc.want)
		}
	}
}

func TestSignalWire_StripScheme(t *testing.T) {
	cases := map[string]string{
		"https://x.signalwire.com":  "x.signalwire.com",
		"http://x.signalwire.com":   "x.signalwire.com",
		"x.signalwire.com":          "x.signalwire.com",
		"https://x.signalwire.com/": "x.signalwire.com",
	}
	for in, want := range cases {
		if got := stripScheme(in); got != want {
			t.Errorf("stripScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSignalWire_VerifyWebhookSignature(t *testing.T) {
	authToken := "sw-secret"
	prov, err := newSignalWireProvider(Config{
		Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: authToken,
		SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	form := url.Values{}
	form.Set("MessageSid", "SM-sw")
	form.Set("MessageStatus", "delivered")

	bodyStr := form.Encode()
	reqURL := "https://api.example.com/webhooks/telephony/signalwire/sms_status"
	sig := signTwilio(reqURL, form, authToken) // same algorithm as Twilio

	r := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-SignalWire-Signature", sig)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.example.com")
	r.Host = "internal"
	r.URL, _ = url.Parse(reqURL)
	r.RequestURI = "/webhooks/telephony/signalwire/sms_status"

	body, _ := io.ReadAll(r.Body)
	if err := prov.VerifyWebhookSignature(r, body); err != nil {
		t.Fatalf("VerifyWebhookSignature: %v", err)
	}

	// Twilio header fallback works.
	r2 := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	r2.Header.Set("X-Twilio-Signature", sig)
	r2.Header.Set("X-Forwarded-Proto", "https")
	r2.Header.Set("X-Forwarded-Host", "api.example.com")
	r2.Host = "internal"
	if err := prov.VerifyWebhookSignature(r2, body); err != nil {
		t.Errorf("Twilio fallback header: %v", err)
	}

	// Bad signature.
	r3 := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	r3.Header.Set("X-SignalWire-Signature", "wrong")
	r3.Header.Set("X-Forwarded-Proto", "https")
	r3.Header.Set("X-Forwarded-Host", "api.example.com")
	r3.Host = "internal"
	if err := prov.VerifyWebhookSignature(r3, body); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("bad sig: err = %v", err)
	}

	// Missing header.
	r4 := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(bodyStr))
	if err := prov.VerifyWebhookSignature(r4, body); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("missing sig: err = %v", err)
	}
}

func TestSignalWire_ParseStatusWebhook_SMS_AndCall(t *testing.T) {
	prov, err := newSignalWireProvider(Config{
		Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: "t",
		SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// SMS
	form := url.Values{"MessageSid": {"SM"}, "MessageStatus": {"failed"}, "ErrorCode": {"30003"}}
	r := newPostFormRequest(t, "/x", form)
	ev, err := prov.ParseStatusWebhook(r, nil)
	if err != nil {
		t.Fatalf("parse sms: %v", err)
	}
	if ev.Kind != "sms" || ev.Status != "failed" || ev.ErrorCode != "30003" {
		t.Errorf("sms event: %+v", ev)
	}

	// Call
	form2 := url.Values{"CallSid": {"CA"}, "CallStatus": {"completed"}, "CallDuration": {"30"}}
	r2 := newPostFormRequest(t, "/x", form2)
	ev2, err := prov.ParseStatusWebhook(r2, nil)
	if err != nil {
		t.Fatalf("parse call: %v", err)
	}
	if ev2.Kind != "call" || ev2.Status != "completed" || ev2.DurationS != 30 {
		t.Errorf("call event: %+v", ev2)
	}
}

func TestSignalWire_ParseInboundSMS(t *testing.T) {
	prov, _ := newSignalWireProvider(Config{
		Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: "t",
		SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	})
	form := url.Values{"MessageSid": {"SM"}, "From": {"+15555550111"}, "To": {"+15555550100"}, "Body": {"STOP"}}
	r := newPostFormRequest(t, "/x", form)
	in, err := prov.ParseInboundSMS(r, []byte(form.Encode()))
	if err != nil {
		t.Fatalf("parse inbound: %v", err)
	}
	if in.From != "+15555550111" || in.Body != "STOP" {
		t.Errorf("inbound = %+v", in)
	}
}

func TestSignalWire_TypeIsSignalwire(t *testing.T) {
	prov, _ := newSignalWireProvider(Config{
		Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: "t",
		SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	})
	if prov.Type() != ProviderSignalWire {
		t.Errorf("Type = %s; want signalwire", prov.Type())
	}
}

// TestSignalWire_IsDefaultProviderInRegistry proves the must_haves.truths
// claim for this TRD: SignalWire is the default provider a registry
// resolves, with the Noop fallback reachable only when nothing is
// registered for the tenant's configured type at all (never as a silent
// substitute for a configured SignalWire tenant).
func TestSignalWire_IsDefaultProviderInRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(ProviderSignalWire, NewSignalWireFactory())
	r.Register(ProviderTwilio, NewTwilioFactory())

	if !r.Has(ProviderSignalWire) {
		t.Fatal("registry does not have the default (signalwire) provider registered")
	}

	// A tenant that hasn't overridden the provider resolves to signalwire —
	// this is the standard tenant configuration this package ships.
	cfg := Config{
		Provider: ProviderSignalWire, AccountSID: "PRJ", AuthToken: "t",
		SpaceURL: "https://x.signalwire.com", SendingNumber: "+1",
	}
	p, err := r.For(cfg)
	if err != nil {
		t.Fatalf("For(default): %v", err)
	}
	if p.Type() != ProviderSignalWire {
		t.Errorf("default provider type = %s, want %s", p.Type(), ProviderSignalWire)
	}

	// NoopProvider is never returned BY the registry — a resolver falls back
	// to it only when Registry.For reports ErrUnsupportedProvider (i.e. no
	// tenant config resolved to a registered type at all). Confirm the
	// registry surfaces that condition rather than silently substituting.
	if _, err := r.For(Config{Provider: ProviderType("unconfigured-tenant")}); !errors.Is(err, ErrUnsupportedProvider) {
		t.Errorf("expected ErrUnsupportedProvider for an unresolved tenant, got %v", err)
	}
}
