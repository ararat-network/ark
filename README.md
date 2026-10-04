<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/svg/ark-logo-horizontal-flush-white.svg">
    <img src="docs/brand/svg/ark-logo-horizontal-flush-navy.svg" alt="Ark" width="280">
  </picture>
</h1>

[![Test](https://github.com/ararat-network/ark/actions/workflows/test.yml/badge.svg)](https://github.com/ararat-network/ark/actions/workflows/test.yml)
[![Lint](https://github.com/ararat-network/ark/actions/workflows/lint.yml/badge.svg)](https://github.com/ararat-network/ark/actions/workflows/lint.yml)
[![E2E](https://github.com/ararat-network/ark/actions/workflows/e2e.yml/badge.svg)](https://github.com/ararat-network/ark/actions/workflows/e2e.yml)
[![Sims](https://github.com/ararat-network/ark/actions/workflows/sims.yml/badge.svg)](https://github.com/ararat-network/ark/actions/workflows/sims.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ararat-network/ark)](https://goreportcard.com/report/github.com/ararat-network/ark)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue?logo=go)](https://pkg.go.dev/github.com/ararat-network/ark)

Ark is a sovereign Cosmos SDK blockchain that converts its native asset, NOAH, into stablecoins and back at oracle
prices, with a governed capital structure standing behind the stablecoin liability.

## Why should you be interested in Ark

Ark is built using the [Cosmos SDK](https://github.com/cosmos/cosmos-sdk) and compiled to a binary called `arkd` (Ark
Daemon). Validators also run `pricefeed`, a separate off-chain sidecar that supplies the prices their nodes report in
vote extensions. Ark interacts with other sovereign chains through [IBC](https://github.com/cosmos/ibc) and runs
[CosmWasm](https://github.com/CosmWasm/wasmd) contracts.

What sets Ark apart is its economic model. There is no routine NOAH issuance: no mint module, no staking inflation, and
no seigniorage. NOAH is minted and burned only inside stablecoin conversion, and three funds stand behind the
stablecoins: a Redemption Buffer that recycles retained NOAH into redemptions, a strategic Reserve that governance can
commit and a committee can deploy under a bounded mandate, and Insurance that pays covered losses through recorded
claims. Validators and the oracle are funded from a fixed transfer tax and a genesis-seeded subsidy pool. Governance
holds every unbounded power; committees hold bounded, height-scoped mandates. To understand how funds and authority
move, read the [economic design](docs/design/ECONOMIC_DESIGN.md).

The [manifesto](docs/papers/MANIFESTO.pdf), _The Revolution Begins Within_, sets out the purpose that design serves.

## Documentation

Documentation lives in this repository. Start at the [documentation index](docs/README.md), which arranges the guides by
reading path: understanding the chain, launching a network, operating and monitoring services, governing and responding,
integrating clients, future direction, and the decision record. Each subsystem's README owns its own behaviour, design,
and development guidance; the [repository map](#repository-map) links them.

### Additional resources

**For node operators:** [Node operations](docs/operations/NODE_OPERATIONS.md) covers building `arkd`, configuring a
home, joining an existing network, and upgrading. [Process monitoring](docs/operations/PROCESS_MONITORING.md) covers
scraping the node and sidecar, and [protocol monitoring](docs/operations/PROTOCOL_MONITORING.md) covers chain-state
signals and alert calibration.

**For validators:** Each validator reports prices through vote extensions, so it runs a `pricefeed` sidecar beside its
node. [Pricefeed operations](docs/operations/PRICEFEED_OPERATIONS.md) covers the two-way node/sidecar setup, TLS,
reload, and failover; the [oracle module](x/oracle/README.md) explains attendance and reward settlement. Every validator
also follows the [upgrade binary policy](docs/operations/NODE_OPERATIONS.md#upgrade-binary-policy): cosmovisor never
downloads binaries.

**For delegators:** Ark pays no staking inflation. Validator rewards come from the transfer tax and the subsidy pool, so
read [validator and Oracle funding](docs/design/ECONOMIC_DESIGN.md#9-validator-and-oracle-funding) before delegating.

**For governance participants and committees:** [Governance operations](docs/governance/GOVERNANCE_OPERATIONS.md) covers
proposals, service permissions, and appointments. The
[economic committee runbook](docs/governance/ECONOMIC_COMMITTEE_RUNBOOK.md) and
[emergency submission runbook](docs/governance/EMERGENCY_SUBMISSION_RUNBOOK.md) cover bounded policy changes and
emergency transactions.

**For client integrators:** [Client fee construction](docs/clients/CLIENT_FEES.md) explains signed fee declarations,
simulation, and failure handling for external transaction builders.

## Repository map

| Directory                                                                | Entry point                                                                          |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| [app](app/README.md)                                                     | Application wiring, transaction hooks, mempool, IBC/Wasm, and upgrades.              |
| [abci](abci/README.md)                                                   | Oracle vote extensions, proposal envelope, preblock, and aggregation implementation. |
| [pricefeed](pricefeed/README.md)                                         | Node-side cached client and off-chain sidecar/provider pipeline.                     |
| [cmd/arkd](cmd/arkd/README.md), [cmd/pricefeed](cmd/pricefeed/README.md) | Binary startup and CLI ownership.                                                    |
| [pkg](pkg/README.md)                                                     | Shared arithmetic, encoding, transport, mandates, and telemetry.                     |
| [proto](proto/README.md)                                                 | Handwritten schemas and dual generation. `api/` contains generated runtime API code. |
| [tests](tests/README.md)                                                 | Test map, integration fixtures, simulations, and CI.                                 |
| [contrib](contrib/README.md)                                             | Images, localnet, probes, and operational rehearsals.                                |

On-chain modules: [Asset](x/asset/README.md), [Claims](x/claims/README.md), [Disbursement](x/disbursement/README.md),
[Market](x/market/README.md), [Oracle](x/oracle/README.md), [Reserve](x/reserve/README.md), [Security](x/security/README.md), and
[Treasury](x/treasury/README.md).

## Build and run locally

[go.mod](go.mod) records the SDK version; [mise.toml](mise.toml) pins the development tools. From the repository root,
with the pinned Go toolchain and the native dependencies required by Wasm available:

```sh
make build
make build-pricefeed
./build/arkd --help
./build/pricefeed --help
```

## Testnet

Until a public network launches, run one locally. The Docker localnet wires validators and their sidecars together;
install Docker with Compose and run:

```sh
make localnet-start
make localnet-liveness
make localnet-stop
```

`localnet-start` rebuilds and recreates the localnet homes, replacing the previous localnet state. Read the
[localnet guide](contrib/localnet/README.md) for topology, data paths, ports, state sync, and rehearsals. For a single
validator on the host without Docker, or a testnet assembled by several developers, follow the
[disposable host testnet](docs/operations/NODE_OPERATIONS.md#disposable-host-testnet) and
[manual assembly](docs/operations/NODE_OPERATIONS.md#manually-assembling-a-testnet) sections of node operations. A
shared testnet starts from the curated artefact [app/genesis/testnet.json](app/genesis/testnet.json), the launch genesis
with its windows compressed ([genesis §15](docs/governance/GENESIS.md#15-testnet-artefact)).

## Genesis and relaunch

Ark launches from a clean genesis as `ark-1`. The curated launch artefact is
[app/genesis/genesis.json](app/genesis/genesis.json); [Genesis](docs/governance/GENESIS.md) records each setting, its
status, and the decision behind it. Do not treat `arkd init` defaults as the launch policy. A relaunch takes a new chain
ID and starts from `arkd export`, which is a continuation export: heights stay absolute and the exported genesis starts
at the next height, so every height-anchored record resumes as it is. Zero-height export is refused on purpose. See
[upgrades and relaunch](docs/operations/NODE_OPERATIONS.md#upgrades-and-relaunch).

## Contact

For questions, feedback, or collaboration:

- Twitter / X: `@Chitpole0`
- Email: `chitpole@proton.me`

For security vulnerabilities, follow the [security policy](SECURITY.md) to report privately.

## Contributing

[AGENTS.md](AGENTS.md) holds the repository guidelines: Cosmos SDK conventions, arithmetic rules, proto generation,
collections patterns, and testing conventions. Start verification with the affected package tests;
[tests/README.md](tests/README.md) maps each kind of change to its checks, and [contrib](contrib/README.md) holds the
images, localnet, probes, and rehearsals. Documentation follows the
[placement rule](docs/README.md#documentation-placement-rule): one authoritative home per subject, linked from
everywhere else.

## Release builds

Ark-authored source is licensed under [Apache 2.0](LICENSE). The node also links
third-party code under LGPLv3 and the Business Source License 1.1, whose
production-use terms operators should read.
[Licensing and corresponding source](THIRD_PARTY_NOTICES.md) explains component
licences, attribution, source downloads, and the release verification procedure.

The chain releases from `vX.Y.Z`; the sidecar releases independently from `pricefeed/vX.Y.Z`.
[The release workflow](.github/workflows/release.yml) prepares signed drafts from reviewed tags.
A suffix such as `-rc1` marks the draft as a pre-release. Maintainers review and publish through GitHub;
only stable chain releases should be marked latest.
[Release verification](docs/operations/RELEASE_VERIFICATION.md) covers dispatch, review and consumer checks.

`make release` packages locally without uploading or signing. It checks a clean tree and the chain tag on HEAD, selects it explicitly, and runs the
[chain configuration](.goreleaser.yml), which builds static Linux binaries for amd64 and arm64. GoReleaser passes its
build command to [the release adapter](contrib/scripts/release-build.py), which builds the committed source export with
each architecture's compiler in the pinned base from the [image Dockerfile](contrib/images/arkd-env/Dockerfile).
Docker must support running both Linux architectures, either natively or through emulation. The builder checks the
wasmvm version and archive digests, records the linked system libraries and packages their corresponding runtime sources.
The adapter verifies the binary, target, source revision and runtime-source pairing before returning it to GoReleaser.
macOS node use remains a source build.

`make release-pricefeed` also packages locally without uploading or signing. It checks a clean tree and the sidecar tag on HEAD, selects it explicitly, then skips GoReleaser's
bare-semver validation. The [sidecar configuration](.goreleaser.pricefeed.yml) derives version strings from `.Tag` and
cross-compiles without cgo. Both release configurations prepare a corresponding
source bundle, verify builds from it, and generate per-archive SPDX inventories with pinned Syft.
Checksums cover the archives, SPDX files and every attached source/notice asset. This requires Python
3.12+, Docker with both Linux architectures, and the source-packaging prerequisites
listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Published images carry
their source bundle and notices inside `/usr/share/ark/`, including nightly builds.
Standalone node releases attach both `arkd-source-<commit>` application sources and
`arkd-runtime-source-<commit>-linux-<arch>` runtime sources. Retain both with the matching binaries.
Neither release command is part of ordinary verification.
