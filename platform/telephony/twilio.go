package telephony

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	twilio "github.com/twilio/twilio-go"
	twilioClient "github.com/twilio/twilio-go/client"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// twilioProvider is the Twilio adapter binding tenant credentials to the
// twilio-go REST client and signature validator. It exists alongside
// signalwireProvider to prove the Provider seam is a real abstraction and
// not an assertion with a single implementation: adding it required no
// change to Provider, NoopProvider, or Registry.
type twilioProvider struct {
	cfg   Config
	rest  *twilio.RestClient
	valid *twilioClient.RequestValidator
}

// NewTwilioFactory returns a ProviderFactory that constructs a Twilio
// adapter from a Config. Registered via Registry.Register(ProviderTwilio,
// telephony.NewTwilioFactory()).
func NewTwilioFactory() ProviderFactory {
	return newTwilioProvider
}

func newTwilioProvider(c Config) (Provider, error) {
	if c.AccountSID == "" || c.AuthToken == "" {
		return nil, fmt.Errorf("%w: twilio requires account_sid and auth_token", ErrProviderNotConfigured)
	}
	rc := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: c.AccountSID,
		Password: c.AuthToken,
	})
	v := twilioClient.NewRequestValidator(c.SignatureToken())
	return &twilioProvider{cfg: c, rest: rc, valid: &v}, nil
}

// Type implements Provider.
func (p *twilioProvider) Type() ProviderType { return ProviderTwilio }

// SendSMS implements Provider.
func (p *twilioProvider) SendSMS(_ context.Context, to, body, fromOverride string) (SMSResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return SMSResult{}, fmt.Errorf("%w: twilio sending number required", ErrProviderNotConfigured)
	}
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(from)
	params.SetBody(body)

	resp, err := p.rest.Api.CreateMessage(params)
	if err != nil {
		return SMSResult{}, fmt.Errorf("twilio send: %w", err)
	}
	out := SMSResult{}
	if resp.Sid != nil {
		out.MessageSID = *resp.Sid
	}
	if resp.Status != nil {
		out.Status = mapStatus(*resp.Status)
	}
	return out, nil
}

// InitiateCall implements Provider. Inline TwiML routes the call — the same
// shape used for click-to-dial outbound calling.
func (p *twilioProvider) InitiateCall(_ context.Context, to, fromOverride, statusCallbackURL string) (CallResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return CallResult{}, fmt.Errorf("%w: twilio sending number required", ErrProviderNotConfigured)
	}
	twiml := fmt.Sprintf(`<Response><Dial callerId=%q><Number>%s</Number></Dial></Response>`, from, to)

	params := &twilioApi.CreateCallParams{}
	params.SetTo(to)
	params.SetFrom(from)
	params.SetTwiml(twiml)
	if statusCallbackURL != "" {
		params.SetStatusCallback(statusCallbackURL)
		params.SetStatusCallbackMethod("POST")
	}

	resp, err := p.rest.Api.CreateCall(params)
	if err != nil {
		return CallResult{}, fmt.Errorf("twilio call: %w", err)
	}
	out := CallResult{}
	if resp.Sid != nil {
		out.CallSID = *resp.Sid
	}
	if resp.Status != nil {
		out.Status = mapStatus(*resp.Status)
	}
	return out, nil
}

// InitiateBridge implements Provider — the masked two-leg bridge call:
// Twilio calls toA; on answer the inline TwiML dials toB and bridges the
// legs. Both parties see `from` (the tenant DID) as caller-ID, so neither
// sees the other's personal number.
func (p *twilioProvider) InitiateBridge(_ context.Context, toA, toB, fromOverride, statusCallbackURL string) (CallResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return CallResult{}, fmt.Errorf("%w: twilio sending number required", ErrProviderNotConfigured)
	}
	twiml := fmt.Sprintf(`<Response><Dial callerId=%q><Number>%s</Number></Dial></Response>`, from, toB)

	params := &twilioApi.CreateCallParams{}
	params.SetTo(toA)
	params.SetFrom(from)
	params.SetTwiml(twiml)
	if statusCallbackURL != "" {
		params.SetStatusCallback(statusCallbackURL)
		params.SetStatusCallbackMethod("POST")
	}

	resp, err := p.rest.Api.CreateCall(params)
	if err != nil {
		return CallResult{}, fmt.Errorf("twilio bridge: %w", err)
	}
	out := CallResult{}
	if resp.Sid != nil {
		out.CallSID = *resp.Sid
	}
	if resp.Status != nil {
		out.Status = mapStatus(*resp.Status)
	}
	return out, nil
}

// VerifyWebhookSignature implements Provider. Delegates the HMAC-SHA1
// comparison to twilio-go's RequestValidator — the same delegation
// signalwireProvider uses, over the same (URL + sorted form fields) payload.
func (p *twilioProvider) VerifyWebhookSignature(r *http.Request, body []byte) error {
	sig := r.Header.Get("X-Twilio-Signature")
	if sig == "" {
		return ErrInvalidSignature
	}
	if !p.valid.ValidateBody(reconstructURL(r), body, sig) {
		return ErrInvalidSignature
	}
	return nil
}

// ParseStatusWebhook implements Provider.
//
// Twilio status callbacks for SMS carry MessageSid/MessageStatus and for
// voice carry CallSid/CallStatus/CallDuration; presence of MessageSid is
// used to disambiguate.
func (p *twilioProvider) ParseStatusWebhook(r *http.Request, _ []byte) (StatusEvent, error) {
	if err := r.ParseForm(); err != nil {
		return StatusEvent{}, fmt.Errorf("twilio parse status form: %w", err)
	}
	if sid := r.PostFormValue("MessageSid"); sid != "" {
		return StatusEvent{
			Kind:      "sms",
			SID:       sid,
			Status:    mapStatus(r.PostFormValue("MessageStatus")),
			ErrorCode: r.PostFormValue("ErrorCode"),
			From:      r.PostFormValue("From"),
			To:        r.PostFormValue("To"),
		}, nil
	}
	if sid := r.PostFormValue("CallSid"); sid != "" {
		dur, _ := strconv.Atoi(r.PostFormValue("CallDuration"))
		return StatusEvent{
			Kind:      "call",
			SID:       sid,
			Status:    mapStatus(r.PostFormValue("CallStatus")),
			From:      r.PostFormValue("From"),
			To:        r.PostFormValue("To"),
			DurationS: dur,
		}, nil
	}
	return StatusEvent{}, fmt.Errorf("twilio status: missing MessageSid and CallSid")
}

// ParseInboundSMS implements Provider.
func (p *twilioProvider) ParseInboundSMS(r *http.Request, body []byte) (InboundSMS, error) {
	if err := r.ParseForm(); err != nil {
		return InboundSMS{}, fmt.Errorf("twilio parse inbound form: %w", err)
	}
	sid := r.PostFormValue("MessageSid")
	if sid == "" {
		return InboundSMS{}, fmt.Errorf("twilio inbound: missing MessageSid")
	}
	return InboundSMS{
		MessageSID: sid,
		From:       r.PostFormValue("From"),
		To:         r.PostFormValue("To"),
		Body:       r.PostFormValue("Body"),
		Raw:        body,
	}, nil
}
