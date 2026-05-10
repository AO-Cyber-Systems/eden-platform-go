package issuer

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// cryptoRandRead is the seam for randRead in token.go.
func cryptoRandRead(p []byte) (int, error) {
	return rand.Read(p)
}

// UserinfoHandler serves GET/POST /oauth2/userinfo. It validates the
// Bearer access token via JWTManager and returns the standard claim
// set for the user.
func (i *Issuer) UserinfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authz := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="missing bearer token"`)
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimSpace(authz[len(prefix):])
	claims, err := i.JWT.ValidateAccessToken(token)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="token validation failed"`)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		http.Error(w, "bad token sub", http.StatusUnauthorized)
		return
	}
	user, err := i.UserStore.GetUserByID(r.Context(), uid)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":                user.ID.String(),
		"email":              user.Email,
		"email_verified":     user.IsActive,
		"name":               user.DisplayName,
		"preferred_username": user.Email,
	})
}
