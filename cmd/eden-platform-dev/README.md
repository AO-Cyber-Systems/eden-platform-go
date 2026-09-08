# eden-platform-dev — the dev playground

A single command that boots the Eden platform server with a seeded demo
tenant and hands you an already-authenticated session — no signup form,
no login screen, no manually-run migrations. Built across objective
`42-dev-playground` (TRDs 01-04).

```
just dev-quickstart
```

or, equivalently:

```
SERVER_ADDR=127.0.0.1:8091 go run -tags dev ./cmd/eden-platform-dev
```

Never `:8080` — this workspace fixes local verification at `127.0.0.1:8091`,
and `guardServerAddr` (below) will refuse to start on anything but loopback.

## What this IS

- A real `platform/server` instance — the same `RegisterPlatformHandlers`,
  the same Connect handlers, the same auth and RBAC interceptors production
  uses — running against an **in-memory** store backend
  (`platform/devstore`) instead of Postgres.
- Pre-seeded, on every boot, with one coherent demo tenant: a company, a
  user with a real Argon2id password hash, a company membership, and an
  owner-role RBAC binding (`devseed.go`).
- Pre-authenticated: at startup the server logs that seeded user in through
  the real production login path and prints a working token, base URL, and
  a copy-pasteable `curl` (`quickstart.go`), and separately exposes that
  same session at `GET /dev/session` for a client that wants zero
  interaction — not even reading a log line (`devsession.go`).

## What this is NOT

- **Not a staging environment.** There is no database. Nothing you do here
  persists anywhere.
- **Not durable.** All data lives in a `platform/devstore` in-memory
  backend. Restart the process and you get a fresh tenant, from scratch,
  every time.
- **Not reachable from anywhere but this machine.** `guardServerAddr`
  refuses to bind to anything other than a loopback address — including the
  common bare `:PORT` Go idiom, which binds every interface, not just
  loopback.
- **Not present in a production binary.** Every file specific to this
  playground compiles only under `-tags dev`; see "Safety model" below.
- **Not something to trust with real data or real credentials.** The seeded
  password (`devseed.go`'s `DevTenantPassword`) is a hardcoded, publicly
  known string, by design.

**Nothing here is new plumbing.** The `-db` flag on `cmd/eden-platform-dev`
already defaulted to `false` — an in-memory server — before objective 42.
What this objective added is the demo *tenant* (a company, a user, a
membership, an owner role) and the *session* (a real login performed for
you at boot). The in-memory server itself predates this work.

## Authorization is LIVE, not exempted

This is the point most worth stating plainly, because the failure mode in
the opposite direction — assuming a "dev playground" fakes its auth — would
make this tool useless for the thing it is actually for.

The token you get, from either the startup banner or `GET /dev/session`, is
minted by `mintDevSession` calling `auth.Service.Login` — the **exact**
production login path: real password verification against the seeded
Argon2id hash, a real company-membership lookup, real role resolution, a
genuine audit log entry, and a real access token signed by the same
`auth.JWTManager` every other request is validated against. Every RBAC
decision made against that token is a genuine decision by the real
`rbac.Enforcer`.

Concretely:

- `platform/server/interceptors.go` is untouched by this objective — byte
  identical, 0 changed lines.
- `server.DefaultPublicProcedures()` is untouched. The dev playground adds
  no public-procedure exemptions.
- `grep -rn "eden-platform-dev" platform/` returns nothing — the platform
  library has no dependency on, or awareness of, this binary at all.
- `integration_test.go`'s
  `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied`
  proves this two ways in one test: the seeded owner's token is *accepted*
  by the real interceptors for a permission it genuinely holds, and a
  second, genuinely under-privileged (viewer-role) user — authenticated
  through the same real login path — is *rejected* with
  `CodePermissionDenied`. Authorization is enforced, not mocked, for both
  directions.
- `devsession_test.go`'s `TestUpdateCompany_UnderPrivilegedUserIsDenied`
  (TRD 02) establishes the same guarantee at the unit level.

The only thing this playground removes is the *interactive* step of a human
typing a login form. It does not remove, weaken, or bypass a single
authorization decision.

## Safety model

Three independent layers, each sufficient on its own:

1. **Build tag isolation.** Every dev-only file (`devseed.go`,
   `devsession.go`, `quickstart.go`, and their tests) carries
   `//go:build dev`. Each has an inverse-tagged `_stub.go` twin
   (`//go:build !dev`) that no-ops the same function signature, so
   `main.go`'s call sites compile in every build mode without `main.go`
   itself needing a build tag. **A binary built without `-tags dev` does
   not contain this code at all** — it is not merely disabled at runtime,
   it is absent from the compiled artifact.
2. **Refuse-to-start guard.** `guardAgainstProductionConfig` (in
   `devsession.go`) runs unconditionally at boot, regardless of which
   storage backend flag was passed, and refuses to proceed if either:
   - `DATABASE_URL`'s host is not loopback (`guardDatabaseURL`) — this
     playground auto-issues a real, valid access token for a hardcoded
     password, so it must never run against anything resembling a real
     database.
   - `SERVER_ADDR` binds beyond loopback (`guardServerAddr`) — including a
     bare `:PORT` wildcard bind, which listens on every interface, not just
     `127.0.0.1`.
3. **Zero library dependency.** `grep -rn "eden-platform-dev" platform/`
   returns no matches. Nothing under `platform/` imports, references, or
   special-cases this binary. The playground depends on the platform
   library; the library does not know the playground exists.

## Verifying it yourself

```
go build ./... && go build -tags dev ./...
go test -tags dev ./cmd/eden-platform-dev/... -count=1 -race
```

The second command runs every test in this package, including:

- `devseed_test.go` — the seeded tenant is idempotent and coherent.
- `devsession_test.go` — the minted token is accepted by real interceptors
  (`TestMintDevSession_AcceptedByRealAuthInterceptor`), a missing token is
  rejected (`TestUpdateCompany_NoTokenIsUnauthenticated`), an
  under-privileged token is denied
  (`TestUpdateCompany_UnderPrivilegedUserIsDenied`), and the production-config
  guard refuses production-shaped input
  (`TestGuardAgainstProductionConfig`).
- `quickstart_test.go` — the printed banner names the real seeded identity
  and states dev-only/in-memory/data-loss in plain language, and the
  printed `curl` genuinely returns 2xx against a live in-process server
  (`TestQuickstartCurl_ExecutedAgainstRealServer_Returns2xx`), with a
  negative control proving an invalid token is rejected.
- `integration_test.go` (this TRD) — the full loop, against one live
  in-process server: **start → seeded session → authenticated call
  succeeds → unauthorized call is DENIED.** See "Closing TRD 03's gap"
  below.

## Closing TRD 03's gap

TRD 03 built and proved the printed startup `curl`, but its live-server
test only exercised the **login path** (`auth.Service.Login` → real
interceptors → real handler) — it never issued a request against the
literal `GET /dev/session` HTTP route, because `DevSession` /
`startDevSession` / `registerDevSessionEndpoint` live in `devsession.go`,
outside TRD 03's file ownership (see `42-03-SUMMARY.md`, "Closing TRD 02's
gap").

**This gap is now closed.** `integration_test.go` owns no such
restriction. `TestIntegration_StartSeededSessionAuthenticatedOK_UnauthorizedDenied`
stands up a live `httptest.Server` that mounts `/dev/session` through the
real `startDevSession` entry point — the identical function `main.go` calls
at boot — issues a real `net/http` `GET` against that literal route,
decodes a usable `DevSession`, and then uses that token exactly as a
zero-interaction client would: one successful authenticated call, one
correctly-denied unauthorized call.
`TestIntegration_DevSessionEndpoint_StableAcrossRepeatedRequests`
additionally proves the endpoint serves one pre-minted session across
repeated requests, not a fresh mint per hit.

## What's deferred

Two pieces, both **cross-repo** and deliberately out of scope for this
objective:

- **The eden-docs "try it live" bridge.** `eden-docs` lives in the
  `eden-libs` workspace — a separate module from `eden-platform-go`. Wiring
  generated API documentation to a live "try it" call against this
  playground is future cross-repo work, not something this objective builds.
- **A Flutter entry point.** Pointing `eden-platform-flutter` at this
  playground (so the example app can run against a seeded, authenticated
  backend with zero setup) is likewise a separate repo's work.

Also deferred, within this repo: in-memory backing for the platform's other
`Store` interfaces. Twenty packages under `platform/` define a `Store`
interface; `platform/devstore` currently backs five of them (auth, company,
rbac, audit, webhook) — the set `runServer` actually wires up. Extending
`devstore` to back the remaining fifteen is a natural, but unstarted, next
step for anyone who wants the playground to exercise more of the platform
surface.

## File map

| File | Build tag | Owns |
|---|---|---|
| `main.go` | (none — always compiles) | wiring; calls the dev-tagged functions below unconditionally, resolved by tag at compile time |
| `devseed.go` / `devseed_stub.go` | `dev` / `!dev` | the seeded demo tenant (company, user, membership, owner role) — TRD 01 |
| `devsession.go` / `devsession_stub.go` | `dev` / `!dev` | `mintDevSession` via `auth.Service.Login`, `guardAgainstProductionConfig`, the `GET /dev/session` endpoint — TRD 02 |
| `quickstart.go` / `quickstart_stub.go` | `dev` / `!dev` | the startup banner and the printed, working `curl` — TRD 03 |
| `README.md` | — | this file — TRD 04 |
| `integration_test.go` | `dev` | start → seeded session → authenticated OK → unauthorized DENIED, and the `/dev/session`-gap closure — TRD 04 |

The repo-root `justfile` (`eden-platform-go`'s own — not the `eden-libs`
workspace-root `justfile`, which is a different repository) provides the
`dev-quickstart` recipe used at the top of this document.
