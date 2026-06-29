# Experience Contract Versioning (experience.v1)

TRD 140-12 — the cross-repo versioning gate for the frozen `experience.v1`
keystone contract.

## Why this exists

The `experience.v1` contract fans out across **four repos**:

```
eden-platform-go (proto — THIS repo, the single source of truth)
  ├─ eden-experience-api-dart        generated Dart consumer
  │    └─ eden-platform-flutter       Flutter consumer
  │         └─ eden-biz (replace-pin) product consumer
  └─ aocore                           REST / ORG consumer
```

The fan-out **desynced once already** (the three Flutter clients drifted off the
proto). A `v1` proto is **frozen forever**: an offline-cached spec on a device,
or a store-submitted binary, cannot migrate. So M0 ships a bump/compat policy
that keeps the frozen contract frozen **safely** and catches a desync at CI time
instead of in a shipped binary.

## The two enforcement halves

| Half | Where | What it catches |
|------|-------|-----------------|
| **Wire** | `.github/workflows/experience-proto-breaking.yml` (`buf breaking`) | A removed / retyped / renumbered frozen field in the proto. |
| **In-process** | `platform/experience/contract_version.go` (`ClassifyChange` / `BumpVersion` / `ContractVersion`) + per-consumer compat tests | The same judgment, reasoned about in Go; plus the drift guard + forward-compat decode proof on each consumer. |

Both halves make the **same** judgment. The wire gate is the hard CI block; the
in-process layer lets Go code (and tests) classify a proposed change without
shelling out to buf, and proves each consumer actually decodes a newer spec
forward-compatibly.

## The semver bump policy

A proto change to `experience.v1` maps to a version action:

| Proto change | Class | Action |
|--------------|-------|--------|
| New field (in a reserved range), new message, new rpc | **additive** | `MINOR` bump — compatible, allowed. |
| Field removal | **breaking** | **BLOCKED.** Forbidden on a frozen v1. |
| Field type change | **breaking** | **BLOCKED.** |
| Field renumber | **breaking** | **BLOCKED.** |
| Reserved-range removal | **breaking** | **BLOCKED** (buf flags it; never un-reserve-then-delete). |

**A breaking change is never an automatic major bump.** A frozen v1 contract
never silently majors. A genuine breaking change must become a **new contract**
(`experience.v2`) by a deliberate human decision — `BumpVersion(_, ChangeBreaking)`
returns an error, and the buf gate hard-fails the PR. This is the whole point:
the gate forces the breaking change to be a conscious v2 fork, not a quiet
in-place mutation that strands cached `v1` specs.

The fine-grained versioning lives in the **three orthogonal axes** on the spec
(`spec_schema_version`, `surface_contract_version`, `min_binary_version`) — see
`version_negotiation.go`. `ContractVersion` ("experience.v1") is the contract
NAME, the single token every spec/AppDefinition carries and the drift guard
couples the proto to.

## The buf breaking gate

The workflow runs `buf breaking` scoped to `proto/experience/v1`, pinned to
**buf 1.69.0**:

```yaml
- uses: bufbuild/buf-setup-action@v1
  with:
    version: "1.69.0"   # PINNED — see WKT-drift note below
- run: buf breaking --against '.git#ref=origin/main,subdir=.' --path proto/experience/v1
```

### Baseline ref

The baseline is the committed proto on the **default branch (`origin/main`)**.
While 140-01..12 is still on the `feat/experience-v1-contracts` integration
branch (pre-merge), the proto is not yet on `main`; the first PR after that
branch merges compares against a baseline that has no experience proto, which is
a pure addition (PASS) — correct. From then on every PR is gated against the
frozen `main` proto.

To run the gate **locally** against the current frozen baseline (the
`feat/experience-v1-contracts` tip, which is the proto being frozen):

```sh
buf breaking --against '.git#branch=feat/experience-v1-contracts' --path proto/experience/v1
```

### buf 1.69.0 is pinned (load-bearing, not cosmetic)

An **unpinned** buf reflows the `google/protobuf` well-known-type doc-comments
that ship with the CLI binary, producing **FALSE** breaking/drift diffs even when
nothing in our proto changed. The pin (`version: "1.69.0"`) is the fix. If the
only diff buf reports is in `google/protobuf/*`, it is a CLI-version mismatch —
re-check the pin before assuming a real desync.
(Memory: `feedback_buf_cli_version_wkt_drift`.)

### Proven gate behavior

Captured during 140-12 execution against the frozen baseline:

| Scenario | Simulated change | `buf breaking` exit |
|----------|------------------|---------------------|
| Breaking | renumber `AppDefinition.contract_version` 5 → 7 | **100 (FAIL)** |
| Breaking | remove a field from a reserved range | **100 (FAIL)** |
| Additive | add a new top-level message | **0 (PASS)** |
| No change | identical proto | **0 (PASS)** |

## Per-consumer forward-compat tests

Each consumer ships a test proving the contract's **core promise** — an OLD
binary reading a NEWER spec ignores an unknown surface, never crashes:

| Consumer | Test | Asserts |
|----------|------|---------|
| **Go** (eden-biz-shaped) | `platform/experience/contract_version_test.go` | decodes a spec with an unknown surface under IGNORE → drops it, renders the rest; drift guard couples the decoded `contract_version` to `ContractVersion`; wrong-tenant scope carried through untouched (no widen). |
| **Dart** (api-dart) | `eden-experience-api-dart/test/contract_compat_test.dart` | same forward-compat (`negotiateSurfaces`), `UNSPECIFIED` policy fails safe (block), drift guard couples `kExperienceContractVersion`, wrong-tenant no-widen. |

The Dart `negotiateSurfaces` + `kExperienceContractVersion`
(`lib/src/contract_compat.dart`) mirror the Go `Negotiate` + `ContractVersion`;
the two version tokens MUST agree, and the drift guards on both sides enforce it.

**aocore** is a REST/ORG consumer; its forward-compat surface is the same
JSON/proto-decode-ignores-unknown-fields property (protobuf JSON + binary both
preserve unknown fields by default). M0 ships the Go + Dart consumer tests; the
aocore consumer test is tracked for its own stream and is a no-crash decode of
the same frozen spec — an unknown surface is ignored, never a 500.

## The 4-repo fan-out order

When the contract changes (additively — a breaking change is a v2 fork, see
above), regenerate and pin in this order so no consumer reads a spec shape its
generated types don't know:

1. **eden-platform-go** — edit the proto (additive only), regenerate the Go
   package, bump `MINOR` per the policy.
2. **eden-experience-api-dart** — regenerate the Dart package from the same
   proto; the drift guard test must stay green.
3. **eden-platform-flutter** — pull the new api-dart.
4. **eden-biz** — advance the `replace`-pin to the new platform-flutter /
   api-dart.
5. **aocore** — regenerate / re-pin the REST consumer.

The buf breaking gate sits at step 1 and blocks an incompatible change before it
can fan out at all.
