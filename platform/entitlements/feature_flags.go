package entitlements

import (
	"context"
	"fmt"
)

// FeatureFlagClient checks feature flags via the entitlement client's bootstrap cache.
type FeatureFlagClient struct {
	entClient *EntitlementClient
}

// NewFeatureFlagClient creates a feature flag client backed by the given entitlement client.
func NewFeatureFlagClient(entClient *EntitlementClient) *FeatureFlagClient {
	return &FeatureFlagClient{entClient: entClient}
}

// IsEnabled returns whether a feature flag is enabled for a company.
// It reads from the bootstrap cache first; on miss it fetches the bootstrap.
func (f *FeatureFlagClient) IsEnabled(ctx context.Context, companyID, flagKey string) (bool, error) {
	flags, err := f.AllFlags(ctx, companyID)
	if err != nil {
		return false, err
	}
	return flags[flagKey], nil
}

// AllFlags returns all feature flags for a company as a key→enabled map.
func (f *FeatureFlagClient) AllFlags(ctx context.Context, companyID string) (map[string]bool, error) {
	bootstrap, err := f.entClient.Bootstrap(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("feature flags: %w", err)
	}

	result := make(map[string]bool, len(bootstrap.FeatureFlags))
	for _, flag := range bootstrap.FeatureFlags {
		result[flag.Key] = flag.Enabled
	}
	return result, nil
}
