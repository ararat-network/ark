# Network registry generator

Writes Ark's network registry: for every network, the community chain-registry files, `chain.json` and
`assetlist.json`, and `ark.json` for what that schema has no field for. The registry itself lives in
[ark-registry](https://github.com/ararat-network/ark-registry); this generator is the chain's half of it, so the
denominations, prefix, coin type, versions, and genesis identity a client reads can never drift from the chain. The
[tooling direction](../../docs/direction/TOOLING_DIRECTION.md#2-establish-a-canonical-network-registry) is why.

```sh
make registry                                  # into build/registry
go run ./contrib/registry --out ../ark-registry --endpoints ../ark-registry/endpoints.json --version v1.0.0
```

| Output | Source |
| --- | --- |
| `<network>/chain.json` | Chain ID, prefix, coin type from `app/params`; NOAH as fee and staking token with the treasury's minimum base gas price; SDK, CometBFT, and wasmvm versions from `go.mod`; the genesis URL at `--version`; endpoints and explorers from `--endpoints` |
| `<network>/assetlist.json` | NOAH and every registry asset the network's genesis artefact lists as active, each from `pkg/chain`'s Bank metadata |
| `<network>/ark.json` | The genesis file's SHA-256, feature flags, the reference denomination and feeds, the accepted fee denominations, the fee contract, logos, and indexer endpoints |
| `images/` | The brand kit's symbol, served by the registry |

Networks are fixed in [main.go](main.go): `ark` on `ark-1` from the launch artefact, `arktestnet` on `ark-testnet-1`
from the testnet artefact, and `arklocal` on `localark`, which runs the testnet artefact under its own chain ID, carries
the localnet's ports, and has no genesis hash since its genesis is made on every init. Mainnet's asset list holds only
what its artefact registers, NOAH alone until a vote registers the stablecoins.

Two things the standard files leave out on purpose. Logos: the schema admits only the community registry's own URLs,
so they live in `ark.json`. Stablecoin fee prices: the chain prices gas in a stablecoin from consensus state through
the [fee contract](../../docs/clients/CLIENT_FEES.md), so no fixed number is true; `ark.json` lists the accepted
denominations and points at the contract.

`--endpoints` takes a JSON object keyed by network name, each with `rpc`, `rest`, `grpc`, optional `wss`, `explorers`,
and `indexer` arrays in the chain-registry shape. The registry keeps that file by hand; a name that is not a network is
refused. The generator reads artefacts and `go.mod`; a `--node` mode that reads a running chain's registry is the
natural next step once governance registers assets on mainnet.
