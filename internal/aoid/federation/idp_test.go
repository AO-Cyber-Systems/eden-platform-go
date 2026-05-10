package federation

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/auth/saml/idp"
	"github.com/google/uuid"
)

func newTestManager(t *testing.T) (*IdPManager, *InMemoryRegistry) {
	t.Helper()
	reg := NewInMemoryRegistry()
	resolver, err := MustGenerateSharedKey("AO ID Federation Test")
	if err != nil {
		t.Fatalf("MustGenerateSharedKey: %v", err)
	}
	mgr, err := NewIdPManager(reg, resolver)
	if err != nil {
		t.Fatalf("NewIdPManager: %v", err)
	}
	return mgr, reg
}

func TestIdPManager_GetUnknownTenant(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.Get(context.Background(), uuid.New())
	if !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestIdPManager_GetInactiveTenant(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	cfg.IsActive = false
	_ = reg.Register(context.Background(), cfg)

	_, err := mgr.Get(context.Background(), cfg.TenantID)
	if !errors.Is(err, ErrTenantInactive) {
		t.Errorf("expected ErrTenantInactive, got %v", err)
	}
}

func TestIdPManager_GetCaches(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	_ = reg.Register(context.Background(), cfg)

	first, err := mgr.Get(context.Background(), cfg.TenantID)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := mgr.Get(context.Background(), cfg.TenantID)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if first != second {
		t.Errorf("expected pointer identity (cache hit), got distinct instances")
	}
}

func TestIdPManager_InvalidateRebuilds(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	_ = reg.Register(context.Background(), cfg)

	first, _ := mgr.Get(context.Background(), cfg.TenantID)
	mgr.Invalidate(cfg.TenantID)
	second, _ := mgr.Get(context.Background(), cfg.TenantID)
	if first == second {
		t.Errorf("expected rebuilt instance after Invalidate, got same pointer")
	}
}

func TestIdPManager_Metadata(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	_ = reg.Register(context.Background(), cfg)

	body, err := mgr.Metadata(context.Background(), cfg.TenantID)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, cfg.EntityID) {
		t.Errorf("metadata missing EntityID %q", cfg.EntityID)
	}
	if !strings.Contains(bodyStr, "HTTP-Redirect") || !strings.Contains(bodyStr, "HTTP-POST") {
		t.Errorf("metadata missing SSO bindings")
	}
	// Must be parseable XML.
	var doc struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Errorf("metadata unmarshal: %v", err)
	}
}

func TestIdPManager_IssueAssertion(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	_ = reg.Register(context.Background(), cfg)

	// First, exercise the AssertionLifetime helper for coverage.
	lifetime, err := mgr.AssertionLifetime(context.Background(), cfg.TenantID)
	if err != nil {
		t.Fatalf("AssertionLifetime: %v", err)
	}
	if lifetime <= 0 {
		t.Errorf("AssertionLifetime must be positive, got %s", lifetime)
	}

	// Build an AssertionInput targeting the registered SP.
	spEntity := "https://partner.example.com/saml/metadata"
	signed, err := mgr.IssueAssertion(context.Background(), cfg.TenantID, idp.AssertionInput{
		SPEntityID: spEntity,
		NameID:     "user@acme.com",
		Attributes: map[string][]string{
			"email": {"user@acme.com"},
		},
	})
	if err != nil {
		t.Fatalf("IssueAssertion: %v", err)
	}
	if !strings.Contains(string(signed), "user@acme.com") {
		t.Errorf("signed response missing NameID")
	}
	if !strings.Contains(string(signed), spEntity) {
		t.Errorf("signed response missing SP audience")
	}
}

func TestIdPManager_AttributeTemplate(t *testing.T) {
	mgr, reg := newTestManager(t)
	cfg := newTenantConfig(t)
	_ = reg.Register(context.Background(), cfg)
	tpl, err := mgr.AttributeTemplate(context.Background(), cfg.TenantID)
	if err != nil {
		t.Fatalf("AttributeTemplate: %v", err)
	}
	if got := tpl["email"]; len(got) != 1 || got[0] != "email" {
		t.Errorf("email template: got %v", got)
	}
}

func TestSharedKeyResolver_NilReturns(t *testing.T) {
	var r *SharedKeyResolver
	if _, _, err := r.Resolve(context.Background(), uuid.New()); err == nil {
		t.Errorf("nil resolver should error")
	}
	r = &SharedKeyResolver{}
	if _, _, err := r.Resolve(context.Background(), uuid.New()); err == nil {
		t.Errorf("resolver without key should error")
	}
}

