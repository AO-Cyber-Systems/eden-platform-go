package telephony

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	twilioClient "github.com/twilio/twilio-go/client"
)

// swBaseClient wraps a twilio-go client.Client and rewrites the
// api.twilio.com host on every outbound request to the configured
// SignalWire space host.
//
// twilio-go v1.30.4 hardcodes "https://api.twilio.com" inside each
// generated ApiService's baseURL field — there is no ClientParams.BaseURL in
// this version, so the only injection point is the BaseClient interface.
// This is exactly why the SignalWire adapter wraps twilio-go rather than
// forking its request-building logic: SignalWire's REST API is
// Twilio-compatible, and rewriting the host here is the one place the two
// backends actually differ in transport terms.
type swBaseClient struct {
	inner     *twilioClient.Client
	spaceHost string // e.g. "example.signalwire.com"
}

// AccountSid implements twilio-go BaseClient.
func (s *swBaseClient) AccountSid() string { return s.inner.AccountSid() }

// SetTimeout implements twilio-go BaseClient.
func (s *swBaseClient) SetTimeout(d time.Duration) { s.inner.SetTimeout(d) }

// SendRequest implements twilio-go BaseClient.
func (s *swBaseClient) SendRequest(method, raw string, data url.Values, headers map[string]any, body ...byte) (*http.Response, error) {
	return s.inner.SendRequest(method, rewriteHost(raw, s.spaceHost), data, headers, body...)
}

// SetOauth implements twilio-go BaseClient.
func (s *swBaseClient) SetOauth(o twilioClient.OAuth) { s.inner.SetOauth(o) }

// OAuth implements twilio-go BaseClient.
func (s *swBaseClient) OAuth() twilioClient.OAuth { return s.inner.OAuth() }

// rewriteHost returns rawURL with its host replaced by newHost, preserving
// path and query. Returns rawURL unchanged on parse error.
func rewriteHost(rawURL, newHost string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Host = newHost
	return u.String()
}

// stripScheme removes a leading https:// or http:// from a URL string.
// Used because the SignalWire base client wants a bare hostname.
func stripScheme(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.TrimSuffix(u, "/")
}
