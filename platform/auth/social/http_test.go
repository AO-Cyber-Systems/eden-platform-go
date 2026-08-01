package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
)

// These tests are the regression gate for AOID obj-50 SDK-08 / D6.
//
// They used to assert the OPPOSITE: that the success redirect's Location
// contains access_token= and refresh_token=. That was the leak, written down as
// the contract. The callback now delivers a short-lived, single-use handoff CODE
// (?code=&state=), and the token pair is obtainable only by POSTing that code to
// /auth/social/exchange, which returns it in a JSON response BODY.
//
// The inverted assertions were kept rather than deleted: a deleted test is
// indistinguishable from a test that never existed, whereas an inverted one
// fails loudly and names the class if anyone reintroduces the leak.

const testRedirectURI = "com.justindonnaruma.app://auth/social/callback"

// assertNoTokensInLocation is the regression gate itself. Both substrings, not
// one — the original tests checked them on separate lines and a partial
// inversion would leave half the leak asserted.
func assertNoTokensInLocation(t *testing.T, loc string) {
	t.Helper()
	if strings.Contains(loc, "access_token=") || strings.Contains(loc, "refresh_token=") {
		t.Fatalf("SECURITY REGRESSION (AOID obj-50 SDK-08 / D6): the callback redirect "+
			"must carry an authorization CODE, never tokens. Tokens in a URL leak to "+
			"browser history, Referer headers and proxy logs. Location=%q", loc)
	}
}

// Test-list items 7 + 8: the success redirect carries a code and a state and NO
// tokens.
func TestHandleCallbackHTTP_Success_RedirectsWithCodeNotTokens(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	// Inject a canned HandleCallback so the handler test never touches the
	// network / real OIDC discovery.
	svc.callback = func(_ context.Context, code, state, _ string) (*auth.AuthResponse, string, error) {
		return &auth.AuthResponse{
			AccessToken:  "ACCESS123",
			RefreshToken: "REFRESH456",
			User:         auth.User{ID: uuid.New()},
		}, testRedirectURI, nil
	}

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/auth/social/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")

	// Item 7 — the inversion of the old lines 40/43.
	assertNoTokensInLocation(t, loc)
	// Belt and braces: the token VALUES must not appear under any parameter name.
	if strings.Contains(loc, "ACCESS123") || strings.Contains(loc, "REFRESH456") {
		t.Fatalf("SECURITY REGRESSION (AOID obj-50 SDK-08 / D6): a token value appears in "+
			"the redirect target under some other parameter name: %q", loc)
	}

	// Item 8.
	if !strings.Contains(loc, "code=") {
		t.Errorf("Location %q is missing the handoff code", loc)
	}
	if !strings.Contains(loc, "state=xyz") {
		t.Errorf("Location %q is missing the echoed state", loc)
	}
	if !strings.HasPrefix(loc, testRedirectURI) {
		t.Errorf("Location %q does not start with the app redirect URI", loc)
	}
}

// Test-list item 9 (CONTROL, unchanged behaviour): on a callback error WITH a
// known redirect URI, the handler redirects to redirectURI?error=... rather than
// leaking a 500. This assertion was already negative before the change and must
// keep passing untouched.
func TestHandleCallbackHTTP_Error_RedirectsWithErrorParam(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state, _ string) (*auth.AuthResponse, string, error) {
		return nil, testRedirectURI, errTest
	}

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/auth/social/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (redirect with error param)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testRedirectURI) {
		t.Errorf("Location %q does not start with the app redirect URI", loc)
	}
	if !strings.Contains(loc, "error=") {
		t.Errorf("Location %q missing error param", loc)
	}
	assertNoTokensInLocation(t, loc)
	if strings.Contains(loc, "code=") {
		t.Errorf("the error redirect must not carry a handoff code: %q", loc)
	}
}

// CONTROL, unchanged: on a callback error WITHOUT a known redirect URI, the
// handler returns an HTTP error (no open redirect to an unknown target).
func TestHandleCallbackHTTP_Error_NoRedirectURI_HTTPError(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state, _ string) (*auth.AuthResponse, string, error) {
		return nil, "", errTest
	}

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/auth/social/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusFound {
		t.Fatalf("expected an HTTP error, got a 302 redirect (open-redirect risk)")
	}
	if rec.Code < 400 {
		t.Errorf("status = %d, want a 4xx/5xx error", rec.Code)
	}
}

// Test-list item 10 (CONTROL): the SUCCESS path is still open-redirect guarded.
// An un-allowlisted redirect target produces an HTTP error, not a 302 — and
// certainly not a handoff code delivered to an attacker's URL.
func TestHandleCallbackHTTP_Success_UnallowlistedRedirect_HTTPError(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state, _ string) (*auth.AuthResponse, string, error) {
		return &auth.AuthResponse{AccessToken: "A", RefreshToken: "R", User: auth.User{ID: uuid.New()}},
			"https://evil.example/steal", nil
	}

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/auth/social/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusFound {
		t.Fatalf("SECURITY: redirected to a non-allowlisted target %q (open redirect)",
			rec.Header().Get("Location"))
	}
	if rec.Code < 400 {
		t.Errorf("status = %d, want a 4xx error", rec.Code)
	}
}

// Test-list item 13 (CONTROL): Apple POSTs the callback as a form and carries a
// one-time `user` (name) field on the FIRST authorization only. The handler must
// still read code/state from the form body and the `user` field must still reach
// the provisioning path.
func TestHandleCallbackHTTP_POST_AppleUserFieldStillReachesCallback(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	var gotCode, gotState, gotUser string
	svc.callback = func(_ context.Context, code, state, formUserField string) (*auth.AuthResponse, string, error) {
		gotCode, gotState, gotUser = code, state, formUserField
		return &auth.AuthResponse{AccessToken: "A", RefreshToken: "R", User: auth.User{ID: uuid.New()}},
			testRedirectURI, nil
	}

	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)

	body := strings.NewReader("code=abc&state=xyz&user=%7B%22name%22%3A%7B%7D%7D")
	req := httptest.NewRequest(http.MethodPost, "/auth/social/callback", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("POST status = %d, want 302", rec.Code)
	}
	if gotCode != "abc" || gotState != "xyz" {
		t.Errorf("form code/state not read from the body: code=%q state=%q", gotCode, gotState)
	}
	if gotUser != `{"name":{}}` {
		t.Errorf("Apple's one-time user field did not reach the callback: %q", gotUser)
	}

	loc := rec.Header().Get("Location")
	// The inversion of the old POST assertion, which REQUIRED the access token
	// to be present in the Location header.
	assertNoTokensInLocation(t, loc)
	if !strings.Contains(loc, "code=") {
		t.Errorf("POST Location missing the handoff code: %q", loc)
	}
}

// Test-list item 11: POST to the exchange endpoint with a valid code returns the
// token pair as JSON, in the BODY, with Content-Type: application/json. This is
// also the end-to-end proof that the code the callback emits is spendable.
func TestExchange_POST_ReturnsTokenPairInJSONBody(t *testing.T) {
	svc, mux := newExchangeTestService(t)

	code := callbackHandoffCode(t, svc, mux)

	rec := postExchange(t, mux, code, testRedirectURI)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode exchange body %q: %v", rec.Body.String(), err)
	}
	if out["access_token"] != "ACCESS123" {
		t.Errorf("access_token = %q, want ACCESS123", out["access_token"])
	}
	if out["refresh_token"] != "REFRESH456" {
		t.Errorf("refresh_token = %q, want REFRESH456", out["refresh_token"])
	}
}

// Test-list item 12: GET on the exchange endpoint is rejected, so the code can
// never be spent from a URL (which would put the tokens in a cacheable response
// to a request line containing the code).
func TestExchange_GET_Rejected(t *testing.T) {
	svc, mux := newExchangeTestService(t)
	code := callbackHandoffCode(t, svc, mux)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/social/exchange?code="+url.QueryEscape(code)+
			"&redirect_uri="+url.QueryEscape(testRedirectURI), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /auth/social/exchange status = %d, want 405", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "ACCESS123") ||
		strings.Contains(rec.Body.String(), "REFRESH456") {
		t.Fatalf("SECURITY: a GET exchange returned tokens: %q", rec.Body.String())
	}
}

// The exchange is single-use: spending the same code twice fails and yields no
// tokens the second time.
func TestExchange_ReplayIsRefused(t *testing.T) {
	svc, mux := newExchangeTestService(t)
	code := callbackHandoffCode(t, svc, mux)

	if rec := postExchange(t, mux, code, testRedirectURI); rec.Code != http.StatusOK {
		t.Fatalf("first exchange status = %d, want 200", rec.Code)
	}

	rec := postExchange(t, mux, code, testRedirectURI)
	if rec.Code == http.StatusOK {
		t.Fatal("SECURITY: a handoff code was spendable twice at the exchange endpoint")
	}
	if strings.Contains(rec.Body.String(), "ACCESS123") ||
		strings.Contains(rec.Body.String(), "REFRESH456") {
		t.Fatalf("SECURITY: the replayed exchange returned tokens: %q", rec.Body.String())
	}
}

// Audience binding at the HTTP layer: a code minted for the app's redirect URI
// cannot be spent by claiming a different one.
func TestExchange_WrongRedirectURI_Refused(t *testing.T) {
	svc, mux := newExchangeTestService(t)
	code := callbackHandoffCode(t, svc, mux)

	rec := postExchange(t, mux, code, "http://localhost/other")
	if rec.Code == http.StatusOK {
		t.Fatal("SECURITY: a handoff code was spent for a redirect URI it was not minted for")
	}
	if strings.Contains(rec.Body.String(), "ACCESS123") {
		t.Fatalf("SECURITY: cross-audience exchange returned tokens: %q", rec.Body.String())
	}

	// Control: still spendable for its own redirect URI, so the rejection above
	// was audience binding and not the code being burned or malformed.
	if ok := postExchange(t, mux, code, testRedirectURI); ok.Code != http.StatusOK {
		t.Fatalf("control: the code must still be spendable for its own audience, got %d (%q)",
			ok.Code, ok.Body.String())
	}
}

// A garbage code yields a generic failure and no tokens.
func TestExchange_UnknownCode_Refused(t *testing.T) {
	_, mux := newExchangeTestService(t)

	rec := postExchange(t, mux, "not-a-real-code", testRedirectURI)
	if rec.Code == http.StatusOK {
		t.Fatal("SECURITY: a bogus handoff code was accepted")
	}
	if strings.Contains(rec.Body.String(), "ACCESS") || strings.Contains(rec.Body.String(), "REFRESH") {
		t.Fatalf("SECURITY: bogus exchange returned tokens: %q", rec.Body.String())
	}
}

// --- helpers -------------------------------------------------------------

// newExchangeTestService builds a service whose canned callback issues a known
// pair, plus a mux with both the callback and the exchange registered.
func newExchangeTestService(t *testing.T) (*SocialAuthService, *http.ServeMux) {
	t.Helper()
	svc, _, _ := newOIDCTestService(t)
	svc.callback = func(_ context.Context, code, state, _ string) (*auth.AuthResponse, string, error) {
		return &auth.AuthResponse{
			AccessToken:  "ACCESS123",
			RefreshToken: "REFRESH456",
			User:         auth.User{ID: uuid.New()},
		}, testRedirectURI, nil
	}
	mux := http.NewServeMux()
	svc.RegisterSocialHTTPHandlers(mux)
	return svc, mux
}

// callbackHandoffCode drives a real callback and extracts the handoff code from
// the redirect target, so the exchange tests spend a code the handler actually
// minted rather than one fabricated by the test.
func callbackHandoffCode(t *testing.T, _ *SocialAuthService, mux *http.ServeMux) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/social/callback?code=abc&state=xyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")

	q := loc[strings.Index(loc, "?")+1:]
	values, err := url.ParseQuery(q)
	if err != nil {
		t.Fatalf("parse redirect query %q: %v", q, err)
	}
	code := values.Get("code")
	if code == "" {
		t.Fatalf("no handoff code in redirect target %q", loc)
	}
	return code
}

func postExchange(t *testing.T, mux *http.ServeMux, code, redirectURI string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"code": {code}, "redirect_uri": {redirectURI}}
	req := httptest.NewRequest(http.MethodPost, "/auth/social/exchange",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

var errTest = &testError{"boom"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }
