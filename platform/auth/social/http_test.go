package social

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
)

// Test list case 5: on success the callback handler writes a 302 whose Location
// contains both access_token= and refresh_token=.
func TestHandleCallbackHTTP_Success_RedirectsWithTokens(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	// Inject a canned HandleCallback so the handler test never touches the
	// network / real OIDC discovery.
	svc.callback = func(_ context.Context, code, state string) (*auth.AuthResponse, string, error) {
		return &auth.AuthResponse{
			AccessToken:  "ACCESS123",
			RefreshToken: "REFRESH456",
			User:         auth.User{ID: uuid.New()},
		}, "com.justindonnaruma.app://auth/social/callback", nil
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
	if !strings.Contains(loc, "access_token=ACCESS123") {
		t.Errorf("Location %q missing access_token", loc)
	}
	if !strings.Contains(loc, "refresh_token=REFRESH456") {
		t.Errorf("Location %q missing refresh_token", loc)
	}
	if !strings.HasPrefix(loc, "com.justindonnaruma.app://auth/social/callback") {
		t.Errorf("Location %q does not start with the app redirect URI", loc)
	}
}

// On a callback error WITH a known redirect URI, the handler redirects to
// redirectURI?error=... rather than leaking a 500 to the user.
func TestHandleCallbackHTTP_Error_RedirectsWithErrorParam(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state string) (*auth.AuthResponse, string, error) {
		return nil, "com.justindonnaruma.app://auth/social/callback", errTest
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
	if !strings.HasPrefix(loc, "com.justindonnaruma.app://auth/social/callback") {
		t.Errorf("Location %q does not start with the app redirect URI", loc)
	}
	if !strings.Contains(loc, "error=") {
		t.Errorf("Location %q missing error param", loc)
	}
	if strings.Contains(loc, "access_token=") {
		t.Errorf("SECURITY: error redirect must not contain tokens: %q", loc)
	}
}

// On a callback error WITHOUT a known redirect URI, the handler returns an HTTP
// error (no open redirect to an unknown target).
func TestHandleCallbackHTTP_Error_NoRedirectURI_HTTPError(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state string) (*auth.AuthResponse, string, error) {
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

// POST is registered too (Apple posts the user/name field as a form). The
// handler must accept it and still redirect with tokens.
func TestHandleCallbackHTTP_POST_Accepted(t *testing.T) {
	svc, _, _ := newOIDCTestService(t)

	svc.callback = func(_ context.Context, code, state string) (*auth.AuthResponse, string, error) {
		return &auth.AuthResponse{AccessToken: "A", RefreshToken: "R", User: auth.User{ID: uuid.New()}},
			"com.justindonnaruma.app://auth/social/callback", nil
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
	if !strings.Contains(rec.Header().Get("Location"), "access_token=A") {
		t.Errorf("POST Location missing tokens: %q", rec.Header().Get("Location"))
	}
}

var errTest = &testError{"boom"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }
