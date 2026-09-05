# `ArkApp`

`ArkApp` is an application built using the Cosmos SDK for testing and educational purposes.

## Running testnets with `arkd`

If you want to spin up a quick testnet with your friends, you can follow these steps.
Unless otherwise noted, every step must be done by everyone who wants to participate
in this testnet.

1. From the repository root, run `$ go build -o build/arkd ./cmd/arkd`. This builds the
   `arkd` binary inside the `build` directory. The following instructions are run from inside
   the `build` directory.
2. If you've run `arkd` before, you may need to reset your database before starting a new
   testnet. You can reset your database with the following command: `$ ./arkd comet unsafe-reset-all`.
3. `$ ./arkd init [moniker] --chain-id [chain-id]`. This will initialize a new working directory
   at the default location `~/.ark`. You need to provide a "moniker" and a "chain id". These
   two names can be anything, but you will need to use the same "chain id" in the following steps.
4. `$ ./arkd keys add [key_name]`. This will create a new key, with a name of your choosing.
   Save the output of this command somewhere; you'll need the address generated here later.
5. `$ ./arkd genesis add-genesis-account [key_name] [amount]`, where `key_name` is the same key name as
   before; and `amount` is something like `10000000000000000000000000stake`.
6. `$ ./arkd genesis gentx [key_name] [amount] --chain-id [chain-id]`. This will create the genesis
   transaction for your new chain. Here `amount` should be at least `1000000000stake`. If you
   provide too much or too little, you will encounter an error when starting your node.
7. Now, one person needs to create the genesis file `genesis.json` using the genesis transactions
   from every participant, by gathering all the genesis transactions under `config/gentx` and then
   calling `$ ./arkd genesis collect-gentxs`. This will create a new `genesis.json` file that includes data
   from all the validators (we sometimes call it the "super genesis file" to distinguish it from
   single-validator genesis files).
8. Once you've received the super genesis file, overwrite your original `genesis.json` file with
   the new super `genesis.json`.
9. Modify your `config/config.toml` (in the arkapp working directory) to include the other participants as
   persistent peers:

   ```text
   # Comma separated list of nodes to keep persistent connections to
   persistent_peers = "[validator_address]@[ip_address]:[port],[validator_address]@[ip_address]:[port]"
   ```

   You can find `validator_address` by running `$ ./arkd comet show-node-id`. The output will
   be the hex-encoded `validator_address`. The default `port` is 26656.

10. Now you can start your nodes: `$ ./arkd start`.

Now you have a small testnet that you can use to try out changes to the Cosmos SDK or CometBFT!

## Local network with Docker

`make localnet-start` builds the `ark/arkd` image from the working tree, generates four validator
homes under `.testnets/`, and starts four `arkd` nodes each paired with its own `pricefeed` sidecar.
Node 0 serves RPC on `localhost:26657`, REST on `localhost:1317`, and Prometheus metrics on
`localhost:9464/metrics`; later nodes offset each port by one. `make localnet-liveness` waits until
blocks and an oracle exchange rate appear, and `make localnet-stop` tears everything down.
`VALIDATORS=1 make localnet-start` runs a single validator with one sidecar instead.
`make localnet-runbook` rehearses the emergency submission runbook against the four-validator
localnet: offline multisig ceremony, dark-carrier submission, leak and inclusion checks.
`make upgrade-rehearsal` runs a coordinated upgrade on one host node under cosmovisor, from a binary
built at `OLD_REF` (default `HEAD`) to the working tree. See [contrib/README.md](contrib/README.md)
for the layout.

## Price-feed sidecar

`pricefeed` is the off-chain price process each validator runs beside its node. `arkd` polls it for
prices, and the sidecar reads the feed registry back from the node's gRPC port. Both links are plaintext
by default, which is fine only while both processes share a host. For a sidecar on another machine,
give each link a trust anchor:

- **Node to sidecar.** Start the sidecar with `--tls-cert-file` and `--tls-key-file`, and add
  `--tls-client-ca-file` to admit only nodes that present a certificate signed by that CA. In the node's
  `app.toml`, set `tls_ca_file` under `[pricefeed]` to the CA the sidecar's certificate chains to, with
  `tls_cert_file` and `tls_key_file` when the sidecar requires a client certificate and `tls_server_name`
  when the address is an IP.
- **Sidecar to node.** The node's gRPC port does not terminate TLS, so put a TLS terminator in front of it
  and point `client.tls.caFile` in the sidecar's `config.json` at the terminator's CA; `certFile`,
  `keyFile`, and `serverName` sit beside it. `pricefeed validate` and `pricefeed prices` take the same
  files as `--tls-*` flags, and `validate` takes the chain link's as `--chain-tls-*`.

`client.addresses` in the sidecar's `config.json` lists the chain nodes to query, in preference order and
at most four — a local sentry first, a fallback behind it. The sidecar polls the first that answers and
stays on it until it fails, then sweeps the rest in order under `client.timeout` each. There is no
fail-back, so a flapping preferred node cannot bounce the sidecar between endpoints. All of them are
dialled with the one `client.tls` block, and a failed sweep keeps the last known feed set rather than
emptying it. The chain node that answers is the `address` label on
`ark_pricefeed_chainstate_refreshes_total`.

The sidecar releases on its own cadence under `pricefeed/vX.Y.Z` tags: `make release-pricefeed` builds
one from the tag on `HEAD`, and `make build-pricefeed` builds the binary from the working tree. The
compatibility rule between node and sidecar is stated in `pricefeed/doc.go`.

## Recommended node environment

Set a fixed SDK config scope in the environment of every long-running `arkd` process
(systemd unit, container env, or shell profile):

```text
COSMOS_SDK_CONFIG_SCOPE=arkd
```

The value is arbitrary; it only needs to be non-empty and constant for the process's
lifetime. Without it, cosmos-sdk v0.54's `GetConfig` rebuilds its config-registry key on
every call, which syscalls `os.Hostname` on every Bech32 address parse (measured at
~9µs per transaction in the ante path alone), and a mid-run hostname change — a DHCP
rename, a cloud instance rename — lands the process on a fresh, unsealed config with
the wrong address prefixes. Pinning the scope removes both. The setting is per-process,
harmless to set host-wide, and freely reversible; drop it once the SDK caches the
fallback key upstream.

Run `arkd` under cosmovisor with binary downloads disabled:

```text
DAEMON_ALLOW_DOWNLOAD_BINARIES=false
```

The security committee can schedule an upgrade without a governance vote, and an
upgrade plan's `info` field names a binary. With downloads enabled, cosmovisor would
install and run that binary at the upgrade height on every node that trusts it; with
them disabled, the upgrade halts until you have placed a binary you built or verified
yourself. The setting is what keeps the committee's power to *schedule* an upgrade from
becoming the power to choose what your node runs. See the committee-upgrades section of
`docs/EMERGENCY_SUBMISSION_RUNBOOK.md`.

NOTE: Sometimes creating the network through the `collect-gentxs` will fail, and validators will start
in a funny state (and then panic). If this happens, you can try to create and start the network first
with a single validator and then add additional validators using a `create-validator` transaction.
