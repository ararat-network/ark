# Ark

Ark is a Cosmos SDK blockchain with an asset registry, oracle price voting, market conversion, treasury policy,
Insurance claims, strategic Reserve, and security committee. The `arkd` node and the off-chain `pricefeed` sidecar are
separate binaries. [go.mod](go.mod) records the SDK version; [mise.toml](mise.toml) pins the development tools.

## Build and run locally

From the repository root, with the pinned Go toolchain and the native dependencies required by Wasm available:

```sh
make build
make build-pricefeed
./build/arkd --help
./build/pricefeed --help
```

For a disposable Docker network with validators and sidecars, install Docker with Compose and run:

```sh
make localnet-start
make localnet-liveness
make localnet-stop
```

`localnet-start` rebuilds and recreates the localnet homes, replacing the previous localnet state. Read the
[localnet guide](contrib/localnet/README.md) for topology, data paths, ports, and rehearsals. For host-based setup,
configuration, joining a network, and upgrades, use [node operations](docs/NODE_OPERATIONS.md) and
[pricefeed operations](docs/PRICEFEED_OPERATIONS.md).

## Repository map

| Directory | Entry point |
| --- | --- |
| [app](app/README.md) | Application wiring, transaction hooks, mempool, IBC/Wasm, and upgrades. |
| [abci](abci/README.md) | Oracle vote extensions, proposal envelope, preblock, and aggregation implementation. |
| [pricefeed](pricefeed/README.md) | Node-side cached client and off-chain sidecar/provider pipeline. |
| [cmd/arkd](cmd/arkd/README.md), [cmd/pricefeed](cmd/pricefeed/README.md) | Binary startup and CLI ownership. |
| [pkg](pkg/README.md) | Shared arithmetic, encoding, transport, mandates, and telemetry. |
| [proto](proto/README.md) | Handwritten schemas and dual generation. `api/` contains generated runtime API code. |
| [tests](tests/README.md) | Test map, integration fixtures, simulations, and CI. |
| [contrib](contrib/README.md) | Images, localnet, probes, and operational rehearsals. |

On-chain modules: [Asset](x/asset/README.md), [Claims](x/claims/README.md), [Market](x/market/README.md),
[Oracle](x/oracle/README.md), [Reserve](x/reserve/README.md), [Security](x/security/README.md), and
[Treasury](x/treasury/README.md).

## Documentation and contributing

The [documentation index](docs/README.md) links protocol design, integration guides, operations, and future work.
Follow its [placement rule](docs/README.md#documentation-placement-rule) and the [repository guidelines](AGENTS.md).
Start verification with the affected package tests; [tests/README.md](tests/README.md) explains broader checks.
