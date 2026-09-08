package telephony

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// stubProvider is a minimal Provider used by the factory tests.
type stubProvider struct {
	pType ProviderType
}

func (s stubProvider) Type() ProviderType { return s.pType }
func (stubProvider) SendSMS(_ context.Context, _, _, _ string) (SMSResult, error) {
	return SMSResult{}, nil
}
func (stubProvider) InitiateCall(_ context.Context, _, _, _ string) (CallResult, error) {
	return CallResult{}, nil
}
func (stubProvider) InitiateBridge(_ context.Context, _, _, _, _ string) (CallResult, error) {
	return CallResult{}, nil
}
func (stubProvider) VerifyWebhookSignature(_ *http.Request, _ []byte) error { return nil }
func (stubProvider) ParseStatusWebhook(_ *http.Request, _ []byte) (StatusEvent, error) {
	return StatusEvent{}, nil
}
func (stubProvider) ParseInboundSMS(_ *http.Request, _ []byte) (InboundSMS, error) {
	return InboundSMS{}, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	called := 0
	r.Register(ProviderTwilio, func(c Config) (Provider, error) {
		called++
		if c.Provider != ProviderTwilio {
			t.Errorf("factory got provider=%s, want twilio", c.Provider)
		}
		return stubProvider{pType: ProviderTwilio}, nil
	})

	if !r.Has(ProviderTwilio) {
		t.Fatalf("Has(twilio) = false; want true")
	}
	if r.Has(ProviderSignalWire) {
		t.Fatalf("Has(signalwire) = true; want false")
	}

	p, err := r.For(Config{Provider: ProviderTwilio, CompanyID: uuid.New()})
	if err != nil {
		t.Fatalf("For(twilio): %v", err)
	}
	if p == nil {
		t.Fatal("For(twilio) returned nil provider")
	}
	if p.Type() != ProviderTwilio {
		t.Errorf("provider.Type() = %s, want twilio", p.Type())
	}
	if called != 1 {
		t.Errorf("factory called %d times, want 1", called)
	}
}

func TestRegistry_Unregistered(t *testing.T) {
	r := NewRegistry()
	_, err := r.For(Config{Provider: ProviderSignalWire})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Errorf("err = %v, want ErrUnsupportedProvider", err)
	}
}

func TestRegistry_Override(t *testing.T) {
	r := NewRegistry()
	r.Register(ProviderTwilio, func(Config) (Provider, error) { return stubProvider{pType: "v1"}, nil })
	r.Register(ProviderTwilio, func(Config) (Provider, error) { return stubProvider{pType: "v2"}, nil })

	p, err := r.For(Config{Provider: ProviderTwilio})
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if p.Type() != "v2" {
		t.Errorf("override did not take effect; type=%s", p.Type())
	}
}

func TestRegistry_NilFactoryPanics(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("Register(nil) did not panic")
		}
	}()
	r.Register(ProviderTwilio, nil)
}

func TestNoopProvider_AllErrPaths(t *testing.T) {
	var p Provider = NoopProvider{}
	if _, err := p.SendSMS(context.Background(), "+1", "hi", ""); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("SendSMS err = %v", err)
	}
	if _, err := p.InitiateCall(context.Background(), "+1", "+2", ""); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("InitiateCall err = %v", err)
	}
	if _, err := p.InitiateBridge(context.Background(), "+1", "+2", "+3", ""); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("InitiateBridge err = %v", err)
	}
	if err := p.VerifyWebhookSignature(nil, nil); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("VerifyWebhookSignature err = %v", err)
	}
	if _, err := p.ParseStatusWebhook(nil, nil); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("ParseStatusWebhook err = %v", err)
	}
	if _, err := p.ParseInboundSMS(nil, nil); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("ParseInboundSMS err = %v", err)
	}
}

func TestNoopProvider_Type(t *testing.T) {
	if got := (NoopProvider{}).Type(); got != ProviderType("noop") {
		t.Errorf("NoopProvider{}.Type() = %s, want noop", got)
	}
}
