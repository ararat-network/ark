# Pricefeed operations

For validator operators running `pricefeed`, the off-chain process that prices the oracle's feeds: deploying it,
configuring its providers and routes, linking it to the node, following feed changes, and diagnosing problems.
[Implementation map](../../pricefeed/README.md), [oracle protocol](../../x/oracle/README.md),
[monitoring](PROCESS_MONITORING.md#32-sidecar), and [trust boundaries](../security/THREAT_MODEL.md) cover the adjacent
concerns. Commands run from the repository root unless another working directory is stated.

## Contents

- [1. Role and topology](#1-role-and-topology)
- [2. Install and run](#2-install-and-run)
- [3. Configuration](#3-configuration)
- [4. Providers](#4-providers)
- [5. Linking the node](#5-linking-the-node)
- [6. Feed changes](#6-feed-changes)
- [7. Checking it](#7-checking-it)
- [8. Reload and replacement](#8-reload-and-replacement)
- [9. Troubleshooting](#9-troubleshooting)
- [10. Releases and compatibility](#10-releases-and-compatibility)

## 1. Role and topology

Every validator runs a sidecar. Two links run in opposite directions: the node polls the sidecar for a price snapshot
each `interval`, and puts it in its vote extension; the sidecar polls a chain node's gRPC port for the oracle's feed
registry, so it prices exactly the feeds the chain asks for, and warms scheduled additions before they activate. The
sidecar fetches from external price providers, takes the median of their observations for each pair, and resolves each
feed to NOAH per unit through its configured routes.

The simplest deployment runs the sidecar on the validator's host, where both links stay on loopback. A sidecar on
another host needs TLS or an explicitly protected plaintext link (§5). A second sidecar, listed second in the node's
`sidecar_addresses`, keeps prices flowing while the first restarts (§8). The sidecar's chain client can read the
registry from the validator itself or a sentry, up to four addresses in preference order.

## 2. Install and run

Use a release binary from a `pricefeed/vX.Y.Z` tag, verified as [release
verification](RELEASE_VERIFICATION.md#verify-a-standalone-download) describes, or build one with
`make build-pricefeed`. Then write and check its configuration:

```sh
./build/pricefeed init
./build/pricefeed config validate
```

`init` writes `~/.ark/pricefeed/pricefeed.toml`, a directory of its own rather than the node's `config/`, so the two
processes need not share a user; `--config` points anywhere else. It refuses an existing file unless `--overwrite` is
supplied. The file holds provider credentials: keep it readable only by the sidecar's user. A structurally valid file
does not prove that every feed can be priced; `pricefeed check` does (§7).

| Listener | Flag | Default | Purpose |
| --- | --- | --- | --- |
| Public gRPC and HTTP gateway | `--address` | `127.0.0.1:8080` | Serves the node its prices |
| Admin gRPC | `--admin-address` | `127.0.0.1:8081`, loopback only | Runtime config reload; empty disables it |
| Prometheus metrics | `--metrics`, `--metrics-address` | off, `127.0.0.1:9091` | [Sidecar metrics](PROCESS_MONITORING.md#32-sidecar) |
| pprof | `--pprof`, `--pprof-address` | off, `127.0.0.1:6060`, loopback only | Profiling |

In the default `--tls-mode local` the public listener binds loopback only; §5 covers serving another host. Listener,
TLS, and telemetry options are startup flags: change them by restarting.

A panic in a provider or the chain-state client, or a listener failing, stops the whole sidecar: the process exits
non-zero rather than keep pricing from a component in an unknown state. Run it under a supervisor that restarts it:

```ini
[Unit]
Description=Ark price-feed sidecar
After=network-online.target
Wants=network-online.target

[Service]
User=pricefeed
ExecStart=/usr/local/bin/pricefeed start --config /etc/pricefeed/pricefeed.toml --log-format json --metrics
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## 3. Configuration

`pricefeed.toml` has four parts. The loader refuses a key it does not know, so a misspelled setting fails validation
rather than falling back to a default. Run `pricefeed config validate` after every edit.

**Top level.** `update_interval` is how often cached provider prices are resolved into the public snapshot.
`fallback_feeds` lists the feeds served until the first registry snapshot arrives from the chain.

**`[client]` and `[client.tls]`.** The chain nodes the registry is read from:

```toml
[client]
addresses = ["127.0.0.1:9090"]
timeout = "2s"
interval = "5s"
```

`addresses` lists at most four nodes in preference order: a local node or sentry first, a fallback behind it. The
sidecar polls the first that answers and stays on it until it fails, then sweeps the rest in order under `timeout`
each. There is no fail-back, so a flapping preferred node cannot bounce the sidecar between endpoints. All of them are
dialled with the one `[client.tls]` table (§5), and a failed sweep keeps the last known feed set rather than emptying
it. The node that answers is the `address` label on `ark_pricefeed_chainstate_refreshes_total`.

**`[providers.<name>]`.** One table per provider. The table name, `name`, and the transport table's `name` must agree,
and `name` must be one of the [registered providers](#4-providers).

| Key | Meaning |
| --- | --- |
| `transport_type` | `api` for HTTP polling, `websocket` for streaming |
| `markets` | `{ pair = "BASE/QUOTE", symbol = "<venue symbol>" }` entries: the canonical pair and the venue's name for it |
| `max_price_age` | The oldest cached sample still used; older samples are skipped |
| `max_unchanged_age` | How long a heartbeat confirming an unchanged price keeps a sample alive; zero, never |
| `[providers.<name>.api]` | `interval` between polls, `timeout`, `requests_per_second` (zero, unlimited), `batch_size` (zero, the provider's grouping) |
| `[providers.<name>.websocket]` | Reconnect, handshake, read and write timeouts, `ping_interval`, `write_interval`, `max_tickers_per_connection` (zero, one connection), `max_subscriptions_per_batch` |
| `[[...endpoints]]` | `url`, and `authentication = { api_key, api_key_header }` for a provider that needs a key |

Endpoints must be HTTPS for APIs and WSS for WebSockets. Both refuse redirects before a second request, including
same-origin redirects, so configure the final endpoint URL. An API handler may change the path and query but must keep
the configured HTTPS origin before credentials are attached. Set `max_price_age` above the polling `interval`, with room
for a failed poll: the generated Frankfurter table polls every minute and keeps a sample 90 seconds.

**`[resolver]`.** How each feed resolves to NOAH per unit:

```toml
[[resolver.routes.aeur]]
name = "eur-usd-noah"
pairs = ["EUR/USD", "USD/NOAH"]

[[resolver.bootstrap_prices]]
pair = "USD/NOAH"
price = "0.729394602479941648"
valid_until = "2027-10-29T00:00:00Z"
```

A route's legs multiply, each leg the median of every provider observing that pair in either orientation. Several
routes for one feed are averaged. A feed with no route resolves through its direct `UNIT/NOAH` pair. A bootstrap price
serves a leg only while no provider observes it and never past `valid_until`; [the resolver](../../pricefeed/sidecar/resolver/README.md)
owns the arithmetic. Every default route ends on the `USD/NOAH` leg, priced by the bootstrap until NOAH trades: one
NOAH an XDR at the 1.371 cross.

## 4. Providers

The generated file enables three currency services; the other adapters are configured by hand.

| Provider name | Kind | Credential |
| --- | --- | --- |
| `frankfurter_api` | Currency rates | None |
| `currencybeacon_api` | Currency rates | API key, `Authorization: Bearer <key>` |
| `openexchangerates_api` | Currency rates | App ID, `Authorization: Token <app_id>` |
| `coingecko_api` | Crypto aggregator | Optional pro key, `x-cg-pro-api-key` |
| `coinmarketcap_api` | Crypto aggregator | API key, `X-CMC_PRO_API_KEY` |
| `gecko_terminal_api` | On-chain token prices by contract address | None |
| `polymarket_api` | Outcome-token prices | None |
| `binance_api`, `bitstamp_api`, `coinbase_api`, `kraken_api` | Exchange REST | None |
| `binance_ws`, `bitfinex_ws`, `bitstamp_ws`, `bybit_ws`, `coinbase_ws`, `crypto_dot_com_ws`, `gate_ws`, `huobi_ws`, `kraken_ws`, `kucoin_ws`, `mexc_ws`, `okx_ws` | Exchange WebSocket | None |

The [API](../../pricefeed/sidecar/providers/api/README.md) and [WebSocket](../../pricefeed/sidecar/providers/websocket/README.md)
adapter guides describe each venue, and each adapter's `config.go` holds its endpoint and default settings. Replace the
generated `YOUR_API_KEY` and `YOUR_APP_ID` placeholders, or remove those providers: a placeholder credential fails every
request.

Give every leg at least two independent providers, three where possible: the median of one is that provider's price,
and alert S5 fires on it. Size polling intervals to each service's quota; the generated intervals stay inside the free plans of
CurrencyBeacon and Open Exchange Rates.

A WebSocket provider table, here Kraken for a NOAH market:

```toml
[providers.kraken_ws]
name = "kraken_ws"
transport_type = "websocket"
max_price_age = "1m0s"
max_unchanged_age = "0s"
markets = [
  { pair = "NOAH/USD", symbol = "NOAH/USD" },
]

[providers.kraken_ws.websocket]
name = "kraken_ws"
max_buffer_size = 1024
reconnection_timeout = "10s"
post_connection_timeout = "1s"
handshake_timeout = "10s"
enable_compression = false
read_timeout = "10s"
write_timeout = "5s"
ping_interval = "0s"
write_interval = "100ms"
max_tickers_per_connection = 0
max_subscriptions_per_batch = 1

[[providers.kraken_ws.websocket.endpoints]]
url = "wss://ws.kraken.com/v2"
```

Do not add NOAH markets to a provider until the [opening runbook](../governance/GOVERNANCE_RUNBOOK.md#opening-conversion)
calls for it (§6).

## 5. Linking the node

In the node's `config/app.toml`, enable the client and name the sidecars, in preference order:

```toml
[pricefeed]
enabled = true
sidecar_addresses = ["127.0.0.1:8080"]
client_timeout = "3s"
price_ttl = "10s"
interval = "1500ms"

[pricefeed.tls]
mode = "local"
```

Edit the existing tables rather than adding duplicate TOML tables. These are node boot settings and take effect after
an `arkd` restart, which refuses to start unless `client_timeout` is under `price_ttl` minus `interval`. With
`enabled = false`, which `arkd init` writes, `start` warns once and a validator on the node abstains from every oracle
vote; the vote handler notes each abstention at Info rather than as a failure. The sidecar reads the registry from the
node's gRPC server, which `[grpc]` in `app.toml` enables on `localhost:9090` by default.

### TLS

Both links default to `local` mode, which permits plaintext only on local endpoints. Remote connections require
`mode = "tls"` or an explicit `mode = "plaintext"` for an externally protected link. Missing mode means local;
supplying certificate files without TLS mode is an error. TLS uses version 1.3 or newer with normal hostname and
certificate verification.

- **Node to sidecar.** Start the sidecar with `--tls-mode tls`, `--tls-cert-file`, and `--tls-key-file`.
  Add `--tls-client-ca-file` to require client certificates chaining to that bundle. Set `mode = "tls"`
  under `[pricefeed.tls]` in the node's `app.toml`. An empty `ca_file` uses system roots; a supplied
  bundle replaces them. Set `cert_file` and `key_file` for client authentication and `server_name`
  when the certificate names the service rather than the dialled host or IP.
- **Sidecar to node.** Put a TLS terminator in front of the node's plaintext gRPC port and select
  `mode = "tls"` under `[client.tls]` in `pricefeed.toml`. The same root and client-certificate rules
  apply. `pricefeed prices` and `pricefeed check` accept `--tls-mode` and `--tls-*` files;
  `check` also accepts `--chain-tls-mode` and `--chain-tls-*` for its chain connection.

**Mode is explicit:** a configuration that sets certificate files must also set the TLS mode, and remote
plaintext must select plaintext mode, since the default refuses remote endpoints. Local plaintext needs no
mode. The Docker localnet selects plaintext explicitly for its container-to-container links.

Long-lived connections check certificate/key contents every minute, including timestamp-preserving
file replacements and symlink swaps. Handshakes read only the current certificate from memory.
Malformed, mismatched, expired, not-yet-valid, or unsuitable replacements retain the previous identity
and log an error; recovery and successful rotation are logged too. Expiry warnings begin 24 hours
before the earliest presented certificate expires. Repeated failure/expiry messages are limited to
once an hour. Publish complete pairs, preferably by atomically swapping a directory symlink.
Rotation affects new handshakes; it does not reauthenticate or revoke existing connections.

Trust bundles are fixed for each loaded transport. Changing a bundle at the same path requires a
restart. Short-lived commands load once.

## 6. Feed changes

The sidecar prices whatever the chain's feed registry lists, so it follows feed changes without a reload, provided its
configuration can price them.

- **Governance adds a feed.** Before the proposal's vote ends, add markets for the feed's legs to at least two providers
  and, unless the direct `UNIT/NOAH` pair is available, a route; then reload (§8). The sidecar reads scheduled additions
  and prices them before activation, two blocks after the proposal executes. A feed the sidecar cannot price earns the
  validator nothing for it from the block it activates.
- **Governance removes a feed.** Nothing is needed: the sidecar stops serving it when the registry drops it. Its
  markets and route can be removed at the next edit.
- **Conversion opens.** Operators add NOAH markets in one coordinated window, as step 1 of the [opening
  runbook](../governance/GOVERNANCE_RUNBOOK.md#opening-conversion) sets out, not as each notices a listing. The first
  live sample replaces the bootstrap `USD/NOAH` price on every sidecar that carries the venue, and
  `ark_pricefeed_bootstrap_price_uses_total` falling to zero shows the switch.

## 7. Checking it

With both processes running:

```sh
./build/pricefeed prices --address 127.0.0.1:8080
./build/pricefeed check --address 127.0.0.1:8080 --chain-address 127.0.0.1:9090
```

`prices` prints the current snapshot; `check` samples it against the chain's feed set and reports any feed it cannot
price. A snapshot alone does not establish coverage, and provider freshness and the chain's rate freshness are separate
stages. `prices` and `check` accept `--tls-mode` and `--tls-*` files; `check` also accepts `--chain-tls-mode` and
`--chain-tls-*` for its chain connection.

On chain, what matters is whether the validator's reports count:

- `arkd query oracle attendance <valoper>` shows attended blocks against eligible ones in the current window. Under
  `min_attendance_per_window`, 5% at launch, at the weekly settlement, the validator is jailed for a week.
- `arkd query oracle reward-weight <valoper>` shows the weight its accurate reports have earned: a price outside the
  `reward_band` around the chain's median earns nothing for that feed.
- `arkd query oracle exchange-rates` shows the fresh consensus rates; `stored-rates` shows every rate with its age.

[Process monitoring](PROCESS_MONITORING.md#32-sidecar) defines the sidecar alerts S1–S7 and the node's N1–N3.

## 8. Reload and replacement

**Runtime reload.** After editing the running process's config file:

```sh
./build/pricefeed config validate --config /path/to/pricefeed.toml
./build/pricefeed config reload --admin-address 127.0.0.1:8081
```

Reload asks the running service to read the config path fixed at startup; the reload command's `--config` does not
select a different file for that process. Runtime prepares replacement providers and chain-client material before
committing an update, and an invalid replacement leaves the active runtime intact. A chain-client address or TLS change
loads a new transport before accepting the update; timing-only changes keep the existing material.

**Replacing a sidecar.** Preconfigure two sidecar endpoints in the node's `sidecar_addresses` and warm the replacement
before stopping the old process. The node stays on its active endpoint until failure, then tries alternatives. Each
attempt has its own `client_timeout`; the last successful snapshot remains usable only within `price_ttl`. The node
refuses to start unless `client_timeout` is less than `price_ttl` minus `interval`, so a sweep that waits out one hung
sidecar still lands in time; each further hung sidecar costs another `client_timeout`. Continuity therefore depends on
replacement readiness, feed coverage, and the failover sweep completing before the usable snapshot expires.

Changing the node's address list requires a node restart. Existing connections do not acquire new trust roots when a CA
file is replaced. Separate these changes from a sidecar-only replacement when planning availability. A successful
rolling sidecar change can preserve price service without requiring a validator restart, but is not an unconditional
zero-gap guarantee.

## 9. Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| The validator abstains from every oracle vote | `[pricefeed] enabled = false` in the node's `app.toml`; the node warns at start. |
| The node cannot reach the sidecar | Address, TLS mode, or firewall: alert N2, and the node's log. |
| `check` reports a feed missing | No provider covers one of its route's legs, or every sample for a leg is past `max_price_age`: alerts S1–S3. |
| A provider fails every request | A placeholder or expired credential, a quota exhausted, or a redirect refused; its `error_code` on `ark_pricefeed_provider_responses_total` says which. |
| A provider answers but contributes nothing | Its samples are past `max_price_age` or `max_unchanged_age`, as a frozen WebSocket stream looks: alert S1. |
| Prices still use the bootstrap | No provider observes a leg, often `USD/NOAH` before opening: alert S4. Past `valid_until` the leg fails instead. |
| A newly activated feed is missing | The registry is stale: no `[client]` address answers gRPC (alert S7), or the sidecar lacks markets for it (§6). |
| Reload changes nothing | The replacement failed validation and the runtime kept the old one; the sidecar logs why. |
| The sidecar exits | A provider panic or a listener failure stops the process by design; the supervisor restarts it, and the node fails over to a second sidecar meanwhile. |
| Attendance falls while prices look healthy | The node's snapshot is older than `price_ttl` (alert N3), or the validator's reports fail verification on peers (N5). |

## 10. Releases and compatibility

The sidecar releases on its own cadence under `pricefeed/vX.Y.Z` tags: `make release-pricefeed` builds one from the tag
on `HEAD`, and `make build-pricefeed` builds the binary from the working tree. Nodes and sidecars release
independently.

The node-to-sidecar contract is the proto package `ark.pricefeed.v1`, generated into `pricefeed/api`.
The node uses `pricefeed/client`; the sidecar implements the service in `pricefeed/sidecar`.
Within v1, changes must be additive: existing fields and RPCs must not be changed or removed.
New fields must preserve operation with older peers; a new RPC is unavailable on older sidecars,
so callers need a fallback to retain compatibility with them. Incompatible changes require a new
service version, such as v2. A missing required service or RPC returns `Unimplemented`.
Prices use the compact encoding in `pkg/encoding`, also carried by vote extensions, so that
encoding is part of the compatibility contract; fixed byte vectors in its tests pin it.

v1 is frozen from the first `pricefeed/vX.Y.Z` release, since that is the first sidecar an operator can run
beside a newer node. Until then it may still change. From then, `make proto-check-pricefeed`, which CI runs on
every pull request, refuses a breaking change to `proto/ark/pricefeed` against the newest release.

The sidecar's reported build version identifies the build for operators. The node logs it at Info on
first contact and whenever it changes, exports no metric for it, and never gates compatibility on it.
