# Node operations

For operators running `arkd`: deploying and configuring a node, joining a network, running a validator, keeping its
keys, upgrading, and diagnosing problems. [Pricefeed operations](PRICEFEED_OPERATIONS.md) owns the validator's price
sidecar, [process monitoring](PROCESS_MONITORING.md) what to watch, [release verification](RELEASE_VERIFICATION.md) how
to check a downloaded binary, and [launch genesis](../governance/GENESIS.md) the launch values. Commands run from the
repository root unless another working directory is stated.

## Contents

- [1. Roles and topology](#1-roles-and-topology)
- [2. Install](#2-install)
- [3. Configuration](#3-configuration)
- [4. Joining a network](#4-joining-a-network)
- [5. Running a validator](#5-running-a-validator)
- [6. Keys and backups](#6-keys-and-backups)
- [7. Upgrades](#7-upgrades)
- [8. Relaunch](#8-relaunch)
- [9. Troubleshooting](#9-troubleshooting)
- [10. Development networks](#10-development-networks)

## 1. Roles and topology

| Role | Runs | Exposes |
| --- | --- | --- |
| Validator | `arkd` with its consensus key, and a [price sidecar](PRICEFEED_OPERATIONS.md) | P2P to its own sentries only |
| Sentry | `arkd` without a key, relaying for the validator behind it | P2P publicly |
| RPC node | `arkd` serving queries and broadcasts, and snapshots for state sync | RPC, REST, and gRPC behind a proxy |

A validator should never face the public network: it peers only with its sentries, which absorb connection load and
hide its address. Run at least two sentries, in different locations. The sidecar runs beside the validator, on the same
host or a private link, and reads the feed registry from the validator's own gRPC port or a sentry's.

| Port | Service | Default bind | Open to |
| --- | --- | --- | --- |
| 26656 | CometBFT P2P | all interfaces | sentries: everyone; validator: its sentries |
| 26657 | CometBFT RPC | loopback | operators; the public only on RPC nodes, behind a proxy |
| 26658 | ABCI proxy | loopback | nobody |
| 1317 | REST API, disabled by default | loopback | RPC nodes' clients |
| 9090 | gRPC | loopback | the sidecar's chain client; RPC nodes' clients |
| 9464 | application metrics, `[prometheus]` in `app.toml`, off by default | loopback | the scraper |
| 26660 | CometBFT metrics, `[instrumentation]` in `config.toml`, off by default | all interfaces | the scraper |

The sidecar's own listeners are in [pricefeed operations](PRICEFEED_OPERATIONS.md#2-install-and-run).

## 2. Install

Use a release binary, verified as [release verification](RELEASE_VERIFICATION.md#verify-a-standalone-download)
describes, or build from source with the toolchain pinned in [mise.toml](../../mise.toml) and the host's Wasm/native
build prerequisites:

```sh
make build
./build/arkd version
```

`make build` stamps version and commit metadata; `go build -o build/arkd ./cmd/arkd` builds the same node without
them. Container builds are in [contributor tooling](../../contrib/README.md).

Run the node under [cosmovisor](https://docs.cosmos.network/main/build/tooling/cosmovisor), which swaps the binary at
an upgrade height, as a dedicated unprivileged user. Initialise the home first (§4), then:

```sh
export DAEMON_NAME=arkd DAEMON_HOME=/var/lib/ark
cosmovisor init /usr/local/bin/arkd
```

A systemd unit for it:

```ini
[Unit]
Description=Ark node
After=network-online.target
Wants=network-online.target

[Service]
User=ark
ExecStart=/usr/local/bin/cosmovisor run start --home /var/lib/ark
Environment=DAEMON_NAME=arkd
Environment=DAEMON_HOME=/var/lib/ark
Environment=DAEMON_ALLOW_DOWNLOAD_BINARIES=false
Environment=DAEMON_RESTART_AFTER_UPGRADE=true
Environment=COSMOS_SDK_CONFIG_SCOPE=arkd
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

`DAEMON_ALLOW_DOWNLOAD_BINARIES=false` is a [standing policy](#upgrade-binary-policy), not a default to revisit.
`COSMOS_SDK_CONFIG_SCOPE` must be non-empty and constant for the process lifetime: it pins SDK configuration lookup to
a stable scope, so address parsing does not depend on a hostname-derived lookup changing while the node runs. Set it
in every long-running node's environment, cosmovisor or not.

Keep the clock synchronised with NTP. A proposer leaves transactions out of its block when its clock finds the
block's prices older than the Oracle's `max_exchange_rate_age`, so a clock running a minute fast proposes empty blocks
and one running slow weakens that guard ([proposal handlers](../../abci/README.md#proposal-handlers)).

## 3. Configuration

The default home is `~/.ark`; `--home` selects another. `config.toml` configures CometBFT, `app.toml` the application
and its servers, and `client.toml` CLI defaults.

| Setting | Validator | Sentry | RPC node |
| --- | --- | --- | --- |
| `config.toml` `[p2p] pex` | `false` | `true` | `true` |
| `[p2p] persistent_peers` | its sentries | its validator, the other sentries, and public peers | the network's peers |
| `[p2p] private_peer_ids`, `unconditional_peer_ids` | | the validator's node ID | |
| `[p2p] addr_book_strict` | `false` when its sentries are on private addresses | `false` for a private validator address | `true` |
| `[tx_index] indexer` | `null` | `null` | `kv` |
| `app.toml` `[api] enable` | `false` | `false` | `true` |
| `[state-sync] snapshot-interval` | `0` | `0` | positive, such as `1000` |
| `[pricefeed] enabled` | `true` ([sidecar link](PRICEFEED_OPERATIONS.md#5-linking-the-node)) | `false` | `false` |
| `[prometheus] enabled` | `true` | `true` | `true` |

`pruning = "default"` suits every role; an archive node serving historical queries sets `"nothing"`. `arkd query tx`
reads the index of the node it asks, so only nodes with `indexer = "kv"` answer it. `minimum-gas-prices` does not affect
admission: every node admits at Treasury's consensus base fee. [Mempool
policy](../../app/mempool/README.md#admission-service-and-sdk-behaviour) owns the coupled CometBFT and application
mempool settings, which the start command checks. `[wasm]` is wasmd's table: `query_gas_limit` caps the gas one smart
query may spend and `memory_cache_size` sizes the contract cache in MiB; `arkd start --wasm.<key>` overrides either for
one run. [Process monitoring](PROCESS_MONITORING.md) owns `[prometheus]`, `[telemetry]`, and `otel.yaml`.

Edit configuration with the CLI rather than by hand where it can:

- `arkd config set <app|config|client> <key> <value>` changes an existing key, checking app.toml against the
  validation start applies and config.toml against CometBFT's; it does not create missing keys.
- `arkd config validate [app|config|client]` runs those checks over a file alone.
- `arkd config diff` compares app settings with this binary's defaults.
- `arkd config migrate --stdout` shows what a migration would write; `migrate` adds missing app.toml keys and leaves
  config.toml and client.toml alone.

`validate` and `set` take a path to a `.toml` file in place of the name. Configuration changes take effect at restart.

## 4. Joining a network

1. Install the network's current binary (§2). A node that syncs from genesis instead needs every historical binary,
   one per upgrade, under cosmovisor's `upgrades/` directory; state sync needs only the current one.
2. Initialise the home: `arkd init <moniker> --chain-id <chain-id> --home <home>`. `arkd init` writes SDK and
   CometBFT defaults, not the network's genesis or launch policy.
3. Fetch the network's genesis, compare its SHA-256 with the published hash (`shasum -a 256 genesis.json`), copy it
   over `config/genesis.json`, and run `arkd genesis validate --home <home>`.
4. Set `[p2p] seeds` or `persistent_peers` to the network's published peers, as `<node-id>@<host>:26656`. A node's ID
   comes from `arkd comet show-node-id --home <home>`; it is not an operator address.
5. For state sync, set under `[statesync]` in `config.toml`:

   ```toml
   enable = true
   rpc_servers = "https://rpc-1.example:443,https://rpc-2.example:443"
   trust_height = 1234000
   trust_hash = "<block hash at trust_height>"
   ```

   Take the trust height a little below the latest height of a node you trust, and its hash from
   `curl -s <rpc>/block?height=<height> | jq -r .result.block_id.hash`. The two RPC servers must be independent and
   must agree; peers that serve snapshots must be reachable. Snapshots carry contract code and state with the stores.
   CometBFT ignores the setting once the store is non-empty.
6. Start the node and wait for `curl -s localhost:26657/status | jq .result.sync_info.catching_up` to read `false`.

[Docker localnet's state sync](../../contrib/localnet/statesync.sh) runs the same recipe end to end.

## 5. Running a validator

**Before creating it.** Run a synced node in the validator role (§1, §3), with its sidecar running and
`pricefeed check` passing ([pricefeed operations](PRICEFEED_OPERATIONS.md#7-checking-it)). From the first block it is
bonded, a validator must report prices in its vote extensions: one that abstains loses attendance and can be jailed.
Fund the operator account with the self-delegation and gas. Create the operator key with a durable keyring backend,
`os` or `file`, never `test`; the address is `arkd keys show <key> -a`, the operator address
`arkd keys show <key> --bech val -a`.

**Create it.** Write the consensus public key from `arkd comet show-validator --home <home>` into a `validator.json`:

```json
{
  "pubkey": {"@type": "/cosmos.crypto.ed25519.PubKey", "key": "<from show-validator>"},
  "amount": "<amount>anoah",
  "moniker": "<moniker>",
  "identity": "",
  "website": "",
  "security": "<contact>",
  "details": "",
  "commission-rate": "0.05",
  "commission-max-rate": "0.20",
  "commission-max-change-rate": "0.01",
  "min-self-delegation": "1"
}
```

and send `arkd tx staking create-validator validator.json --from <key> --gas auto`. Amounts are `anoah` base units,
eighteen decimals. Commission below `min_commission_rate`, 5% at launch, is refused, and the maximum rate and maximum
daily change are fixed for the validator's life. The CLI prices the transaction itself and prints the fee before
confirming ([client fees](../clients/CLIENT_FEES.md)). The validator joins the active set if its bonded stake ranks
within `max_validators`.

**Check it.**

- `arkd query staking validator <valoper>` shows it bonded and unjailed.
- `arkd query slashing signing-info '<show-validator output>'` shows `missed_blocks_counter` staying low.
- After a few blocks, `arkd query oracle attendance <valoper>` shows attended blocks rising with eligible ones, and
  `arkd query oracle reward-weight <valoper>` a positive weight.

**Penalties.** Values are the launch parameters ([genesis §11](../governance/GENESIS.md#11-cosmos-sdk-modules),
[§8](../governance/GENESIS.md#8-oracle)); query `arkd query slashing params` and `arkd query oracle params` for the live
ones.

| Fault | Condition | Penalty |
| --- | --- | --- |
| Downtime | Signed under 5% of the last 10,000 blocks, about 16 hours | Jailed 600 s, 0.01% of stake slashed |
| Double sign | Two different votes at one height | 5% slashed, tombstoned: never unjailed |
| Oracle absence | Reported in under 5% of the eligible blocks in a weekly attendance window | Jailed a week, not slashed |

A jailed validator earns nothing and leaves the active set. Fix the cause, then send
`arkd tx slashing unjail --from <key>`; a downtime jail admits it after its 600 s, an oracle jail after its week
(`jail_duration`). Edit the description or commission with `arkd tx staking edit-validator`.

## 6. Keys and backups

| File | What it is | Back up |
| --- | --- | --- |
| `config/priv_validator_key.json` | The consensus key: signs blocks and votes | Yes, encrypted and offline |
| `data/priv_validator_state.json` | The last height, round, and step signed | No: it must only ever move forward |
| `config/node_key.json` | The P2P identity peers know the node by | Yes |
| The operator key's mnemonic | Controls the stake, commission, and governance votes | Yes, offline |

**Never run the consensus key in two places.** Two processes signing with one key double sign, which is tombstoning
and a 5% slash. Never start a validator from an older `priv_validator_state.json` either: it lets the node sign a
height it already signed.

**Moving a validator.** The chain has no consensus-key rotation, so the key moves with the validator:

1. Bring the new host up as a synced non-validator node with its own `node_key.json`, peered with the sentries,
   which add its node ID to `private_peer_ids` and `unconditional_peer_ids`.
2. Stop the old validator, and make sure it cannot restart: disable its service.
3. Copy `priv_validator_key.json` and the current `priv_validator_state.json` from the stopped node to the new one.
4. Set `[consensus] double_sign_check_height = 10` on the new node, which then refuses to start while the key's votes
   still appear in the last ten blocks, and restart it. Set it back to `0` once the node signs, since it delays every
   ordinary restart by as many blocks.
5. Confirm signing with `signing-info`, then remove the old node's ID from the sentries.

The downtime window tolerates a move of minutes; a missed block costs nothing until the window's floor.

## 7. Upgrades

[Upgrade authoring](../../app/upgrade/README.md) and the [rehearsal](../../contrib/scripts/README.md#upgrade-rehearsal)
cover implementation and validation. A governance or Security-committee plan halts every node at its height until it
runs the plan's binary.

1. Read the plan: `arkd query upgrade plan` shows the name and height. Match it against the release announcement.
2. Build or download the release and verify it ([release verification](RELEASE_VERIFICATION.md)).
3. Stage it: `cosmovisor add-upgrade <plan-name> <path-to-arkd>` places it at
   `$DAEMON_HOME/cosmovisor/upgrades/<plan-name>/bin/arkd`.
4. At the height the node halts and logs `UPGRADE NEEDED`; cosmovisor switches to the staged binary and restarts it.
5. Confirm `arkd version`, block progression, and the node's signing or RPC role.

Independent RPC nodes can be upgraded in rotation to preserve service availability. A single validator's restart can
interrupt its signing; a coordinated consensus upgrade requires the network to reach the upgrade boundary and restart on
compatible code. Do not run the same validator signing key concurrently in old and replacement processes.

### Upgrade binary policy

The security committee can schedule a software upgrade during its term (`CommitteePlanUpgrade`), and the plan's `info`
field is free text. Cosmovisor reads that field: with `DAEMON_ALLOW_DOWNLOAD_BINARIES=true` it fetches whatever binary
the plan names, verifies only that the download matches the checksum the plan author chose, and runs it at the upgrade
height. On such a fleet, the committee key — or anyone holding it — chooses what every validator executes, with no vote
and no operator in the loop.

Every validator therefore runs cosmovisor with

```text
DAEMON_ALLOW_DOWNLOAD_BINARIES=false
```

If the independently verified binary is not installed in the plan's directory at the upgrade height, the node halts
until the operator supplies it. This is a standing requirement, not an emergency setting: it is what limits the
committee's upgrade power to choosing *when* the chain stops for an upgrade rather than *what* runs after it. The
rehearsal harness (`contrib/scripts/upgrade-rehearsal.sh`) sets it the same way; a real node that differs from the
rehearsal in this one setting has delegated executable-selection authority that the standing policy excludes.

If the build or plan disagrees with the agreed release, keep the node stopped and resolve the mismatch with the
network's upgrade coordination; do not enable automatic downloads as a recovery shortcut.

## 8. Relaunch

`arkd export` is a continuation export: absolute heights and height-anchored records remain meaningful, and genesis
resumes at the next height. The export keeps the original `chain_id` and `genesis_time`. Set `chain_id` to the
relaunch's new ID, and `genesis_time` to the coordinated restart time, at or after the export height's block time and
identical in every validator's file, because Oracle and Disbursement import refuse records dated after genesis. Records
anchored in block time keep their absolute times, so the outage counts against them: committee terms, pending claims'
closing times, unactivated settlement plans, grant schedules, and the attestation windows of open Reserve positions.
Before the relaunch, decide whether to push pending claims' `closing_time`, unactivated plans' `activation_time`, and
open positions' `attested_time` forward by the outage in the exported genesis, returning to governance the veto and
cancel windows the outage ate and to the committee the attestation time it ate, and whether any committee term lapsed
during it; a position left to lapse earns nothing until the committee restates it. Zero-height export is refused.
`--jail-allowed-addrs` limits the exported validator set to listed operators for a relaunch that lost more than a third
of its power. Use `arkd export --help` for height and output options, and validate the exported genesis before the
coordinated relaunch.

## 9. Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `start` refuses the mempool configuration | `config.toml [mempool]` must keep `type = "flood"`, `recheck = true`, and `size` at least `app.toml [mempool] max-txs`; fix with `arkd config set config mempool.type flood` and the like ([mempool policy](../../app/mempool/README.md#admission-service-and-sdk-behaviour)). |
| `start` refuses the price-feed client settings | `client_timeout` must be under `price_ttl` minus `interval` ([replacing a sidecar](PRICEFEED_OPERATIONS.md#8-reload-and-replacement)). |
| `start` warns that the price feed is disabled | `[pricefeed] enabled = false`: harmless on a sentry or RPC node, an abstaining validator otherwise. |
| A validator abstains from oracle votes | The sidecar is unreachable or its snapshot is older than `price_ttl`: alerts N1–N3 in [process monitoring](PROCESS_MONITORING.md#31-node) and [the sidecar's own checks](PRICEFEED_OPERATIONS.md#9-troubleshooting). |
| Blocks carry no transactions | By design for the first block after a stall or upgrade past `max_exchange_rate_age`; persistently, check the proposer's clock and `arkd query oracle exchange-rates`. |
| The validator is jailed | `signing-info` shows a downtime jail; an `EventOracleJail` in the jailing block shows an oracle one. Fix the cause, then unjail (§5). |
| The node stops on an app-hash mismatch | `arkd module-hash-by-height <height> --home <home>`, run against the stopped node's data, reports which module store differs; compare with a node that stayed in consensus. |
| No peers | Check `persistent_peers` IDs and ports, the firewall, and `addr_book_strict` for private addresses. |
| State sync finds no snapshot | No reachable peer serves snapshots: add peers whose nodes set `snapshot-interval`. |

## 10. Development networks

### Disposable host testnet

For a local developer validator, use a fresh directory and the test keyring backend:

```sh
ARK_TESTNET_DIR=$(mktemp -d)
./build/arkd testnet init-files --genesis app/genesis/testnet.json --validator-count 1   --output-dir "$ARK_TESTNET_DIR" --chain-id ark-local   --keyring-backend test --single-host --starting-ip-address 127.0.0.1
./build/arkd genesis validate --home "$ARK_TESTNET_DIR/node0/arkd"
COSMOS_SDK_CONFIG_SCOPE=arkd ./build/arkd start --home "$ARK_TESTNET_DIR/node0/arkd"
```

The generator starts from the testnet artefact and grants each validator a seat from its community pool; without
`--genesis` it generates from code defaults instead. Its test keyring is for disposable development. It does not
launch a pricefeed process. Configure and run a [sidecar](PRICEFEED_OPERATIONS.md) to produce oracle prices, or use the
[Docker localnet](../../contrib/localnet/README.md), which wires the processes together. Stop the foreground node with Ctrl-C.
Use a fresh directory for another run; do not reset an existing validator home as a setup step.

For multiple developers, generate the intended validator count and host addresses, distribute each validator's own home,
and agree on a single genesis. `--single-host` is for separate ports on one machine; omit it when using distinct hosts.

Each generated home carries a `client.toml` with its chain ID, keyring backend, key name, and node address, so client
commands against it need none of those as flags. `arkd testnet start` runs an in-process multi-validator network
from code defaults instead: no artefact, no seats. Its commit timeout must leave the first block inside the SDK
network's five-second start budget, which its one-second default does.

To test against real state, copy a node's home and run `arkd in-place-testnet <new-chain-id> <operator-address> --home
<copy>`: every validator is replaced by one bonded at the operator with the copy's consensus key, CometBFT's validator
set and last commit are rewritten to match, and the node starts under the new chain ID. `--trigger-testnet-upgrade
<name>` runs an upgrade handler in the first block. The copy's data folder is rewritten for good; the plain `start`
command resumes it afterwards.

### Manually assembling a testnet

When validators create their own keys, each participant initialises a fresh home with the agreed chain ID using
`arkd init <moniker> --chain-id <chain-id> --home <home>`, then creates its own key with `arkd keys add <name> --home <home>`.
Keep each participant's mnemonic and private validator key local to that participant.

The coordinator starts from the curated testnet artefact, [app/genesis/testnet.json](../../app/genesis/testnet.json),
copied over `config/genesis.json`, sets its `genesis_time`, which the seats vest from, and grants every validator's
seat in one `arkd genesis add-validator-seats --founder <address> --any-count <address>... --home <home>`, the
founder's at half the others' sum, with the founder's grant and stream written beside it, and the launch count bounds
lifted for the testnet, up to what its community pool funds ([genesis §14](../governance/GENESIS.md#assembly-sequence)).
Faucet and test accounts are added with `arkd genesis add-genesis-account <address> <amount>anoah --home <home>`; amounts
are base units in the chain's denomination, `anoah`. Each validator then creates its gentx with
`arkd genesis gentx <name> 5000000000000000000000000anoah --commission-rate 0.05 --chain-id ark-testnet-1 --home <home>`,
one seat at the commission floor, the founder's too. The coordinator collects all gentxs under `config/gentx`, runs
`arkd genesis collect-gentxs --home <home>`, checks the result with `arkd genesis validate` and `arkd genesis dry-run`,
and distributes the same final file to everyone.

In each `config/config.toml`, set `[p2p] persistent_peers` to the intended `<node-id>@<host>:<p2p-port>` peers. Obtain the
P2P node ID with `arkd comet show-node-id --home <home>`; it is not the validator operator address. Start only after all
participants have the same chain ID and genesis. Diagnose genesis/gentx validation failures directly rather than
starting a different genesis as a workaround.
