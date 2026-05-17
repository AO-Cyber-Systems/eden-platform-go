// Package fipsmode is the single source of truth for FIPS 140-3 build-setting and
// runtime gating across Eden services.
//
// Consumers (notably AOID's cmd/aoid boot orchestration) MUST call MustRequire as
// the first step of service boot, before any cryptographic operation runs. If the
// process was not built with GOFIPS140 or the runtime FIPS path is not active,
// MustRequire returns an error and the caller is expected to abort startup.
//
// SelfTest provides an additional service-readiness check that the FIPS-approved
// stdlib paths (ECDSA P-256, AES-256-GCM) work end-to-end inside this binary. It
// is NOT a duplicate of the CMVP-mandated module self-tests, which run in
// crypto/fips140's own init(); SelfTest is defense-in-depth.
//
// Canonical reference: https://go.dev/doc/security/fips140
//
// Build tags:
//
//   - fips140v1.0   — produced automatically when GOFIPS140=v1.0.0
//   - fips140v1.26  — produced automatically when GOFIPS140=v1.26.0
//
// fipsmode_build_tag.go is selected when either tag is set; fipsmode_nobuild.go is
// selected when neither tag is set. The buildTagPresent flag exposed by those two
// paired files is the compile-time signal that GOFIPS140 was supplied at build.
package fipsmode
