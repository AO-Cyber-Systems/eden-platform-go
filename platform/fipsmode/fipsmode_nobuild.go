//go:build !fips140v1.0 && !fips140v1.26

package fipsmode

// buildTagPresent is false when this binary was NOT compiled with a supported
// GOFIPS140 version. MustRequire uses this flag as the compile-time half of its
// gate; the runtime crypto/fips140.Enabled() check is the other half.
var buildTagPresent = false
