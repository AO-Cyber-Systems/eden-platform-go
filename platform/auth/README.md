# platform/auth

Authentication primitives for the AOC platform. Used by AODex, AOSentry,
eden-biz, AO ID, and AOFamily backends.

## Layout

```
platform/auth/
├── (root package)        Service/JWT/SSOService/PasswordHasher/Require* —
│                          full-fat orchestration layer
│   ├── jwt.go            ML-DSA-65 (post-quantum) signed JWTs with rotation
│   ├── service.go        SignUp / Login / RefreshToken / Logout / UpdateProfile
│   ├── sso.go            OIDC + SAML SP wrapper using crewjam/saml + go-oidc
│   ├── password.go       Argon2id PasswordHasher (recommended for new work)
│   ├── legacy_bcrypt.go  bcrypt helpers (Devise-compatible) for migration
│   ├── require.go        HTTP middleware
│   ├── store.go          AuthStore / TxAuthStore interfaces
│   ├── context.go        Caller identity in request context
│   └── mldsa.go          ML-DSA-65 signing method registration
│
├── apikey/      Bcrypt-hashed API keys with prefix lookup + scopes
├── emailotp/    6-digit constant-time email OTPs
├── oauth/       Provider-userinfo fetchers (Google, GitHub, FB, X, Apple, MS)
├── oidc/        Standalone OIDC SP primitives (Auth URL, exchange, verify)
├── saml/        Standalone SAML SP primitives (AuthnRequest, attr extraction,
│                metadata, IdP cert parsing)
├── session/     scs/pgxstore-backed cookie sessions + per-session metadata
├── totp/        RFC 6238 TOTP + bcrypt-hashed backup codes
└── webauthn/    go-webauthn wrapper + User type implementing webauthn.User
```

## When to use what

| Need | Package |
|------|---------|
| Sign / verify access + refresh tokens | root `auth.JWTManager` |
| Password verification (new code) | root `auth.PasswordHasher` (Argon2id) |
| Password verification (Devise-compat) | root `auth.VerifyLegacyPassword` |
| Tenant-configured OIDC/SAML SSO | root `auth.SSOService` |
| Roll-your-own OIDC SP flow | `platform/auth/oidc` |
| Roll-your-own SAML SP flow | `platform/auth/saml` |
| SAML IdP issuance | `platform/auth/saml/idp` (added in obj 23) |
| Userinfo from generic OAuth providers | `platform/auth/oauth` |
| Cookie sessions (web) | `platform/auth/session` |
| API-key authentication (machine-to-machine) | `platform/auth/apikey` |
| Passkey / WebAuthn ceremonies | `platform/auth/webauthn` |
| Authenticator-app TOTP + backup codes | `platform/auth/totp` |
| One-time codes via email | `platform/auth/emailotp` |

## Migration from AODex `internal/auth`

This package was promoted wholesale from `aodex-go/internal/auth/` per
decision D9 in `PORTFOLIO_STANDARDIZATION_PLAN.md`. AODex retains
`internal/auth/ws_token.go` (in-memory WebSocket auth, AODex-specific) but
every other primitive lives here. AODex itself becomes a re-export shim in a
follow-up workstream (`ws-aodex-auth-donor-shim`).

If you're still calling `aodex-go/internal/auth.*` directly, switch to
`github.com/aocybersystems/eden-platform-go/platform/auth` (or one of the
sub-packages above) at your earliest convenience.

## Rollback

See [ROLLBACK.md](./ROLLBACK.md) for the documented procedure if anything
in `platform/auth` regresses during the migration window.

## Test coverage

Every sub-package has standalone unit tests. Run them with:

```
go test ./platform/auth/...
```

Production-equivalent verification (with a real PostgreSQL pool for the
pgxstore-backed `session` and `apikey` packages) belongs in the consuming
service's e2e test suite — neither sub-package requires Postgres in unit
tests, so CI can run them anywhere.
