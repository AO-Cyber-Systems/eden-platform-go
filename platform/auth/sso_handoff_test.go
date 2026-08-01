package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The SSO OIDC callback used to build its redirect target with
//
//	fmt.Sprintf("%s%saccess_token=%s&refresh_token=%s", ...)
//
// which put a ~9.6KB ML-DSA token pair into a URL. It now emits
// ?code=<handoff>&state=<state> and the pair is obtainable only by POSTing that
// code to /auth/oidc/exchange. (AOID obj-50 SDK-08 / decision D6.)

const ssoTestRedirectURI = "https://app.example.test/auth/complete"

func newSSOHandoffTestService(t *testing.T, redirectURI string) (*SSOService, *http.ServeMux) {
	t.Helper()
	svc := NewSSOService(nil, newTestJWTManager(t), "https://example.test")
	svc.callback = func(context.Context, string, string) (*AuthResponse, string, error) {
		return &AuthResponse{AccessToken: "ACCESS123", RefreshToken: "REFRESH456"}, redirectURI, nil
	}
	mux := http.NewServeMux()
	svc.RegisterHTTPHandlers(mux)
	return svc, mux
}

func ssoCallback(t *testing.T, mux *http.ServeMux) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Test-list item 14: the SSO OIDC callback Location contains no tokens and does
// contain code=. This is the inversion of the old sso.go redirect tail.
func TestSSOCallbackRedirectsWithCodeNotTokens(t *testing.T) {
	_, mux := newSSOHandoffTestService(t, ssoTestRedirectURI)

	rec := ssoCallback(t, mux)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body=%q)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")

	if strings.Contains(loc, "access_token=") || strings.Contains(loc, "refresh_token=") {
		t.Fatalf("SECURITY REGRESSION (AOID obj-50 SDK-08 / D6): the SSO OIDC callback "+
			"redirect must carry an authorization CODE, never tokens. Tokens in a URL leak "+
			"to browser history, Referer headers and proxy logs. Location=%q", loc)
	}
	if strings.Contains(loc, "ACCESS123") || strings.Contains(loc, "REFRESH456") {
		t.Fatalf("SECURITY REGRESSION (AOID obj-50 SDK-08 / D6): a token VALUE appears in the "+
			"SSO redirect target under some other parameter name: %q", loc)
	}
	if !strings.Contains(loc, "code=") {
		t.Errorf("Location %q is missing the handoff code", loc)
	}
	if !strings.Contains(loc, "state=xyz") {
		t.Errorf("Location %q is missing the echoed state", loc)
	}
}

// Test-list item 15 (CONTROL): the redirectURI == "json" sentinel still returns
// the token pair in the JSON body. That branch was always fine — tokens in a
// BODY is the shape the exchange endpoint now mimics — and must not have been
// caught up in the rewrite.
func TestSSOCallbackJSONBranchStillReturnsTokens(t *testing.T) {
	for _, redirectURI := range []string{"json", ""} {
		t.Run("redirectURI="+redirectURI, func(t *testing.T) {
			_, mux := newSSOHandoffTestService(t, redirectURI)

			rec := ssoCallback(t, mux)
			if rec.Code == http.StatusFound {
				t.Fatalf("the %q branch must NOT redirect, got Location=%q",
					redirectURI, rec.Header().Get("Location"))
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var out map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if out["access_token"] != "ACCESS123" || out["refresh_token"] != "REFRESH456" {
				t.Errorf("json branch body = %q, want the token pair", rec.Body.String())
			}
		})
	}
}

// The gotcha from the TRD: a consumer may pass a FRAGMENT-shaped redirect_uri
// (the "#/auth/complete" trick sso.go documents, which existed to keep a ~9.6KB
// token pair out of the request line). The ?code= tail must still compose into
// something a hash-routed SPA can parse — i.e. the query lands INSIDE the
// fragment, after the route, not before the '#'.
func TestSSOCallbackFragmentRedirectComposes(t *testing.T) {
	const fragmentURI = "https://app.example.test/#/auth/complete"
	_, mux := newSSOHandoffTestService(t, fragmentURI)

	rec := ssoCallback(t, mux)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")

	if !strings.HasPrefix(loc, fragmentURI+"?") {
		t.Fatalf("fragment redirect did not compose: %q (want %q + \"?code=...\")", loc, fragmentURI)
	}
	hash := strings.Index(loc, "#")
	query := strings.Index(loc, "?")
	if hash < 0 || query < hash {
		t.Fatalf("the query must land INSIDE the fragment for a hash-routed SPA: %q", loc)
	}
	if strings.Contains(loc, "access_token=") || strings.Contains(loc, "refresh_token=") {
		t.Fatalf("SECURITY REGRESSION (AOID obj-50 SDK-08 / D6): tokens in the fragment "+
			"redirect: %q", loc)
	}
}

// The SSO handoff code is spendable at the SSO exchange endpoint, and only over
// POST. Without this the SSO callback would hand out a code nobody could trade.
func TestSSOExchange(t *testing.T) {
	_, mux := newSSOHandoffTestService(t, ssoTestRedirectURI)

	loc := ssoCallback(t, mux).Header().Get("Location")
	values, err := url.ParseQuery(loc[strings.Index(loc, "?")+1:])
	if err != nil {
		t.Fatalf("parse redirect query: %v", err)
	}
	code := values.Get("code")
	if code == "" {
		t.Fatalf("no handoff code in %q", loc)
	}

	post := func(code, redirectURI string) *httptest.ResponseRecorder {
		form := url.Values{"code": {code}, "redirect_uri": {redirectURI}}
		req := httptest.NewRequest(http.MethodPost, "/auth/oidc/exchange",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("GET is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/auth/oidc/exchange?code="+url.QueryEscape(code), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET status = %d, want 405", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "ACCESS123") {
			t.Fatalf("SECURITY: a GET exchange returned tokens: %q", rec.Body.String())
		}
	})

	t.Run("wrong audience is refused", func(t *testing.T) {
		rec := post(code, "https://evil.example/steal")
		if rec.Code == http.StatusOK {
			t.Fatal("SECURITY: an SSO handoff code was spent for the wrong redirect target")
		}
	})

	t.Run("POST returns the pair in a JSON body", func(t *testing.T) {
		rec := post(code, ssoTestRedirectURI)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var out map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		if out["access_token"] != "ACCESS123" || out["refresh_token"] != "REFRESH456" {
			t.Errorf("exchange body = %q, want the token pair", rec.Body.String())
		}
	})

	t.Run("replay is refused", func(t *testing.T) {
		rec := post(code, ssoTestRedirectURI)
		if rec.Code == http.StatusOK {
			t.Fatal("SECURITY: an SSO handoff code was spendable twice")
		}
		if strings.Contains(rec.Body.String(), "ACCESS123") {
			t.Fatalf("SECURITY: the replayed SSO exchange returned tokens: %q", rec.Body.String())
		}
	})
}
