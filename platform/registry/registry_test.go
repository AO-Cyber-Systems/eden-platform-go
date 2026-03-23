package registry

import (
	"testing"
)

func TestRegistry_RegisterAndGetNavItems(t *testing.T) {
	reg := New()
	reg.Register(&ModuleRegistration{
		Name: "crm",
		NavItems: []NavItem{
			{ID: "crm-contacts", Label: "Contacts", Feature: "crm", Priority: 1},
			{ID: "crm-deals", Label: "Deals", Feature: "crm", Priority: 2},
		},
	})
	reg.Register(&ModuleRegistration{
		Name: "hr",
		NavItems: []NavItem{
			{ID: "hr-employees", Label: "Employees", Feature: "hr", Priority: 1},
		},
	})

	// With CRM enabled
	items := reg.GetNavItems(map[string]bool{"crm": true})
	if len(items) != 2 {
		t.Errorf("GetNavItems(crm) = %d items, want 2", len(items))
	}

	// With both enabled
	items = reg.GetNavItems(map[string]bool{"crm": true, "hr": true})
	if len(items) != 3 {
		t.Errorf("GetNavItems(crm+hr) = %d items, want 3", len(items))
	}

	// With none enabled
	items = reg.GetNavItems(map[string]bool{})
	if len(items) != 0 {
		t.Errorf("GetNavItems(none) = %d items, want 0", len(items))
	}
}

func TestRegistry_NavItems_NoFeatureFilter(t *testing.T) {
	reg := New()
	reg.Register(&ModuleRegistration{
		Name: "global",
		NavItems: []NavItem{
			{ID: "dashboard", Label: "Dashboard", Feature: "", Priority: 0},
		},
	})

	items := reg.GetNavItems(map[string]bool{})
	if len(items) != 1 {
		t.Errorf("GetNavItems() = %d items, want 1 (no feature = always shown)", len(items))
	}
}

func TestRegistry_BadgeProvider(t *testing.T) {
	reg := New()
	reg.Register(&ModuleRegistration{
		Name: "helpdesk",
		BadgeProvider: func(companyID, userID string) int {
			return 5
		},
	})

	counts := reg.GetBadgeCounts("company-1", "user-1")
	if counts["helpdesk"] != 5 {
		t.Errorf("GetBadgeCounts()['helpdesk'] = %d, want 5", counts["helpdesk"])
	}
}

func TestRegistry_GetWidgets(t *testing.T) {
	reg := New()
	reg.Register(&ModuleRegistration{
		Name: "crm",
		Widgets: []Widget{
			{ID: "crm-pipeline", Label: "Pipeline", Feature: "crm"},
		},
	})

	widgets := reg.GetWidgets(map[string]bool{"crm": true})
	if len(widgets) != 1 {
		t.Errorf("GetWidgets(crm) = %d, want 1", len(widgets))
	}
}

func TestRegistry_GetSearchScopes(t *testing.T) {
	reg := New()
	reg.Register(&ModuleRegistration{
		Name: "crm",
		SearchScopes: []SearchScope{
			{ID: "contacts", Label: "Contacts", Feature: "crm"},
		},
	})

	scopes := reg.GetSearchScopes(map[string]bool{"crm": true})
	if len(scopes) != 1 {
		t.Errorf("GetSearchScopes(crm) = %d, want 1", len(scopes))
	}
}
