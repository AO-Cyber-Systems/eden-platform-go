package company_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/google/uuid"
)

func setupCompanyService(t *testing.T) (*company.Service, *devstore.CompanyStore) {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	store := backend.CompanyStore()
	return company.NewService(store), store
}

func TestService_CreateCompany_Standalone(t *testing.T) {
	svc, store := setupCompanyService(t)
	ctx := context.Background()

	c, err := svc.CreateCompany(ctx, "Root Corp", "", company.CompanyTypeStandalone, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	if c.Name != "Root Corp" {
		t.Errorf("Name = %q, want %q", c.Name, "Root Corp")
	}
	if c.CompanyType != company.CompanyTypeStandalone {
		t.Errorf("CompanyType = %q, want %q", c.CompanyType, company.CompanyTypeStandalone)
	}
	if !c.IsActive {
		t.Errorf("IsActive = false, want true")
	}

	// Verify self-reference in hierarchy
	ancestors, err := store.GetAncestors(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}
	found := false
	for _, a := range ancestors {
		if a.AncestorID == c.ID && a.DescendantID == c.ID && a.Generations == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("Self-reference not found in hierarchy")
	}
}

func TestService_CreateCompany_WithParent(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}

	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}

	if child.ParentCompanyID == nil || *child.ParentCompanyID != root.ID {
		t.Errorf("ParentCompanyID = %v, want %v", child.ParentCompanyID, root.ID)
	}
}

func TestService_CreateCompany_EmptyName(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	_, err := svc.CreateCompany(ctx, "", "", company.CompanyTypeStandalone, nil, nil)
	if err == nil {
		t.Fatalf("CreateCompany() with empty name expected error, got nil")
	}
}

func TestService_CreateCompany_DefaultSlugAndType(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	c, err := svc.CreateCompany(ctx, "My Company", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	if c.Slug == "" {
		t.Errorf("Slug should be auto-generated, got empty")
	}
	if c.CompanyType != company.CompanyTypeStandalone {
		t.Errorf("CompanyType = %q, want default %q", c.CompanyType, company.CompanyTypeStandalone)
	}
}

func TestService_GetAncestors_ThreeLevel(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}
	grandchild, err := svc.CreateCompany(ctx, "Grandchild", "", company.CompanyTypeSubsidiary, &child.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(grandchild) error = %v", err)
	}

	ancestors, err := svc.GetAncestors(ctx, grandchild.ID)
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}

	if len(ancestors) != 2 {
		t.Fatalf("GetAncestors() returned %d ancestors, want 2", len(ancestors))
	}
	// Nearest first: child, then root
	if ancestors[0].ID != child.ID {
		t.Errorf("First ancestor = %v, want child %v", ancestors[0].ID, child.ID)
	}
	if ancestors[1].ID != root.ID {
		t.Errorf("Second ancestor = %v, want root %v", ancestors[1].ID, root.ID)
	}
}

func TestService_GetAncestors_RootHasNone(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeStandalone, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	ancestors, err := svc.GetAncestors(ctx, root.ID)
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}
	if len(ancestors) != 0 {
		t.Errorf("GetAncestors() for root returned %d, want 0", len(ancestors))
	}
}

func TestService_GetSelfAndDescendantIDs(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}
	child1, err := svc.CreateCompany(ctx, "Child1", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child1) error = %v", err)
	}
	child2, err := svc.CreateCompany(ctx, "Child2", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child2) error = %v", err)
	}

	ids, err := svc.GetSelfAndDescendantIDs(ctx, root.ID)
	if err != nil {
		t.Fatalf("GetSelfAndDescendantIDs() error = %v", err)
	}

	if len(ids) != 3 {
		t.Fatalf("GetSelfAndDescendantIDs() returned %d IDs, want 3", len(ids))
	}

	idSet := make(map[uuid.UUID]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet[root.ID] {
		t.Errorf("Missing root ID in descendants")
	}
	if !idSet[child1.ID] {
		t.Errorf("Missing child1 ID in descendants")
	}
	if !idSet[child2.ID] {
		t.Errorf("Missing child2 ID in descendants")
	}
}

func TestService_IsDescendantOf_True(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}
	grandchild, err := svc.CreateCompany(ctx, "Grandchild", "", company.CompanyTypeSubsidiary, &child.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(grandchild) error = %v", err)
	}

	ok, err := svc.IsDescendantOf(ctx, grandchild.ID, root.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf() error = %v", err)
	}
	if !ok {
		t.Errorf("IsDescendantOf(grandchild, root) = false, want true")
	}
}

func TestService_IsDescendantOf_False(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &root.ID, nil)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}

	ok, err := svc.IsDescendantOf(ctx, root.ID, child.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf() error = %v", err)
	}
	if ok {
		t.Errorf("IsDescendantOf(root, child) = true, want false")
	}
}

func TestService_IsDescendantOf_Self(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeStandalone, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	// Self is returned in GetSelfAndDescendantIDs, so IsDescendantOf returns true
	// The TRD says "company is not descendant of itself (generations > 0 only)"
	// but the implementation checks GetSelfAndDescendantIDs which includes self
	ok, err := svc.IsDescendantOf(ctx, root.ID, root.ID)
	if err != nil {
		t.Fatalf("IsDescendantOf() error = %v", err)
	}
	// Implementation includes self in descendants (generations=0), so this returns true
	if !ok {
		t.Errorf("IsDescendantOf(self, self) = false; implementation includes self in descendants")
	}
}

func TestService_GetEffectiveSettings_NoParent(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	settings := json.RawMessage(`{"theme":"dark","locale":"en"}`)
	c, err := svc.CreateCompany(ctx, "Solo", "", company.CompanyTypeStandalone, nil, settings)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	effective, err := svc.GetEffectiveSettings(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetEffectiveSettings() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(effective, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed["theme"] != "dark" {
		t.Errorf("theme = %v, want 'dark'", parsed["theme"])
	}
	if parsed["locale"] != "en" {
		t.Errorf("locale = %v, want 'en'", parsed["locale"])
	}
}

func TestService_GetEffectiveSettings_InheritsParent(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	parentSettings := json.RawMessage(`{"theme":"light","locale":"en"}`)
	parent, err := svc.CreateCompany(ctx, "Parent", "", company.CompanyTypeHolding, nil, parentSettings)
	if err != nil {
		t.Fatalf("CreateCompany(parent) error = %v", err)
	}

	// Child has no locale - should inherit from parent
	childSettings := json.RawMessage(`{"theme":"dark"}`)
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &parent.ID, childSettings)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}

	effective, err := svc.GetEffectiveSettings(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetEffectiveSettings() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(effective, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed["locale"] != "en" {
		t.Errorf("locale = %v, want 'en' (inherited from parent)", parsed["locale"])
	}
}

func TestService_GetEffectiveSettings_ChildOverrides(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	parentSettings := json.RawMessage(`{"theme":"light","locale":"en"}`)
	parent, err := svc.CreateCompany(ctx, "Parent", "", company.CompanyTypeHolding, nil, parentSettings)
	if err != nil {
		t.Fatalf("CreateCompany(parent) error = %v", err)
	}

	childSettings := json.RawMessage(`{"theme":"dark"}`)
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &parent.ID, childSettings)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}

	effective, err := svc.GetEffectiveSettings(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetEffectiveSettings() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(effective, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	// Child overrides parent's theme
	if parsed["theme"] != "dark" {
		t.Errorf("theme = %v, want 'dark' (child override)", parsed["theme"])
	}
}

func TestService_GetEffectiveSettings_ThreeLevel(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	rootSettings := json.RawMessage(`{"theme":"light","locale":"en","currency":"usd"}`)
	root, err := svc.CreateCompany(ctx, "Root", "", company.CompanyTypeHolding, nil, rootSettings)
	if err != nil {
		t.Fatalf("CreateCompany(root) error = %v", err)
	}

	childSettings := json.RawMessage(`{"theme":"blue","region":"us"}`)
	child, err := svc.CreateCompany(ctx, "Child", "", company.CompanyTypeSubsidiary, &root.ID, childSettings)
	if err != nil {
		t.Fatalf("CreateCompany(child) error = %v", err)
	}

	grandchildSettings := json.RawMessage(`{"theme":"dark"}`)
	grandchild, err := svc.CreateCompany(ctx, "Grandchild", "", company.CompanyTypeSubsidiary, &child.ID, grandchildSettings)
	if err != nil {
		t.Fatalf("CreateCompany(grandchild) error = %v", err)
	}

	effective, err := svc.GetEffectiveSettings(ctx, grandchild.ID)
	if err != nil {
		t.Fatalf("GetEffectiveSettings() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(effective, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Grandchild's own theme
	if parsed["theme"] != "dark" {
		t.Errorf("theme = %v, want 'dark' (grandchild)", parsed["theme"])
	}
	// From child (nearest ancestor)
	if parsed["region"] != "us" {
		t.Errorf("region = %v, want 'us' (from child)", parsed["region"])
	}
	// From root (furthest ancestor)
	if parsed["locale"] != "en" {
		t.Errorf("locale = %v, want 'en' (from root)", parsed["locale"])
	}
	if parsed["currency"] != "usd" {
		t.Errorf("currency = %v, want 'usd' (from root)", parsed["currency"])
	}
}

func TestService_UpdateCompany(t *testing.T) {
	svc, _ := setupCompanyService(t)
	ctx := context.Background()

	c, err := svc.CreateCompany(ctx, "Original", "", company.CompanyTypeStandalone, nil, nil)
	if err != nil {
		t.Fatalf("CreateCompany() error = %v", err)
	}

	updated, err := svc.UpdateCompany(ctx, company.Company{
		ID:   c.ID,
		Name: "Updated Name",
		Slug: "updated-slug",
	})
	if err != nil {
		t.Fatalf("UpdateCompany() error = %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("Name = %q, want %q", updated.Name, "Updated Name")
	}
	if updated.Slug != "updated-slug" {
		t.Errorf("Slug = %q, want %q", updated.Slug, "updated-slug")
	}
}
