package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// buildVersion is set at compile time.
var buildVersion = "dev"

// HealthChecker holds references to dependencies for health checks.
type HealthChecker struct {
	mu         sync.RWMutex
	components map[string]HealthComponent
}

// HealthComponent defines an interface for health-checkable components.
type HealthComponent interface {
	HealthCheck(ctx context.Context) error
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		components: make(map[string]HealthComponent),
	}
}

// Register registers a component for health checking.
func (hc *HealthChecker) Register(name string, component HealthComponent) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.components[name] = component
}

// Check runs health checks against all registered components.
func (hc *HealthChecker) Check(ctx context.Context) (status string, components map[string]string) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	components = make(map[string]string)
	allHealthy := true

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	for name, component := range hc.components {
		if err := component.HealthCheck(checkCtx); err != nil {
			components[name] = fmt.Sprintf("unhealthy: %v", err)
			allHealthy = false
		} else {
			components[name] = "healthy"
		}
	}

	if allHealthy {
		return "healthy", components
	}
	return "degraded", components
}

// Handler returns an HTTP handler for the health endpoint.
func (hc *HealthChecker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		status, components := hc.Check(r.Context())

		resp := map[string]any{
			"status":     status,
			"version":    buildVersion,
			"components": components,
		}

		if status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode health response", "error", err)
		}
	}
}
