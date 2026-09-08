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

// signalwireProvider implements Provider against SignalWire's Twilio
// compatibility REST API. SignalWire is the standard tenant configuration
// for this package: it wraps twilio-go the same way the Twilio adapter does,
// with the request host rewritten to the tenant's SignalWire space (see
// url_rewrite.go) and the webhook-signature header name swapped. Any
// compliance/consent gating a caller layers on top of outbound sends (10DLC
// registration checks, opt-out suppression, etc.) is application policy and
// deliberately does not live in this transport adapter.
type signalwireProvider struct {
	cfg   Config
	rest  *twilio.RestClient
	valid *twilioClient.RequestValidator
}

// NewSignalWireFactory returns a ProviderFactory that constructs a SignalWire
// adapter from a Config. Registered via Registry.Register(ProviderSignalWire,
// telephony.NewSignalWireFactory()).
func NewSignalWireFactory() ProviderFactory {
	return newSignalWireProvider
}

func newSignalWireProvider(c Config) (Provider, error) {
	if c.AccountSID == "" || c.AuthToken == "" || c.SpaceURL == "" {
		return nil, fmt.Errorf("%w: signalwire requires account_sid (project_id), auth_token, and space_url", ErrProviderNotConfigured)
	}

	inner := &twilioClient.Client{
		Credentials: twilioClient.NewCredentials(c.AccountSID, c.AuthToken),
	}
	inner.SetAccountSid(c.AccountSID)

	base := &swBaseClient{
		inner:     inner,
		spaceHost: stripScheme(c.SpaceURL),
	}

	rc := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username:   c.AccountSID,
		Password:   c.AuthToken,
		AccountSid: c.AccountSID,
		Client:     base,
	})

	v := twilioClient.NewRequestValidator(c.SignatureToken())
	return &signalwireProvider{cfg: c, rest: rc, valid: &v}, nil
}

// Type implements Provider.
func (p *signalwireProvider) Type() ProviderType { return ProviderSignalWire }

// SendSMS implements Provider.
func (p *signalwireProvider) SendSMS(_ context.Context, to, body, fromOverride string) (SMSResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return SMSResult{}, fmt.Errorf("%w: signalwire sending number required", ErrProviderNotConfigured)
	}
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(from)
	params.SetBody(body)

	resp, err := p.rest.Api.CreateMessage(params)
	if err != nil {
		return SMSResult{}, fmt.Errorf("signalwire send: %w", err)
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

// InitiateCall implements Provider.
func (p *signalwireProvider) InitiateCall(_ context.Context, to, fromOverride, statusCallbackURL string) (CallResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return CallResult{}, fmt.Errorf("%w: signalwire sending number required", ErrProviderNotConfigured)
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
		return CallResult{}, fmt.Errorf("signalwire call: %w", err)
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

// InitiateBridge implements Provider — the masked two-leg bridge call: rings
// toA first and, on answer, dials toB and connects the two legs. Both
// parties see the tenant's DID (fromOverride, defaulting to the tenant
// SendingNumber when "") as caller-ID, so neither sees the other's personal
// number.
func (p *signalwireProvider) InitiateBridge(_ context.Context, toA, toB, fromOverride, statusCallbackURL string) (CallResult, error) {
	from := fromOverride
	if from == "" {
		from = p.cfg.SendingNumber
	}
	if from == "" {
		return CallResult{}, fmt.Errorf("%w: signalwire sending number required", ErrProviderNotConfigured)
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
		return CallResult{}, fmt.Errorf("signalwire bridge: %w", err)
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

// VerifyWebhookSignature implements Provider.
//
// SignalWire signs with HMAC-SHA1 over the same (URL + sorted form fields)
// payload Twilio uses; the only differences are the header name
// (X-SignalWire-Signature, with X-Twilio-Signature accepted as a fallback
// for compatibility tooling) and the signing token (which may be the API
// token or a separately-rotated webhook signing token — Config.SignatureToken()
// selects between them). Both adapters delegate the actual HMAC comparison to
// twilio-go's RequestValidator; this package never reimplements it.
func (p *signalwireProvider) VerifyWebhookSignature(r *http.Request, body []byte) error {
	sig := r.Header.Get("X-SignalWire-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Twilio-Signature")
	}
	if sig == "" {
		return ErrInvalidSignature
	}
	if !p.valid.ValidateBody(reconstructURL(r), body, sig) {
		return ErrInvalidSignature
	}
	return nil
}

// ParseStatusWebhook implements Provider. Same form-field shape as Twilio.
func (p *signalwireProvider) ParseStatusWebhook(r *http.Request, _ []byte) (StatusEvent, error) {
	if err := r.ParseForm(); err != nil {
		return StatusEvent{}, fmt.Errorf("signalwire parse status form: %w", err)
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
	return StatusEvent{}, fmt.Errorf("signalwire status: missing MessageSid and CallSid")
}

// ParseInboundSMS implements Provider.
func (p *signalwireProvider) ParseInboundSMS(r *http.Request, body []byte) (InboundSMS, error) {
	if err := r.ParseForm(); err != nil {
		return InboundSMS{}, fmt.Errorf("signalwire parse inbound form: %w", err)
	}
	sid := r.PostFormValue("MessageSid")
	if sid == "" {
		return InboundSMS{}, fmt.Errorf("signalwire inbound: missing MessageSid")
	}
	return InboundSMS{
		MessageSID: sid,
		From:       r.PostFormValue("From"),
		To:         r.PostFormValue("To"),
		Body:       r.PostFormValue("Body"),
		Raw:        body,
	}, nil
}
