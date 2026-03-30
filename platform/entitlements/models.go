// Package entitlements provides an HTTP client and middleware for checking
// subscriptions, entitlements, and feature flags against an Eden Biz backend.
//
// The client is lightweight — it depends only on net/http and the standard library.
// Any Go service (aodex-go, aosentry, etc.) can import it without pulling in
// database drivers or protobuf dependencies.
package entitlements

// BootstrapResponse is the unified response from Eden Biz's bootstrap endpoint.
// It contains everything a service needs to gate features for a company.
type BootstrapResponse struct {
	Subscription *Subscription      `json:"subscription"`
	Plan         *Plan              `json:"plan"`
	Entitlements []EntitlementEntry `json:"entitlements"`
	FeatureFlags []FeatureFlag      `json:"feature_flags"`
}

// Plan describes a subscription plan.
type Plan struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Interval string `json:"interval"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Features any    `json:"features,omitempty"`
}

// Subscription describes an active subscription.
type Subscription struct {
	ID                 string `json:"id"`
	PlanID             string `json:"plan_id"`
	Status             string `json:"status"`
	CurrentPeriodStart string `json:"current_period_start"`
	CurrentPeriodEnd   string `json:"current_period_end"`
}

// EntitlementEntry describes a single feature entitlement with optional quota info.
type EntitlementEntry struct {
	FeatureKey    string `json:"feature_key"`
	FeatureType   string `json:"feature_type"` // "boolean" or "quota"
	Allowed       bool   `json:"allowed"`
	IncludedUnits *int64 `json:"included_units,omitempty"`
	UsedUnits     *int64 `json:"used_units,omitempty"`
	Remaining     *int64 `json:"remaining,omitempty"`
	SoftCap       bool   `json:"soft_cap,omitempty"`
}

// EntitlementResult is the response from a single entitlement check.
type EntitlementResult struct {
	Allowed   bool   `json:"allowed"`
	Remaining *int64 `json:"remaining,omitempty"`
	SoftCap   bool   `json:"soft_cap,omitempty"`
}

// FeatureFlag describes a feature flag state.
type FeatureFlag struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}
