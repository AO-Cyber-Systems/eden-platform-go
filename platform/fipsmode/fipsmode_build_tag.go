//go:build fips140v1.0 || fips140v1.26

package fipsmode

// buildTagPresent is true when this binary was compiled with GOFIPS140 set to a
// supported version (v1.0.x or v1.26.x). The Go toolchain appends one of the
// fips140vMAJOR.MINOR build tags automatically; this file is selected by build
// constraint when that tag fires.
var buildTagPresent = true
