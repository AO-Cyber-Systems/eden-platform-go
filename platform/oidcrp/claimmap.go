package oidcrp

import (
	"fmt"
	"strings"
)

// ClaimMap describes how OIDC claims map onto AOID's account model. Each
// field is a dotted-path string into the claims map ("email",
// "profile.email", "address.country"). The default mapping for vanilla
// OIDC providers is:
//
//	ClaimMap{
//	    Email:             "email",
//	    EmailVerified:     "email_verified",
//	    Sub:               "sub",
//	    PreferredUsername: "preferred_username",
//	    Name:              "name",
//	}
//
// Per-IdP overrides (e.g., Okta exposing groups under "groups" but Azure
// exposing them under "wids") are expressed by populating the corresponding
// field. Empty string means "do not extract this field".
//
// CustomAttrs lets callers pull arbitrary claims into the returned
// MappedClaims.Custom map under a user-chosen key. The key is the map key
// in Custom; the value is the dotted path to resolve.
type ClaimMap struct {
	Email             string
	EmailVerified     string
	Sub               string
	PreferredUsername string
	Name              string
	Groups            string
	AssuranceLevel    string

	CustomAttrs map[string]string
}

// MappedClaims is the normalized result of ApplyClaimMap. Required fields
// (Email, Sub) are guaranteed non-empty when ApplyClaimMap returns nil
// error.
type MappedClaims struct {
	Email             string
	EmailVerified     bool
	Sub               string
	PreferredUsername string
	Name              string
	Groups            []string
	AssuranceLevel    string

	Custom map[string]any
}

// ApplyClaimMap walks each non-empty path in m and resolves it against
// claims via a dotted-path lookup. Email and Sub are REQUIRED — if the
// resolved value is missing or not a string, ApplyClaimMap returns an
// error wrapping ErrMissingRequiredClaim with the field name embedded
// for caller-side logging.
//
// Type coercion rules:
//   - String fields: must resolve to a string (or nothing, if optional).
//   - EmailVerified: must resolve to bool, defaults to false if absent.
//   - Groups: accepts []any of strings (typical OIDC shape), []string,
//     or a single string (which becomes a 1-element slice).
//   - CustomAttrs: stored as-is in Custom — caller handles further coercion.
func ApplyClaimMap(claims map[string]any, m ClaimMap) (MappedClaims, error) {
	var out MappedClaims

	// Email (REQUIRED).
	if m.Email != "" {
		v, ok := lookup(claims, m.Email)
		if !ok {
			return out, fmt.Errorf("%w: email (path=%q)", ErrMissingRequiredClaim, m.Email)
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return out, fmt.Errorf("%w: email (path=%q, type=%T)", ErrMissingRequiredClaim, m.Email, v)
		}
		out.Email = s
	} else {
		return out, fmt.Errorf("%w: email (path not configured)", ErrMissingRequiredClaim)
	}

	// Sub (REQUIRED).
	if m.Sub != "" {
		v, ok := lookup(claims, m.Sub)
		if !ok {
			return out, fmt.Errorf("%w: sub (path=%q)", ErrMissingRequiredClaim, m.Sub)
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return out, fmt.Errorf("%w: sub (path=%q, type=%T)", ErrMissingRequiredClaim, m.Sub, v)
		}
		out.Sub = s
	} else {
		return out, fmt.Errorf("%w: sub (path not configured)", ErrMissingRequiredClaim)
	}

	// EmailVerified (optional, defaults false).
	if m.EmailVerified != "" {
		if v, ok := lookup(claims, m.EmailVerified); ok {
			if b, isBool := v.(bool); isBool {
				out.EmailVerified = b
			}
			// If present but wrong type, silently default to false — IdPs
			// occasionally emit "true"/"false" as strings, which we treat
			// as untrusted. Caller can configure stricter custom mapping.
		}
	}

	// PreferredUsername (optional).
	if m.PreferredUsername != "" {
		if v, ok := lookup(claims, m.PreferredUsername); ok {
			if s, ok := v.(string); ok {
				out.PreferredUsername = s
			}
		}
	}

	// Name (optional).
	if m.Name != "" {
		if v, ok := lookup(claims, m.Name); ok {
			if s, ok := v.(string); ok {
				out.Name = s
			}
		}
	}

	// Groups (optional, polymorphic).
	if m.Groups != "" {
		if v, ok := lookup(claims, m.Groups); ok {
			out.Groups = coerceGroups(v)
		}
	}

	// AssuranceLevel (optional).
	if m.AssuranceLevel != "" {
		if v, ok := lookup(claims, m.AssuranceLevel); ok {
			if s, ok := v.(string); ok {
				out.AssuranceLevel = s
			}
		}
	}

	// CustomAttrs (optional, free-form).
	if len(m.CustomAttrs) > 0 {
		out.Custom = make(map[string]any, len(m.CustomAttrs))
		for key, path := range m.CustomAttrs {
			if v, ok := lookup(claims, path); ok {
				out.Custom[key] = v
			}
		}
	}

	return out, nil
}

// lookup resolves a dotted path ("a.b.c") against a nested map[string]any.
// Returns (value, true) on success; (nil, false) on any miss along the path.
//
// Intermediate elements MUST be map[string]any — JSON-decoded nesting via
// encoding/json yields this shape. We do not attempt to traverse arrays
// via numeric indices; the few callers needing that resolve manually.
func lookup(claims map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur any = claims
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, exists := m[p]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// coerceGroups normalizes a claim value into []string. Accepts []any of
// strings (the JSON shape), []string (already typed), or a single string
// (promoted to a 1-element slice).
func coerceGroups(v any) []string {
	switch g := v.(type) {
	case []string:
		return g
	case []any:
		out := make([]string, 0, len(g))
		for _, item := range g {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{g}
	default:
		return nil
	}
}
