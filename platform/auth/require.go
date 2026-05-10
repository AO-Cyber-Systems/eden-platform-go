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

// ErrNoHousehold is returned when the request context does not carry a valid
// JWT-derived household_id. Handlers MUST translate this to HTTP 401.
//
// Used by household-shaped products (AOFamily, Eden Family). B2B consumers
// should keep using ErrNoCompany.
var ErrNoHousehold = errors.New("auth: no household in context")

// ErrNotParentMode is returned by RequireParentMode when the session is
// operating in child mode. Handlers MUST translate this to HTTP 403 —
// the principal is authenticated but lacks parent privileges for the
// requested action.
var ErrNotParentMode = errors.New("auth: not in parent mode")

// ErrNotChildMode is returned by RequireChildMode when the session is
// operating in parent mode (or has no household context). Handlers MUST
// translate this to HTTP 403 — the requested endpoint requires a
// child-mode session.
var ErrNotChildMode = errors.New("auth: not in child mode")

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

// RequireHousehold returns the JWT-derived household UUID from ctx.
// Fail-closed: if ctx has no claims, claims have an empty HouseholdID, or
// HouseholdID does not parse as a UUID, returns uuid.Nil and ErrNoHousehold.
//
// Use as the FIRST line of every household-scoped HTTP handler in
// AOFamily / Eden Family backends:
//
//	householdID, err := platformauth.RequireHousehold(r.Context())
//	if err != nil {
//	    writeError(w, http.StatusUnauthorized, "missing household context")
//	    return
//	}
func RequireHousehold(ctx context.Context) (uuid.UUID, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil || claims.HouseholdID == "" {
		return uuid.Nil, ErrNoHousehold
	}
	id, err := uuid.Parse(claims.HouseholdID)
	if err != nil {
		return uuid.Nil, ErrNoHousehold
	}
	return id, nil
}

// RequireParentMode returns the JWT claims when the session is NOT in
// child mode. Fail-closed: if ctx has no claims, returns ErrNoHousehold;
// if claims indicate ChildMode==true, returns ErrNotParentMode.
//
// Mirrors AOFamily-AI's existing internal/middleware/auth.go shape so
// consumers can swap their import without changing call-site logic.
//
// Note: B2B claims (no HouseholdID set, ChildMode defaults to false) pass
// through this check. RequireParentMode enforces "not in child mode," not
// "has household." Pair with RequireHousehold when both are required.
func RequireParentMode(ctx context.Context) (*Claims, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return nil, ErrNoHousehold
	}
	if claims.ChildMode {
		return nil, ErrNotParentMode
	}
	return claims, nil
}

// RequireChildMode returns the JWT claims when the session IS in child
// mode. Fail-closed: if ctx has no claims, returns ErrNoHousehold; if
// claims indicate ChildMode==false, returns ErrNotChildMode.
//
// Callers can read claims.ChildID directly from the returned Claims for
// the active child identity.
func RequireChildMode(ctx context.Context) (*Claims, error) {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return nil, ErrNoHousehold
	}
	if !claims.ChildMode {
		return nil, ErrNotChildMode
	}
	return claims, nil
}
