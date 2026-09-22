# Contracts

CosmWasm contracts the chain's own processes use. Each is a crate in this Cargo workspace, built with the Rust
[mise.toml](../mise.toml) pins for the `wasm32-unknown-unknown` target, and validated against the capabilities
`app/wasm.go` grants a contract.

| Contract | Purpose |
| --- | --- |
| [grant](grant/README.md) | Member grants and the contributor escrow of the [distribution plan](../docs/governance/DISTRIBUTION_PLAN.md). |

## Build and test

From the repository root:

```sh
make contracts
```

runs `cargo fmt --check`, `cargo clippy`, `cargo test`, builds the wasm, and validates it with `cosmwasm-check`.

```sh
make contracts-optimize
```

is the reproducible build a store proposal cites: the `cosmwasm/optimizer` image, which pins the same Rust,
writes `contracts/artifacts/`, prints the checksum, and copies the artefact to
[app/testdata](../app/testdata/contracts.go), where the application tests drive it against the real app. That copy
is checked in and CI rebuilds it with the same image, failing on a difference, so the tests always run the bytes
the chain would store. It needs Docker; on an arm64 host the amd64 image runs emulated and still reproduces.

The Rust pin is not the newest stable on purpose: from 1.87 the wasm target emits bulk-memory instructions the
chain's wasmvm rejects. Move it with the optimizer image, not ahead of it.
