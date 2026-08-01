package social

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/google/uuid"
)

// RegisterSocialHTTPHandlers registers the consumer social-login callback on the
// given mux. Both GET and POST are registered: most providers return code+state
// on the query string (GET), but Apple posts the one-time `user` (name) field as
// a form POST. 09-03 reads that field from the parsed form; this handler parses
// it but otherwise ignores it.
//
// The exchange endpoint is registered for POST ONLY. That is deliberate: a GET
// would put the handoff code in the request line and return the token pair in a
// cacheable response. ServeMux answers a GET on this path with 405.
func (s *SocialAuthService) RegisterSocialHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/social/callback", s.handleCallbackHTTP)
	mux.HandleFunc("POST /auth/social/callback", s.handleCallbackHTTP)
	mux.HandleFunc("POST /auth/social/exchange", s.handleExchangeHTTP)
	// Meta requires a data-deletion callback to publish a Facebook app. This is a
	// stub: it acknowledges the request (the {url, confirmation_code} shape Meta
	// validates) and audits it, but performs NO real deletion here.
	mux.HandleFunc("POST /auth/social/facebook/deletion", s.handleFacebookDeletion)

	slog.Info("registered social-login HTTP endpoints",
		"social_callback", "/auth/social/callback (GET+POST)",
		"social_exchange", "/auth/social/exchange (POST)",
		"facebook_deletion", "/auth/social/facebook/deletion (POST)")
}

// handleFacebookDeletion implements Meta's required data-deletion callback. Meta
// rejects a bare 200 — it expects a JSON body of {url, confirmation_code} where
// `url` is a human-readable status page and `confirmation_code` is a tracking
// token. We acknowledge the request, record a best-effort audit entry, and do
// NOT delete any data here (a real deletion job is out of scope for this stub).
func (s *SocialAuthService) handleFacebookDeletion(w http.ResponseWriter, r *http.Request) {
	confirmationCode, err := randomConfirmationCode()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Audit the request — best-effort, never block the 200. B2C has no company
	// scope, so companyID/actorID are uuid.Nil.
	if s.users != nil {
		details := fmt.Sprintf(`{"confirmation_code":%q}`, confirmationCode)
		_ = s.users.CreateAuditLog(r.Context(), uuid.Nil, uuid.Nil,
			"social.facebook.deletion_request", "user_identity", "", "", []byte(details))
	}

	statusURL := s.baseURL + "/auth/social/facebook/deletion/status?code=" + url.QueryEscape(confirmationCode)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url":               statusURL,
		"confirmation_code": confirmationCode,
	})
}

// randomConfirmationCode returns a 32-hex-char tracking code for a data-deletion
// request.
func randomConfirmationCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleCallbackHTTP completes a social-login flow and hands the app a
// short-lived AUTHORIZATION CODE for the issued token pair. code+state are read
// from the query string and, for form POSTs (Apple), the form body.
//
// On success it 302-redirects to redirectURI?code=<handoff>&state=<state>. The
// app then POSTs that code to /auth/social/exchange and receives the token pair
// in a JSON response BODY.
//
// This used to redirect to redirectURI?access_token=A&refresh_token=B. That put
// both tokens in browser history, in every Referer header the app subsequently
// sent, in every proxy access log along the path, and — for the desktop loopback
// flow — in shell history via argv. The handoff code is single-use,
// audience-bound and dead after 60 seconds, so the same URL exposure yields
// nothing. (AOID obj-50 SDK-08 / decision D6.)
//
// On error it redirects to redirectURI?error=... when the redirect is known,
// else returns an HTTP error (no open redirect to an unknown target).
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

	handoffCode, err := auth.MintHandoff(r.Context(), s.jwt, s.HandoffStore(), redirectURI, resp)
	if err != nil {
		slog.Error("mint social handoff failed", "error", err)
		http.Redirect(w, r, appendQuery(redirectURI, "error", "authentication_failed"), http.StatusFound)
		return
	}

	target := appendQuery(redirectURI, "code", handoffCode)
	target = appendQuery(target, "state", state)
	http.Redirect(w, r, target, http.StatusFound)
}

// handleExchangeHTTP trades a handoff code for the token pair.
//
//	POST /auth/social/exchange
//	Content-Type: application/x-www-form-urlencoded
//	code=<handoff>&redirect_uri=<the exact target the code was delivered to>
//
//	200 OK
//	Content-Type: application/json
//	{"access_token":"...","refresh_token":"..."}
//
// POST only, and the parameters are read from the POST BODY specifically
// (PostFormValue, not FormValue) so the code cannot be smuggled back into the
// request line as a query parameter. The response carries Cache-Control:
// no-store because it contains bearer credentials.
//
// Every failure — expired, replayed, wrong audience, unknown, malformed — is
// reported identically. Distinguishing them would turn this endpoint into an
// oracle telling an attacker which half of a guess was right.
func (s *SocialAuthService) handleExchangeHTTP(w http.ResponseWriter, r *http.Request) {
	// The mux registers POST only, so a GET is answered with 405 before it gets
	// here. This guard makes the contract hold for consumers that mount the
	// handler on a router without method matching.
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	handoffCode := r.PostFormValue("code")
	redirectURI := r.PostFormValue("redirect_uri")
	if handoffCode == "" || redirectURI == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Defence in depth. The code is already bound to the exact target it was
	// minted for, and minting only happens for an allowlisted target — but a
	// caller naming a target this service would never redirect to is not a
	// caller worth answering.
	if !s.isAllowedRedirectURI(redirectURI) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	resp, err := auth.RedeemHandoff(r.Context(), s.jwt, s.HandoffStore(), redirectURI, handoffCode)
	if err != nil {
		slog.Warn("social handoff exchange refused", "error", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token":  resp.AccessToken,
		"refresh_token": resp.RefreshToken,
	})
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
