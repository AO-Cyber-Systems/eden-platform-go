package oidcrp

import (
	"errors"
	"testing"
)

func TestApplyClaimMap_FlatClaims(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email":              "alice@example.com",
		"email_verified":     true,
		"sub":                "user-42",
		"preferred_username": "alice",
		"name":               "Alice Example",
	}
	m := ClaimMap{
		Email:             "email",
		EmailVerified:     "email_verified",
		Sub:               "sub",
		PreferredUsername: "preferred_username",
		Name:              "name",
	}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q", got.Email)
	}
	if !got.EmailVerified {
		t.Errorf("EmailVerified = false, want true")
	}
	if got.Sub != "user-42" {
		t.Errorf("Sub = %q", got.Sub)
	}
	if got.PreferredUsername != "alice" {
		t.Errorf("PreferredUsername = %q", got.PreferredUsername)
	}
	if got.Name != "Alice Example" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestApplyClaimMap_NestedDottedPath(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"profile": map[string]any{
			"email": "nested@example.com",
		},
		"sub": "u-1",
	}
	m := ClaimMap{
		Email: "profile.email",
		Sub:   "sub",
	}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if got.Email != "nested@example.com" {
		t.Errorf("Email = %q", got.Email)
	}
}

func TestApplyClaimMap_GroupsAsStringSlice(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email":  "x@y",
		"sub":    "s",
		"groups": []any{"admin", "engineering"},
	}
	m := ClaimMap{Email: "email", Sub: "sub", Groups: "groups"}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if len(got.Groups) != 2 || got.Groups[0] != "admin" || got.Groups[1] != "engineering" {
		t.Errorf("Groups = %v", got.Groups)
	}
}

func TestApplyClaimMap_GroupsAsSingleString(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email":  "x@y",
		"sub":    "s",
		"groups": "admin",
	}
	m := ClaimMap{Email: "email", Sub: "sub", Groups: "groups"}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "admin" {
		t.Errorf("Groups = %v, want [admin]", got.Groups)
	}
}

func TestApplyClaimMap_MissingEmailReturnsErr(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"sub": "s",
	}
	m := ClaimMap{Email: "email", Sub: "sub"}
	_, err := ApplyClaimMap(claims, m)
	if !errors.Is(err, ErrMissingRequiredClaim) {
		t.Fatalf("expected ErrMissingRequiredClaim, got: %v", err)
	}
	if err.Error() == "" || !contains(err.Error(), "email") {
		t.Errorf("error should name missing field 'email': %v", err)
	}
}

func TestApplyClaimMap_MissingSubReturnsErr(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email": "x@y",
	}
	m := ClaimMap{Email: "email", Sub: "sub"}
	_, err := ApplyClaimMap(claims, m)
	if !errors.Is(err, ErrMissingRequiredClaim) {
		t.Fatalf("expected ErrMissingRequiredClaim, got: %v", err)
	}
	if !contains(err.Error(), "sub") {
		t.Errorf("error should name missing field 'sub': %v", err)
	}
}

func TestApplyClaimMap_EmailVerifiedDefaultsFalse(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"email": "x@y", "sub": "s"}
	m := ClaimMap{Email: "email", Sub: "sub", EmailVerified: "email_verified"}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if got.EmailVerified {
		t.Errorf("EmailVerified = true, want false")
	}
}

func TestApplyClaimMap_CustomAttrs(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email": "x@y",
		"sub":   "s",
		"address": map[string]any{
			"country": "US",
		},
		"tid": "tenant-77",
	}
	m := ClaimMap{
		Email: "email",
		Sub:   "sub",
		CustomAttrs: map[string]string{
			"country":   "address.country",
			"tenant_id": "tid",
		},
	}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if got.Custom["country"] != "US" {
		t.Errorf("country = %v", got.Custom["country"])
	}
	if got.Custom["tenant_id"] != "tenant-77" {
		t.Errorf("tenant_id = %v", got.Custom["tenant_id"])
	}
}

func TestApplyClaimMap_AssuranceLevel(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"email": "x@y",
		"sub":   "s",
		"acr":   "urn:idmgmt:ial:2",
	}
	m := ClaimMap{Email: "email", Sub: "sub", AssuranceLevel: "acr"}
	got, err := ApplyClaimMap(claims, m)
	if err != nil {
		t.Fatalf("ApplyClaimMap: %v", err)
	}
	if got.AssuranceLevel != "urn:idmgmt:ial:2" {
		t.Errorf("AssuranceLevel = %q", got.AssuranceLevel)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
