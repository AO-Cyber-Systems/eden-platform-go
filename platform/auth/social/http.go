package social

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// RegisterSocialHTTPHandlers registers the consumer social-login callback on the
// given mux. Both GET and POST are registered: most providers return code+state
// on the query string (GET), but Apple posts the one-time `user` (name) field as
// a form POST. 09-03 reads that field from the parsed form; this handler parses
// it but otherwise ignores it.
func (s *SocialAuthService) RegisterSocialHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/social/callback", s.handleCallbackHTTP)
	mux.HandleFunc("POST /auth/social/callback", s.handleCallbackHTTP)

	slog.Info("registered social-login HTTP endpoint",
		"social_callback", "/auth/social/callback (GET+POST)")
}

// handleCallbackHTTP completes a social-login flow and delivers the issued token
// pair to the app via redirect. code+state are read from the query string and,
// for form POSTs (Apple), the form body. On success it 302-redirects to
// redirectURI?access_token=A&refresh_token=B (same tail as the SSO OIDC
// callback). On error it redirects to redirectURI?error=... when the redirect is
// known, else returns an HTTP error (no open redirect to an unknown target).
func (s *SocialAuthService) handleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse the form so Apple's one-time `user` field is available to 09-03 and
	// so POST callbacks expose code/state via r.FormValue. Tolerate a parse
	// failure — query params still work for the common GET case.
	_ = r.ParseForm()

	code := r.FormValue("code")
	if code == "" {
		code = r.URL.Query().Get("code")
	}
	state := r.FormValue("state")
	if state == "" {
		state = r.URL.Query().Get("state")
	}

	if code == "" || state == "" {
		http.Error(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}

	// Apple POSTs a one-time `user` field (name JSON) on the FIRST authorization
	// only; absent for every other provider and on repeat Apple sign-ins.
	formUserField := r.FormValue("user")

	resp, redirectURI, err := s.callback(r.Context(), code, state, formUserField)
	if err != nil {
		slog.Error("social callback failed", "error", err)
		// If we know where to send the user, surface the error there instead of
		// leaking a 500. Only redirect to allowlisted targets.
		if redirectURI != "" && s.isAllowedRedirectURI(redirectURI) {
			target := appendQuery(redirectURI, "error", "authentication_failed")
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Defense-in-depth: never redirect tokens to a non-allowlisted target even
	// if HandleCallback returned one.
	if redirectURI == "" || !s.isAllowedRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect target", http.StatusBadRequest)
		return
	}

	target := appendQuery(redirectURI, "access_token", resp.AccessToken)
	target = appendQuery(target, "refresh_token", resp.RefreshToken)
	http.Redirect(w, r, target, http.StatusFound)
}

// appendQuery appends key=value to a URL, choosing ? or & based on whether the
// URL already carries a query string. Custom-scheme deep-links
// (com.justindonnaruma.app://...) are handled the same as http(s) URLs.
func appendQuery(rawURL, key, value string) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s%s=%s", rawURL, sep, key, url.QueryEscape(value))
}
