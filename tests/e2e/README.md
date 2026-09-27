# End-to-end suites

The built `arkd` image on a real network: validators and their price-feed sidecars in Docker under
[interchaintest](https://github.com/cosmos/interchaintest), driven through the CLI, REST, and RPC, with Hermes relaying
between two chains. This is its own Go module so the root `go test ./...` never starts containers; `make test-e2e-vet`
keeps it compiling.

## Run

Build the image the suites run, then a package at a time:

```sh
make localnet-build-env
make test-e2e E2E_PACKAGES=./delegator/...
```

`make test-e2e` runs every package. Each suite spawns its own chain, so a package takes minutes and the whole tree
most of an hour. The Makefile exports the Docker CLI context's endpoint as `DOCKER_HOST`, which the Go client needs on
OrbStack and Colima.

Every chain runs the testnet artefact, [app/genesis/testnet.json](../../app/genesis/testnet.json): `chainsuite.ArtefactGenesis`
lays its consensus block and module states over the genesis interchaintest generates, keeping interchaintest's
accounts and gentxs and merging the bank ledger, then applies the suite's short windows. Validators are ordinary bonded
accounts on the launch economics, not seats; the gov suite admits a seat through the governance proposal.

| Variable | Default | Meaning |
| --- | --- | --- |
| `TEST_IMAGE_NAME` | `ark/arkd` | Image repository; `TEST_DOCKER_REGISTRY` prefixes it. |
| `TEST_IMAGE_VERSION` | `latest` | Tag under test; the local build's. |
| `TEST_OLD_IMAGE_VERSION` | | Tag a suite with `UpgradeOnSetup` starts on before upgrading to the tag under test. |
| `TEST_UPGRADE_NAME` | | Plan the new binary registers. Empty with an old version means a coordinated binary swap. |
| `TEST_PRICEFEED` | `true` | One price-feed sidecar per validator. Off, the chain holds no exchange rate and the oracle suite skips. |

The sidecars reach their default providers over the internet; a rate can take a minute or two to appear on a cold
start, and the suites that need one wait for it.

Transactions carry no `--gas-prices`: arkd prices each one from Treasury's live sheet and the payer's balances, tax
included, which is the path an operator's CLI takes. Hermes prices its own transactions and gets
`chainsuite.RelayerGasPrices`, checked against the live sheet at setup.

## Packages

| Package | Chain | Covers |
| --- | --- | --- |
| `delegator` | one or four validators | Bank, encode/decode, multisig, fee grants, authorisations, vesting, delegation and unbonding, redelegation, governance through deposit and tally, a cancelled software upgrade, an expedited proposal falling back, community-pool spend, a native Disbursement member tranche, member registration and committee recovery, downtime jail and unjail, a CosmWasm contract, and the oracle: every validator's rates in the extended commit, a power shift keeping them there, and a conversion settling. |
| `validator` | four validators, one chain per test | Node configuration: no indexer, both Prometheus endpoints, pruning everything, node `minimum-gas-prices` ignored, peer limits, websocket limits, API off; a double-sign and its tombstone; a validator withdrawing its whole self-bond. |
| `integrator` | one or four validators | The REST and RPC routes explorers and operators read, Ark's module routes among them; the extended commit in every block; a continuation export relaunched under a new chain ID with the same validator keys, its balances, and its oracle. |
| `ibc` | two chains and Hermes | The governance act that opens the launch genesis's shut hub, a transfer round trip, an interchain account sending on the host through its execution tax, which leaves NOAH untaxed, and a wasm light client stored through governance. |

## Writing a suite

Embed `chainsuite.Suite` (or `delegator.Suite` for funded wallets) and pass `chainsuite.NewSuite` a `SuiteConfig`:
the validator count, `GenesisOverrides` on top of `DefaultGenesis`, `Scope` when tests must not share a chain, and
`UpgradeOnSetup` for suites that should run against an upgraded chain when `TEST_OLD_IMAGE_VERSION` is set.
`chainsuite.Chain` wraps interchaintest's chain with what the suites reach for: proposals, config changes with a
restart, the oracle's rates and vote extensions, Treasury's gas price, and REST fetches. Transactions carry no gas price,
so arkd prices them itself; only Hermes declares `chainsuite.RelayerGasPrices`, which `VerifyGasPrices` checks against the
live sheet at setup.

Keys made with `BuildWallet` live on the first node's keyring; send their transactions through `Chain.GetNode()`.
