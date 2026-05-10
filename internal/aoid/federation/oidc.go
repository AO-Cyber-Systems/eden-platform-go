package federation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// StubOIDCExchanger is a minimal OIDCExchanger that emits deterministic
// authorization URLs and never performs real network IO. Used:
//   - As the default exchanger when no real OIDC client is wired (Phase
//     A composition).
//   - In tests as the well-behaved exchanger.
//
// The auth URL takes the form:
//
//	<issuer>/authorize?client_id=<id>&redirect_uri=<uri>&state=<s>&response_type=code&scope=openid+profile+email
//
// ExchangeCode returns an Assertion with synthetic Email/Subject; this
// is *only* useful for tests. Real OIDC integrations should plumb a
// proper exchanger via the coreos go-oidc package.
type StubOIDCExchanger struct {
	// FixedAssertion, when non-nil, is returned verbatim from
	// ExchangeCode. When nil, ExchangeCode derives a deterministic
	// Assertion from the code (Email = "user-<code>@stub.local").
	FixedAssertion *Assertion
}

// BuildAuthURL returns the synthetic authorization URL.
func (s *StubOIDCExchanger) BuildAuthURL(_ context.Context, cfg TenantExternalIdP, redirectURI, state string) (string, error) {
	base := strings.TrimRight(cfg.EntityID, "/") + "/authorize"
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("federation/stub: parse issuer: %w", err)
	}
	q := u.Query()
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeCode returns the FixedAssertion or a synthetic one.
func (s *StubOIDCExchanger) ExchangeCode(_ context.Context, cfg TenantExternalIdP, code, _ string) (*Assertion, error) {
	if s.FixedAssertion != nil {
		out := *s.FixedAssertion
		return &out, nil
	}
	if code == "" {
		return nil, fmt.Errorf("federation/stub: empty code")
	}
	email := fmt.Sprintf("user-%s@stub.local", sanitize(code))
	return &Assertion{
		Subject:      hex.EncodeToString(mustRandom(8)),
		Email:        email,
		DisplayName:  "Stub User",
		Attributes:   map[string][]string{"email": {email}},
		AuthnContext: "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport",
	}, nil
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "anon"
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return string(out)
}

func mustRandom(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Deterministic fallback so tests don't blow up if rand misbehaves.
		for i := range b {
			b[i] = byte(i)
		}
	}
	return b
}
