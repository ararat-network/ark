# Node operations

This guide covers building, configuring, and operating `arkd`. [Launch genesis](../governance/GENESIS.md) owns launch policy;
[pricefeed operations](PRICEFEED_OPERATIONS.md) owns the validator's sidecar; [process monitoring](PROCESS_MONITORING.md) owns monitoring.
Commands below run from the repository root unless another working directory is stated.

## Build

Use the toolchain pinned in [mise.toml](../../mise.toml), with the host's Wasm/native build prerequisites available:

```sh
make build
./build/arkd version
./build/arkd --help
```

`make build` stamps version/commit metadata. `go build -o build/arkd ./cmd/arkd` also builds the node, without those
Makefile version flags. Container builds are available through [contributor tooling](../../contrib/README.md).

## Disposable host testnet

For a local developer validator, use a fresh directory and the test keyring backend:

```sh
ARK_TESTNET_DIR=$(mktemp -d)
./build/arkd testnet init-files --validator-count 1   --output-dir "$ARK_TESTNET_DIR" --chain-id ark-local   --node-daemon-home arkd --keyring-backend test   --single-host --starting-ip-address 127.0.0.1
./build/arkd genesis validate --home "$ARK_TESTNET_DIR/node0/arkd"
COSMOS_SDK_CONFIG_SCOPE=arkd ./build/arkd start --home "$ARK_TESTNET_DIR/node0/arkd"
```

The generator creates genesis, validator keys and accounts; its test keyring is for disposable development. It does not
launch a pricefeed process. Configure and run a [sidecar](PRICEFEED_OPERATIONS.md) to produce oracle prices, or use the
[Docker localnet](../../contrib/localnet/README.md), which wires the processes together. Stop the foreground node with Ctrl-C.
Use a fresh directory for another run; do not reset an existing validator home as a setup step.

For multiple developers, generate the intended validator count and host addresses, distribute each validator's own home,
and agree on a single genesis. `--single-host` is for separate ports on one machine; omit it when using distinct hosts.
The generator's `--node-daemon-home` defaults to `simd`, so set it explicitly to `arkd` as above.

## Manually assembling a testnet

When validators create their own keys, each participant initialises a fresh home with the agreed chain ID using
`arkd init <moniker> --chain-id <chain-id> --home <home>`, then creates its own key with `arkd keys add <name> --home <home>`.
Keep each participant's mnemonic and private validator key local to that participant.

Add the agreed account balances with `arkd genesis add-genesis-account <address> <amount>anoah --home <home>`, then create
each validator's gentx with `arkd genesis gentx <name> <bond-amount>anoah --chain-id <chain-id> --home <home>`.
Amounts are base units and must cover the bond and intended transactions; use the chain's denomination, `anoah`.
The coordinator assembles the complete account allocations and all gentxs under `config/gentx`, runs
`arkd genesis collect-gentxs --home <home>`, validates the resulting genesis, and distributes the same final file to everyone.

In each `config/config.toml`, set `[p2p] persistent_peers` to the intended `<node-id>@<host>:<p2p-port>` peers. Obtain the
P2P node ID with `arkd comet show-node-id --home <home>`; it is not the validator operator address. Start only after all
participants have the same chain ID and genesis. Diagnose genesis/gentx validation failures directly rather than
starting a different genesis as a workaround.

## Joining an existing network

Initialise a fresh home with the network's chain ID, replace its generated genesis with the network's verified genesis,
and configure the network's peers and sync method. Run `arkd genesis validate --home <home>` before startup. Do not treat
`arkd init` defaults as the curated launch policy: the launch artefact is [app/genesis/genesis.json](../../app/genesis/genesis.json).
Validator admission on an existing chain uses its staking transaction process rather than a new genesis gentx.

## Configuration and process environment

The default home is `~/.ark`; `--home` selects another. `config.toml` controls CometBFT, `app.toml` the application/server,
and `client.toml` CLI defaults. Use `arkd config diff` to compare app settings with this binary's defaults and
`arkd config migrate --stdout` to inspect a migration before writing. `arkd config set app <key> <value>` changes an existing
app key; it does not create missing keys or edit arbitrary CometBFT settings. Use `--help` for each command's path options.
[Mempool policy](../app/mempool/README.md#admission-service-and-sdk-behaviour) explains the coupled CometBFT/application admission configuration.

Set this in every long-running node's service/container environment:

```text
COSMOS_SDK_CONFIG_SCOPE=arkd
```

The scope must remain non-empty and constant for the process lifetime. It pins SDK configuration lookup to a stable scope,
so address parsing does not depend on a hostname-derived lookup changing while the node runs.

## Upgrades and relaunch

[Upgrade authoring](../../app/upgrade/README.md) and the [rehearsal](../../contrib/scripts/README.md#upgrade-rehearsal)
cover implementation and validation. Follow the standing policy below before either a routine or emergency upgrade.

Independent RPC nodes can be upgraded in rotation to preserve service availability. A single validator's restart can
interrupt its signing; a coordinated consensus upgrade requires the network to reach the upgrade boundary and restart on
compatible code. Do not run the same validator signing key concurrently in old and replacement processes.

`arkd export` is a continuation export: absolute heights and height-anchored records remain meaningful, and genesis resumes
at the next height with a new chain ID for relaunch. Zero-height export is refused. `--jail-allowed-addrs` limits the exported
validator set to listed operators for a relaunch that lost more than a third of its power. Use `arkd export --help` for
height and output options, and validate the exported genesis before the coordinated relaunch.

### Upgrade binary policy

The security committee can schedule a software upgrade during its term
(`CommitteePlanUpgrade`), and the plan's `info` field is free text. Cosmovisor
reads that field: with `DAEMON_ALLOW_DOWNLOAD_BINARIES=true` it fetches
whatever binary the plan names, verifies only that the download matches the
checksum the plan author chose, and runs it at the upgrade height. On such a
fleet, the committee key — or anyone holding it — chooses what every validator
executes, with no vote and no operator in the loop.

Every validator therefore runs cosmovisor with

```text
DAEMON_ALLOW_DOWNLOAD_BINARIES=false
```

If the independently verified binary is not installed in the plan's directory at the upgrade height, the node halts
until the operator supplies it. This is a standing requirement,
not an emergency setting: it is what limits the committee's upgrade power to
choosing *when* the chain stops for an upgrade rather than *what* runs after
it. The rehearsal harness (`contrib/scripts/upgrade-rehearsal.sh`) sets it the
same way; a real node that differs from the rehearsal in this one setting has
delegated executable-selection authority that the standing policy excludes.

Before the halt, verify the plan name, height, expected build, and installed binary with the agreed release process.
After restart, confirm the expected version, block progression, and the node's signing or RPC role. If the build or
plan disagrees, keep the node stopped and resolve the mismatch with the network's upgrade coordination; do not enable
automatic downloads as a recovery shortcut. Follow the [emergency runbook](../governance/EMERGENCY_SUBMISSION_RUNBOOK.md) for private
committee submission, not for deciding which executable to trust.
