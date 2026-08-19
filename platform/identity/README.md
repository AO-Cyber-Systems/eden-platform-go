# platform/identity

The wire contract for a signed identity context, plus the two ends that speak it.

An identity context is a short-lived, signed assertion that something has already
authenticated a principal. A service behind that boundary verifies the context
and maps it onto its own authorization model instead of re-authenticating.

This package exists because that contract was previously mirrored by hand in
several independent services, and the copies drifted — most consequentially on
which schema versions each one accepted, a disagreement only discoverable at run
time, as an authorization failure.

## The claim set

The JSON tags are **frozen**. A consumer on the far side of the wire may decode
this document by key name against its own copy of the schema, so renaming a tag
compiles cleanly on both sides and then fails to authorize in production.

| Wire key  | Go field       | Meaning                                                        | Required |
|-----------|----------------|----------------------------------------------------------------|----------|
| `iss`     | `Issuer`       | Who minted the context. Selects the key set it is checked against. | yes   |
| `sub`     | `Subject`      | The principal. Used verbatim; not required to be a UUID.        | yes      |
| `iat`     | `IssuedAt`     | Issued-at.                                                      | yes      |
| `exp`     | `ExpiresAt`    | Expiry. Contexts are short-lived; the minter defaults to 15 minutes. | yes  |
| `jti`     | `ID`           | Token id. Carries `omitempty` — absent when unset.              | no       |
| `tnt`     | `Tenant`       | Tenant **slug**, not a UUID. Never parse it as one.             | yes      |
| `ent`     | `Entitlements` | Entitlements resolved at issuance. Encoded as `[]` when empty, never `null`. | no |
| `aal`     | `AAL`          | Assurance level actually achieved: `AAL1`, `AAL2`, `AAL3`.      | yes      |
| `ctx_ver` | `CtxVer`       | Schema version of this claim set.                               | yes      |
| `tok_ref` | `TokRef`       | Truncated digest of an upstream credential, for log correlation. Carries **no** `omitempty` — always on the wire, empty when unset. | no |

The `jti` / `tok_ref` asymmetry is deliberate and part of the contract: an
absent token id is absent, an unset correlation handle is present and empty.

The `typ` header is always `identity-context+jwt`. It is what keeps a context
from being accepted where a general-purpose access token is expected, and an
access token from being accepted as a context — even when the same key signed
both. Signing is `ES256` or `RS256`.

`tok_ref` is a correlation handle, never a credential. It is not validated, not
compared against a secret, and no decision is gated on it.

## Emitted version vs accepted set

These are two different things and conflating them is how the copies drifted.

- **Emitted**: `Version` is the version this package stamps on what it mints. It
  is `1`.
- **Accepted**: `VersionSet` is what a *verifier* will take. `DefaultVersionSet()`
  is `{1}` — deliberately the strictest, so a consumer never silently accepts a
  schema it was not built to read.

Widening is explicit, per verifier, and a decision:

```go
v, err := identity.NewVerifier(trusted,
    identity.WithAcceptedVersions(identity.NewVersionSet(1, 2)))
```

Mint at a version outside what deployed verifiers already accept and the tokens
are rejected in the field, turning a library change into a coordinated
multi-service deploy. Widen the accepting side first, everywhere, then raise what
is emitted.

## Trusted issuers are configuration

Nothing about which issuer is trusted is compiled in. A verifier takes a **set**
of trusted issuers, each paired with its own key source:

```go
v, err := identity.NewVerifier([]identity.TrustedIssuer{
    {Issuer: "https://first.example/",  Keys: firstKeys},
    {Issuer: "https://second.example/", Keys: secondKeys},
})
```

The set is plural from the outset because the common case is a service that
verifies its own in-process issuer now and an external provider later. A
verifier built around a single pinned issuer has to be rewritten for that, and
the rewrite is where independently-maintained copies come apart.

An issuer outside the set is refused before any key is resolved or any signature
checked. The `iss` read for that lookup is unverified at the time it is read, so
it selects a key set and does nothing else — it never becomes an identity, a
tenant, or an authorization input. A forged `iss` can therefore only select a key
set that will refuse the signature.

## One rejection, on purpose

`Verify` returns `ErrInvalidContext` for every failure — that exact value, never
wrapped, never a different one. Bad signature, unknown issuer, unknown key id,
expired, wrong algorithm, wrong type, unaccepted version and missing claims are
all indistinguishable to a caller.

This is deliberate. A per-reason error becomes a per-reason HTTP response, and
that is an oracle: it tells a prober whether it has guessed a trusted issuer, or
whether it holds a structurally valid but expired token.

Operators still need the real reason, so it goes to a sink instead of to the
caller:

```go
v, err := identity.NewVerifier(trusted,
    identity.WithRejectionLogger(func(cause error) { slog.Warn("context rejected", "cause", cause) }))
```

Do not surface that cause to the party that presented the token.

## Key resolution

A verifier resolves signing keys through a `KeySource`, one per trusted issuer.
Two implementations ship:

- `NewStaticKeys` — an in-memory map, for an in-process issuer or a pinned key.
- `NewRemoteJWKS` — fetches an issuer's published key set over HTTP and caches it.

A consumer that already operates a key cache can implement `KeySource` over it
rather than running a second one.

### Two refetch triggers, and why both

`RemoteJWKS` refetches on an **unknown key id** *and* after a **cache TTL**.

The first alone looks sufficient: rotation introduces a new key id, the cache
misses, it refetches. But an issuer that publishes a *fixed* key id and
regenerates the key behind it — which is what a restart with an ephemeral key
does — never changes the id, so the cache never misses, and the verifier keeps
checking signatures against a public key whose private half is gone. Every
request is refused, indefinitely, with no miss to trigger recovery. The TTL is
the only thing that heals that.

An unknown key id is attacker-controllable, so unknown-kid refetches are floored
by a minimum interval — otherwise a stream of invented key ids turns the verifier
into a request amplifier aimed at the issuer. Inside that interval an unknown id
is refused from the cached set rather than triggering a fetch. The floor is held
at or below the TTL so it can never delay the refresh above.

A failed fetch never replaces a good cached set: a blip degrades rather than
becoming an outage.

### When a published key is skipped

An entry in a fetched key set is **silently skipped** when it:

- has no `kid` — nothing can select it;
- has `use` present and not `sig` — an encryption key is not a signing key;
- repeats a `kid` already seen — **first entry wins**, so an appended entry
  cannot displace a real key;
- has an unsupported key type or curve — only `EC` / `P-256` and `RSA` are read;
- is `RSA` with a modulus under 2048 bits.

A key set with *no* usable entry is an error. A **partial** skip is not: it
surfaces later, and opaquely, as an unknown-key-id rejection. If an issuer's
key verifies nowhere and its key set looks healthy, check it against this list
first.

## Adopting without a wire-format change

A service already verifying these contexts against its own hand-maintained copy
can adopt this package without changing anything on the wire. The claim shape,
the `typ` header and the signing algorithms are identical, so tokens minted
before and after are byte-compatible and the same tokens verify either way.

The one thing worth checking during adoption is the accepted version set:
`DefaultVersionSet()` is `{1}`, so a consumer that had been accepting more than
that must say so explicitly with `WithAcceptedVersions`.

## Acting as an issuer

An application that authenticates its own users can present that result as a
context, so a boundary or a peer service consumes it exactly as it would one
from a dedicated identity provider.

You implement two seams. Nothing else is required — in particular **no storage
interface**. The credential primitives you will build on
(`platform/auth.PasswordHasher.Verify`, `platform/auth/totp.Validate`,
`totp.VerifyBackupCode`) are pure functions, so the seams stay free of any
opinion about where credentials live or how tenancy is modelled.

```go
// Verify a credential. The credential type is yours.
type CredentialVerifier[C any] interface {
    VerifyCredential(ctx context.Context, credential C) (*Authentication, error)
}

// Resolve what an authenticated subject is entitled to.
type ClaimsResolver interface {
    ResolveClaims(ctx context.Context, subject string) (Grant, error)
}
```

Report the factors you actually exercised — that is what the assurance claim is
derived from:

```go
&identity.Authentication{
    Subject: "principal-0042",
    Factors: []identity.Factor{
        {Kind: identity.FactorKnowledge,  Method: "password"},
        {Kind: identity.FactorPossession, Method: "totp"},
    },
}
```

Then wire an issuer and publish its key:

```go
issuer, err := identity.NewIssuer[MyCredential](ctx, signer, "https://app.example/", verifier, resolver)
if err != nil {
    return err // includes a failed signing-key health check
}

keys, err := identity.NewKeySetHandler(signer)
if err != nil {
    return err
}
mux.Handle("/.well-known/jwks.json", keys)

token, err := issuer.Issue(ctx, credential)
```

`Issue` verifies the credential, validates the result, derives assurance,
resolves the grant, and mints — in that order, stopping at the first failure.
The subject in the context is the one that was **proved**, never the one the
credential claimed. The resolver is never consulted for an identifier that has
not authenticated, because resolving entitlements for an unauthenticated
identifier is a disclosure even when nothing is minted afterwards.

Unlike `Verify`, `Issue` returns specific errors. The trust boundary is
different: `Verify`'s caller may be relaying to whoever presented the token, so a
reason would be an oracle, whereas `Issue`'s caller is the application that owns
the login flow and has to decide what to show its own user.

### The assurance ladder

Assurance is derived from what happened, never supplied by a caller.

| Authentication | Level |
|---|---|
| No factors | error — nothing was established, and a level would read as "authenticated weakly" |
| One distinct **category**, however many methods | AAL1 |
| Two or more distinct categories | AAL2 |
| Two or more categories **and** both `HardwareBacked` and `PhishingResistant` asserted | AAL3 |

It counts independent **categories**, not prompts. A password and a security
question are two prompts and one category: whatever compromises the first tends
to compromise the second, so the pair stays AAL1. A one-time code and a recovery
code are both **possession** — the principal holds the seed or the code sheet —
not knowledge.

The top rung additionally needs a hardware-resident authenticator and resistance
to verifier impersonation. Neither is visible in a factor's name — a method of
`"webauthn"` may or may not be hardware-backed — so both are read from what your
verifier asserted, never guessed. Asserting them without two categories does not
reach AAL3: they qualify a multi-factor authentication, they do not substitute
for one.

Everything unknown or ambiguous resolves **downward**. Understating assurance
costs a user an extra prompt; overstating it silently grants access a policy
meant to withhold, and leaves an audit record asserting a control that was never
applied.

### Why the key id is not a parameter

`NewIssuer` takes a signing key from the platform key surface and reads the key
id off it. There is no key-id parameter, so a caller cannot pin a constant one.

That is deliberate, and it is the signature rather than documentation doing the
work. A deployment that pins a constant key id and then replaces the key
material behind it breaks every consumer permanently: a key cache only re-fetches
on an id it does not recognise, so an id that never changes never misses, and
every request is refused with nothing to trigger recovery. An id derived from the
key changes when the key does, producing exactly the miss that heals.

An empty key id is a construction error — it would mint contexts no verifier can
resolve a key for.

### The start-up signing proof

`NewIssuer` runs the signing key's health check and refuses to construct if it
fails. That check performs a real sign-and-verify round trip.

It exists because reading a public key and signing with it are separately
authorized operations on every hosted key service, and a configuration that
permits the first while denying the second looks perfectly healthy right up to
the first login attempt. Failing at construction turns that into a start-up
error an operator sees immediately, rather than an authentication outage a user
discovers.
