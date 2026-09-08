---
objective: 42
name: zero-auth developer playground
date: 2026-09-08
status: complete
---

# Objective 42 — Research: zero-auth developer playground

## Question

Can a developer exercise real Eden platform features without provisioning Postgres,
without an IdP, and without signing in?

## Correction to the earlier sketch

An earlier pass through this idea claimed the PoC was "mostly composition" of
`platform/devstore` plus aoid's `devlogin`/`devseed`. That was **half right and half
wrong**, and both halves matter:

**More exists than claimed.** `cmd/eden-platform-dev` (315 LOC) is already a working
in-memory dev server. Its `-db` flag **defaults to false**, so the no-database path is
not a thing to build — it ships today. It already wires Auth, Company, Registry, RBAC,
Audit, Webhook and Bridge handlers, a JWT manager over a dev seed key, an SSO service,
auth + RBAC interceptors, `/up` and `/metrics`.

`devstore.Backend` is also richer than its filenames suggest: `memory_store.go` exposes
`AuthStore`, `CompanyStore`, `RBACStore`, `AuditStore`, `WebhookStore`, plus seeding
helpers `SeedRBACRole`, `SeedRBACPermission`, `SetRBACMembershipOverrides`, `SetSSOConfig`.

**Less is reusable than claimed.** aoid's `devlogin` is a **test harness**
(`BuildHandler`, `NewHarness`) for that repo's integration tests, not a reusable
synthetic-principal provider. `devseed` is a single `SeedDemoUser` for aoid's own schema.
Neither drops into platform as-is. Treat them as prior art, not as parts.

## The actual gap

`seedRBACData` in `cmd/eden-platform-dev` seeds roles, permissions and SSO configs — and
**no user, no company membership**. So:

1. **There is nobody to sign in as.** A developer cannot authenticate even if willing to.
2. **Every non-public procedure still demands a token**, because `NewAuthInterceptor` is
   wired with `DefaultPublicProcedures()`.

The distance between "runs without a database" (done) and "explore without signing in"
(not done) is therefore small and specific: seed a coherent demo tenant, and hand the
developer a working session without an interactive login.

## Why this is worth doing

`eden-docs` (objective 39) is explicitly a **static** portal — it shows what components
look like; nothing runs. `eden-cli` scaffolds a complete project that needs real infra.
There is nothing in between: no way to *use* a feature before committing to integrate it.
That gap is what deters evaluation.

## Scope

**V1:**
- Seed a complete, coherent demo tenant in devstore: user, company, membership, RBAC
  binding — enough that authorization decisions are real rather than bypassed.
- A zero-interaction dev session: a pre-minted token surfaced at startup and via a
  dev-only endpoint, so a client can call authenticated procedures immediately.
- One command to start, with a printed quickstart naming the seeded identity and token.

**Deferred:**
- The `eden-docs` "try it live" bridge — eden-docs lives in the **eden-libs** repo, a
  separate module. Cross-repo wiring is a follow-up, not this objective.
- A Flutter dev entry point (eden-platform-flutter, also a separate repo).
- In-memory twins for the other 15 store interfaces. 20 platform packages define a
  `Store` interface; devstore backs 5. Expanding that is its own objective, and V1 does
  not need it.

## The hard constraint

A zero-auth path is a **production catastrophe** if it can ever be switched on outside
dev. It must be impossible by construction, not by configuration discipline:

- The seeding and session-minting code must live behind a Go **build tag**, so it is not
  compiled into a production binary at all — an env var or config flag is not sufficient,
  because those can be set by accident.
- The dev binary must refuse to start if it detects production-shaped configuration
  (a real database URL, a non-loopback bind address).
- Nothing in `platform/` may import the dev-only package.

That last rule is what keeps this from leaking: the escape hatch lives in `cmd/`, not in
the library every product depends on.

## Pitfalls

- **Do not bypass the interceptors.** The value of the playground is that RBAC and auth
  behave *exactly* as in production. Minting a real token for a real seeded user preserves
  that; making procedures public does not, and would teach developers a false model.
- **Do not weaken `DefaultPublicProcedures`.** That list is a production security surface.
- Port 8091 for any preview server; never 8080 (occupied on the operator's machine).
- `go mod tidy` without `-e` is broken in this repo for a pre-existing reason
  (`platform/audit` -> `otel/sdk/log` test binary -> a package deleted in otel v1.46.0).
