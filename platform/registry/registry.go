package registry

import (
	"sync"
)

// NavItem represents a sidebar navigation item.
type NavItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Icon     string `json:"icon"`
	Path     string `json:"path"`
	Feature  string `json:"feature"`
	Priority int    `json:"priority"`
}

// Widget represents a dashboard widget registration.
type Widget struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Feature  string `json:"feature"`
	Priority int    `json:"priority"`
}

// SearchScope represents a registered search scope.
type SearchScope struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Feature string `json:"feature"`
}

// BadgeProvider returns a badge count for a feature.
type BadgeProvider func(companyID, userID string) int

// ModuleRegistration holds all registrations for a module.
type ModuleRegistration struct {
	Name          string
	NavItems      []NavItem
	Widgets       []Widget
	SearchScopes  []SearchScope
	BadgeProvider BadgeProvider
}

// Registry is a thread-safe module registry.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]*ModuleRegistration
}

// New creates a new module registry.
func New() *Registry {
	return &Registry{
		modules: make(map[string]*ModuleRegistration),
	}
}

// Register registers a module with its nav items, widgets, and search scopes.
func (r *Registry) Register(reg *ModuleRegistration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[reg.Name] = reg
}

// GetNavItems returns nav items filtered by enabled features and user permissions.
func (r *Registry) GetNavItems(enabledFeatures map[string]bool) []NavItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var items []NavItem
	for _, mod := range r.modules {
		for _, item := range mod.NavItems {
			if item.Feature == "" || enabledFeatures[item.Feature] {
				items = append(items, item)
			}
		}
	}
	return items
}

// GetWidgets returns widgets filtered by enabled features.
func (r *Registry) GetWidgets(enabledFeatures map[string]bool) []Widget {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var widgets []Widget
	for _, mod := range r.modules {
		for _, w := range mod.Widgets {
			if w.Feature == "" || enabledFeatures[w.Feature] {
				widgets = append(widgets, w)
			}
		}
	}
	return widgets
}

// GetSearchScopes returns search scopes filtered by enabled features.
func (r *Registry) GetSearchScopes(enabledFeatures map[string]bool) []SearchScope {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var scopes []SearchScope
	for _, mod := range r.modules {
		for _, s := range mod.SearchScopes {
			if s.Feature == "" || enabledFeatures[s.Feature] {
				scopes = append(scopes, s)
			}
		}
	}
	return scopes
}

// GetBadgeCounts returns badge counts for all modules.
func (r *Registry) GetBadgeCounts(companyID, userID string) map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)
	for name, mod := range r.modules {
		if mod.BadgeProvider != nil {
			counts[name] = mod.BadgeProvider(companyID, userID)
		}
	}
	return counts
}
