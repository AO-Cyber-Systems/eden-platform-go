package oauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withEndpoints temporarily replaces every endpoint with the test server URL
// and returns a cleanup function.
func withEndpoints(t *testing.T, ts *httptest.Server) func() {
	t.Helper()
	saved := Endpoints
	Endpoints.Google = ts.URL + "/google"
	Endpoints.GitHubUser = ts.URL + "/github/user"
	Endpoints.GitHubEmails = ts.URL + "/github/emails"
	Endpoints.Facebook = ts.URL + "/facebook/me"
	Endpoints.Twitter = ts.URL + "/twitter/me"
	Endpoints.MicrosoftGraph = ts.URL + "/ms/me"
	return func() { Endpoints = saved }
}

func TestFetchUserInfo_Google(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/google" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sub":     "g-1",
			"email":   "u@gmail.com",
			"name":    "Google User",
			"picture": "https://photo",
		})
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("google", "good")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "g-1" || info.Email != "u@gmail.com" || info.Name != "Google User" || info.Image != "https://photo" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchUserInfo_GitHub(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         42,
				"login":      "ghuser",
				"name":       "GH User",
				"email":      "gh@example.com",
				"avatar_url": "https://avatars/42",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("github", "good")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "42" || info.Email != "gh@example.com" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchUserInfo_GitHub_PrivateEmailFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    7,
				"login": "private",
				// no email
			})
		case "/github/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "noisy@example.com", "primary": false, "verified": true},
				{"email": "primary@example.com", "primary": true, "verified": true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("github", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Email != "primary@example.com" {
		t.Errorf("expected fallback email primary@example.com, got %q", info.Email)
	}
	if info.Name != "private" { // login fallback when name empty
		t.Errorf("expected name to fall back to login, got %q", info.Name)
	}
}

func TestFetchUserInfo_Facebook(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/facebook/me" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("access_token") != "fbtok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":    "fb-1",
			"name":  "FB User",
			"email": "fb@example.com",
		})
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("facebook", "fbtok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "fb-1" || info.Email != "fb@example.com" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchUserInfo_Twitter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/twitter/me") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{
				"id":       "tw-1",
				"name":     "TW User",
				"username": "twuser",
			},
		})
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("twitter", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "tw-1" || info.Name != "TW User" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchUserInfo_Apple(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"apple-1","email":"a@example.com"}`))
	jwt := header + "." + payload + ".sig"

	info, err := FetchUserInfo("apple", jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "apple-1" || info.Email != "a@example.com" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchUserInfo_Apple_RejectsBadJWT(t *testing.T) {
	if _, err := FetchUserInfo("apple", "not-a-jwt"); err == nil {
		t.Error("expected error for malformed apple id_token")
	}
}

func TestFetchUserInfo_Microsoft_MailFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ms/me" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"ms-1","displayName":"MS User","mail":"","userPrincipalName":"upn@tenant.com"}`))
	}))
	defer ts.Close()
	defer withEndpoints(t, ts)()

	info, err := FetchUserInfo("microsoft", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Email != "upn@tenant.com" {
		t.Errorf("expected fallback email upn@tenant.com, got %q", info.Email)
	}
}

func TestFetchUserInfo_UnsupportedProvider(t *testing.T) {
	if _, err := FetchUserInfo("yahoo", "tok"); err == nil {
		t.Error("expected error for unsupported provider")
	}
}
