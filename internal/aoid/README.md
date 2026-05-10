# aoid — AO ID identity service (scaffold)

`aoid` is the standalone Go service that hosts the AO Cyber portfolio's
identity stack. It composes three platform packages —
[`platform/auth`](../../platform/auth), [`platform/household`](../../platform/household)
and [`platform/consent`](../../platform/consent) — behind a single deployable
HTTP service that products federate into.

This README covers Objective 29: the **scaffolding** milestone. The
service runs, its OIDC discovery document and JWKS endpoint are reachable
and federation tooling can probe them, but token issuance itself is
deliberately **off** until Objective 30 turns it on. The `/oauth2/*`
endpoints reply 503 with a documented error body in the meantime.

## Service location

`aoid` lives inside `eden-platform-go` as a second `cmd/` binary
(sibling of `cmd/eden-platform-dev`) rather than its own repository.
Rationale and trade-offs are documented in
[`.planning/objectives/29-aoid-service-scaffold/29-01-TRD.md`](../../../.planning/objectives/29-aoid-service-scaffold/29-01-TRD.md).
Promoting to a dedicated repo is mechanical when the service stabilises;
keeping it co-located now means platform package changes flow without a
release dance.

## Boot

```bash
# Local dev — in-memory devstore, seeded fixtures, ephemeral JWT keys.
go run ./cmd/aoid

# Production / staging — Postgres-backed.
export AOID_DATABASE_URL=postgres://aoid:secret@db:5432/aoid?sslmode=require
export AOID_ISSUER=https://id.aocyber.ai
export AOID_JWT_KEY_SEED_PATHS="2026-Q2=/etc/aoid/q2.seed,2026-Q3=/etc/aoid/q3.seed"
export AOID_JWT_ACTIVE_KID="2026-Q3"
./aoid
```

A graceful shutdown is triggered by SIGINT or SIGTERM; the server stops
accepting new connections, in-flight requests have `AOID_SHUTDOWN_TIMEOUT`
to drain (default 5s), and the audit logger flushes its event channel.

## Configuration

All configuration is environment-variable driven. See
[`internal/aoid/config/config.go`](./config/config.go) for the full surface.
Highlights:

| Env var | Default | Purpose |
|---|---|---|
| `AOID_LISTEN_ADDR` | `:8090` | HTTP bind address |
| `AOID_ISSUER` | `http://localhost:8090` | Canonical issuer URL emitted in discovery + JWTs |
| `AOID_DATABASE_URL` | empty (devstore) | Postgres DSN; empty selects the in-memory backend |
| `AOID_JWT_KEY_SEED_PATH` | empty | Single-key fallback path to a 32-byte ML-DSA-65 seed |
| `AOID_JWT_KEY_SEED_PATHS` | empty | Multi-key rotation map: `kid1=/path1,kid2=/path2` |
| `AOID_JWT_ACTIVE_KID` | empty | Required when `AOID_JWT_KEY_SEED_PATHS` is set; signs new tokens |
| `AOID_LOG_LEVEL` | `info` | slog level (`debug`, `info`, `warn`, `error`) |
| `AOID_LOG_FORMAT` | `text` | slog format (`text` or `json`) |
| `AOID_SENTRY_DSN` | empty | Forwards error-level logs to Sentry; empty = no-op |
| `AOID_SHUTDOWN_TIMEOUT` | `5s` | Graceful-shutdown deadline |

Empty `JWT_KEY_SEED_PATH(S)` puts the JWT manager in **ephemeral** mode —
tokens signed with a freshly-generated key per process. Useful for tests;
NEVER do this in production.

## Endpoints

| Path | Status | Behaviour |
|---|---|---|
| `/healthz` | 200 / 503 | JSON health document; 503 when any registered component is unhealthy |
| `/readyz` | 200 / 503 | 200 once the listener is up; 503 during startup or shutdown |
| `/.well-known/openid-configuration` | 200 | OIDC discovery doc with `service_status: "scaffold"` |
| `/.well-known/jwks.json` | 200 | JSON Web Key Set — one entry per registered ML-DSA-65 key |
| `/oauth2/token` | 503 | `error: issuer_not_active` until objective 30 |
| `/oauth2/authorize` | 503 | same |
| `/oauth2/userinfo` | 503 | same |

The discovery doc deliberately returns 200 rather than 503: federation
libraries probe metadata up front and we want them to find a parseable
document. The non-standard `service_status` field signals the situation.
The `/oauth2/*` endpoints are where relying parties get the truth.

## JWKS rotation

Key rotation is supported without restart:

1. Add a new `kid=path` entry to `AOID_JWT_KEY_SEED_PATHS` in your secret
   store.
2. Reload aoid (rolling deploy is sufficient — every replica picks up
   the new map at boot).
3. The new kid appears in `/.well-known/jwks.json` immediately.
4. Once all relying parties have re-fetched JWKS (within the
   `Cache-Control: max-age=300` window) flip `AOID_JWT_ACTIVE_KID` to the
   new kid in another rolling deploy.

Old kids stay in JWKS as long as their seed file is on disk so existing
tokens still verify; rotate them out of the map only after the relevant
token expiry has passed.

## Hand-off to objective 30

Objective 30 ("AO ID OIDC Issuer + AODex pilot") activates token
issuance:

- Replaces the `IssuerNotActive` handlers on `/oauth2/{token,authorize,userinfo}`
  with real OIDC code-flow + refresh-token implementations.
- Flips `service_status` in the discovery doc to `"active"`.
- Adds the AODex pilot migration so AODex's existing `internal/auth`
  delegates verification to AO ID JWTs.

Federation (SAML SP imports, SCIM provisioning, decommissioning per-product
session stores) is Objective 31.

## Testing

```bash
go test ./cmd/aoid/... ./internal/aoid/...
go vet ./cmd/aoid/... ./internal/aoid/...
```

CI runs the full module test suite plus a smoke step that boots the
binary and curls every endpoint. See
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml).
