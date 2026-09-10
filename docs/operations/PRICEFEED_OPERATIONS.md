# Pricefeed operations

The node polls the sidecar for prices; the sidecar polls a chain node for oracle feed membership. Each connection has its
own configuration. [Implementation map](../../pricefeed/README.md), [oracle protocol](../../x/oracle/README.md),
[monitoring](PROCESS_MONITORING.md), and [trust boundaries](../design/THREAT_MODEL.md) cover the adjacent concerns.

## Local setup

From the repository root:

```sh
make build-pricefeed
./build/pricefeed init
./build/pricefeed config validate
./build/pricefeed start
```

`init` refuses an existing config unless `--force` is supplied. Review the generated provider credentials, market mappings,
routes, and bootstrap expiry before starting. Defaults include placeholder credentials for providers that need them;
a structurally valid file does not prove that every requested feed can be priced. Defaults live in
[pricefeed/config/default.go](../../pricefeed/config/default.go).

In the node's `config/app.toml`, enable its client and select the sidecar addresses. For a sidecar on the same host:

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

Edit the existing tables rather than adding duplicate TOML tables. These are node boot settings and take effect after an
`arkd` restart. Enable the node's gRPC query server and point the sidecar's `[client] addresses` at it; the generated
sidecar file defaults to `127.0.0.1:9090`. Keep the remaining validated generated settings unless you need to change them.

With both processes running:

```sh
./build/pricefeed prices --address 127.0.0.1:8080
./build/pricefeed check --address 127.0.0.1:8080 --chain-address 127.0.0.1:9090
```

A price snapshot alone does not establish full feed coverage. `check` samples against the chain's feed set; the node also
checks snapshot age when accepting and serving cached prices. Individual provider freshness and consensus rate freshness
are separate stages.

## Runtime reload and process settings

The default admin listener is loopback. After editing the running process's runtime config file:

```sh
./build/pricefeed config validate --config /path/to/pricefeed.toml
./build/pricefeed config reload --admin-address 127.0.0.1:8081
```

Reload asks the running service to read the config path fixed at startup; the reload command's `--config` does not select
a different file for that process. Runtime prepares replacement providers and chain-client material before committing
an update. Invalid replacements leave the active runtime intact. Public/admin listener addresses, process TLS selection,
and process telemetry options are startup settings; use `pricefeed start --help` and restart to change them.

## Transport, identity rotation, and releases

`pricefeed` is the off-chain price process each validator runs beside its node. `arkd` polls it for
prices, and the sidecar reads the feed registry back from the node's gRPC port. `pricefeed init` writes
its config, `pricefeed.toml`, under `~/.ark/pricefeed/`, a directory of its own rather than the node's
`config/`, so the two processes need not share a user; `--config` points anywhere else. Both links default to `local` mode, which permits plaintext only on local endpoints.
Remote connections require `mode = "tls"` or an explicit `mode = "plaintext"` for an externally
protected link. Missing mode means local; supplying certificate files without TLS mode is an error.
TLS uses version 1.3 or newer with normal hostname and certificate verification.

- **Node to sidecar.** Start the sidecar with `--tls-mode tls`, `--tls-cert-file`, and `--tls-key-file`.
  Add `--tls-client-ca-file` to require client certificates chaining to that bundle. Set `mode = "tls"`
  under `[pricefeed.tls]` in the node's `app.toml`. An empty `ca_file` uses system roots; a supplied
  bundle replaces them. Set `cert_file` and `key_file` for client authentication and `server_name`
  when the certificate names the service rather than the dialled host or IP.
- **Sidecar to node.** Put a TLS terminator in front of the node's plaintext gRPC port and select
  `mode = "tls"` under `[client.tls]` in `pricefeed.toml`. The same root and client-certificate rules
  apply. `pricefeed prices` and `pricefeed check` accept `--tls-mode` and `--tls-*` files;
  `check` also accepts `--chain-tls-mode` and `--chain-tls-*` for its chain connection.

**Upgrading existing configurations:** add TLS mode wherever certificate files are already set.
For remote plaintext, explicitly select plaintext mode; the default now refuses those endpoints.
Local plaintext configurations continue to work. The Docker localnet selects plaintext explicitly
for its container-to-container links.

Long-lived connections check certificate/key contents every minute, including timestamp-preserving
file replacements and symlink swaps. Handshakes read only the current certificate from memory.
Malformed, mismatched, expired, not-yet-valid, or unsuitable replacements retain the previous identity
and log an error; recovery and successful rotation are logged too. Expiry warnings begin 24 hours
before the earliest presented certificate expires. Repeated failure/expiry messages are limited to
once an hour. Publish complete pairs, preferably by atomically swapping a directory symlink.
Rotation affects new handshakes; it does not reauthenticate or revoke existing connections.

Trust bundles are fixed for each loaded transport. Changing a bundle at the same path requires a
restart. A sidecar chain-client address or TLS configuration change loads a new transport before
accepting the update; timing-only changes retain existing material. Short-lived commands load once.

Provider endpoints require HTTPS for APIs and WSS for WebSockets. Both refuse redirects before a
second request, including same-origin redirects. Configure the final endpoint URL. API handlers may
change the path and query but must retain the configured HTTPS origin before credentials are attached.

`addresses` under `[client]` in the sidecar's `pricefeed.toml` lists the chain nodes to query, in
preference order and at most four — a local sentry first, a fallback behind it. The sidecar polls the
first that answers and stays on it until it fails, then sweeps the rest in order under `timeout` each.
There is no fail-back, so a flapping preferred node cannot bounce the sidecar between endpoints. All of
them are dialled with the one `[client.tls]` table, and a failed sweep keeps the last known feed set
rather than emptying it. The chain node that answers is the `address` label on
`ark_pricefeed_chainstate_refreshes_total`.

The sidecar releases on its own cadence under `pricefeed/vX.Y.Z` tags: `make release-pricefeed` builds
one from the tag on `HEAD`, and `make build-pricefeed` builds the binary from the working tree.

## Node–sidecar compatibility

The node-to-sidecar contract is the proto package `ark.pricefeed.v1`, generated into `pricefeed/api`.
The node uses `pricefeed/client`; the sidecar implements the service in `pricefeed/sidecar`.
Within v1, changes must be additive: existing fields and RPCs must not be changed or removed.
New fields must preserve operation with older peers; a new RPC is unavailable on older sidecars,
so callers need a fallback to retain compatibility with them. Incompatible changes require a new
service version, such as v2. A missing required service or RPC returns `Unimplemented`.
Prices use the compact encoding in `pkg/encoding`, also carried by vote extensions, so that
encoding is part of the compatibility contract.

The sidecar's reported build version identifies the build for operators. The node logs and exports
it but never gates compatibility on it; nodes and sidecars release independently.

## Replacing a sidecar

Preconfigure two sidecar endpoints in the node's `sidecar_addresses` and warm the replacement before stopping the old
process. The node stays on its active endpoint until failure, then tries alternatives. Each attempt has its own
`client_timeout`; the last successful snapshot remains usable only within `price_ttl`. Continuity therefore depends on
replacement readiness, feed coverage, and the failover sweep completing before the usable snapshot expires.

Changing the node's address list requires a node restart. Existing connections do not acquire new trust roots when a CA
file is replaced. Separate these changes from a sidecar-only replacement when planning availability. A successful rolling
sidecar change can preserve price service without requiring a validator restart, but is not an unconditional zero-gap guarantee.
