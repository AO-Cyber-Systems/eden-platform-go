package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNoCompany is returned when the request context does not carry a valid
// JWT-derived company_id. Handlers MUST translate this to HTTP 401 — never
// fall back to client-supplied values (query string, header, body).
var ErrNoCompany = errors.New("auth: no company in context")

// RequireCompany returns the JWT-derived company UUID from ctx. Fail-closed:
// if ctx has no claims, claims have an empty CompanyID, or CompanyID does
// not parse as a UUID, returns uuid.Nil and ErrNoCompany.
//
// Use as the FIRST line of every protected HTTP handler:
//
//	companyID, err := platformauth.RequireCompany(r.Context())
//	if err != nil {
//	    writeError(w, http.StatusUnauthorized, "missing company context")
//	    return
//	}
func RequireCompany(ctx context.Context) (uuid.UUID, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil || claims.CompanyID == "" {
		return uuid.Nil, ErrNoCompany
	}
	id, err := uuid.Parse(claims.CompanyID)
	if err != nil {
		return uuid.Nil, ErrNoCompany
	}
	return id, nil
}

// RequireUser returns the JWT-derived user UUID from ctx. Same fail-closed
// semantics as RequireCompany. Returns ErrNoCompany on any failure (the
// threat model is identical — no claims means no authenticated principal).
func RequireUser(ctx context.Context) (uuid.UUID, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == "" {
		return uuid.Nil, ErrNoCompany
	}
	id, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, ErrNoCompany
	}
	return id, nil
}
