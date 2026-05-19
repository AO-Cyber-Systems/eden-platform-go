# UPSTREAM_PR — `internal/forked/goxmldsig`

**PR-Status: not-yet-submitted**
**Target:** github.com/russellhaering/goxmldsig (any version >= v1.4.0)
**Tracking issue:** none filed; opened internally only.

## What this fork contains

A single file (`signing_context_signer.go`) plus its test
(`signing_context_signer_test.go`). It exposes:

- `MemoryX509KeyStoreSigner` — a keystore type that holds a
  `crypto.Signer` (NOT `*rsa.PrivateKey`) alongside its cert chain. Its
  `GetKeyPair()` returns the `crypto.Signer` directly so callers using
  HSM/KMS-resident keys do not have to fake an `*rsa.PrivateKey`.
- `NewSigningContextSigner(signer, certChain)` — a defensive wrapper
  around upstream `dsig.NewSigningContext` that rejects nil signers and
  empty cert chains before they cause first-sign-time failures.

The fork does **not** modify upstream behaviour; it sits on top of
`github.com/russellhaering/goxmldsig v1.4.0` and forwards directly to
`dsig.NewSigningContext` for the signing path.

## Why we still vendor it as a "fork"

1. **Single keystore type.** Upstream's `MemoryX509KeyStore` is hard-pinned
   to `*rsa.PrivateKey` (its `GetKeyPair()` signature is
   `(*rsa.PrivateKey, []byte, error)`). KMS-backed signers can only
   implement `crypto.Signer`. AOID's `platform/saml` needs a *single*
   keystore type that round-trips a `crypto.Signer`. Having that type live
   in a forked-package lets us delete it in one commit when upstream lands
   a `MemoryX509KeyStoreSigner` equivalent.
2. **Defensive constructor.** Upstream's `NewSigningContext` accepts nil
   cert chains (the SAML KeyInfo block then comes out empty, which most
   IdPs accept but no SP will). Our wrapper rejects this at boot.
3. **TRD truth alignment.** Eden TRD 06-01 explicitly requires the type
   name `MemoryX509KeyStoreSigner` and the factory name
   `NewSigningContextSigner` — both live here so consumers see one
   import path.

## When to remove

When upstream `github.com/russellhaering/goxmldsig` ships a
`MemoryX509KeyStoreSigner` (or equivalent) whose `GetKeyPair()` returns a
`crypto.Signer`:

1. Replace every `internal/forked/goxmldsig` import in `platform/saml/*.go`
   with the upstream package.
2. Delete the `internal/forked/goxmldsig` directory entirely.
3. Delete this file.

## Upstream PR draft (paste into the issue/PR body)

> **Title:** Add `MemoryX509KeyStoreSigner` mirroring `MemoryX509KeyStore`
> but holding a `crypto.Signer` instead of `*rsa.PrivateKey`
>
> **Motivation:** `dsig.NewSigningContext(crypto.Signer, certs)` already
> exists and handles HSM/KMS-resident keys correctly. The
> `MemoryX509KeyStore` keystore type, however, is hard-pinned to
> `*rsa.PrivateKey`, which makes it impossible to use the
> KeyStore-based path with cloud KMS keys. This PR adds a parallel
> `MemoryX509KeyStoreSigner` type to close that gap.
>
> **Risk:** none — purely additive.

Author: AOCyber Systems (Eden Platform)
Date initially vendored: 2026-05-19
