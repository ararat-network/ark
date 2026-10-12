# Docker localnet

[docker-compose.yml](docker-compose.yml) runs one or four validators, each with its own `pricefeed` sidecar.
[init.sh](init.sh) generates their homes and sidecar files. Run Make targets from the repository root with Docker/Compose
available; host probes also require `curl` and `jq`.

## Start, inspect, stop

```sh
make localnet-start
make localnet-liveness
make localnet-stop
```

The default is four validators. `VALIDATORS=1 make localnet-start` selects the single-validator shape; other counts are
unsupported by the fixed Compose services. `localnet-start` stops the old network, builds the image, initialises homes,
and starts the chosen services. It wipes the previous contents of the localnet data directory. `localnet-up` starts
existing homes without reinitialising them; `localnet-stop` stops all profiles without itself regenerating data.

Homes live under `.testnets/` by default. `ARK_LOCALNET_DATA` selects another disposable directory; use the same value for
subsequent targets. Init makes that directory writable for the image's uid 1025 and clears it from inside the container.
The network runs the testnet artefact, [app/genesis/testnet.json](../../app/genesis/testnet.json), with every validator
granted a seat from its community pool ([genesis §15](../../docs/governance/GENESIS.md#15-testnet-artefact)); the
generated keys are development fixtures.

## Topology and ports

For validator N, the host port map is:

| Surface | Host port |
| --- | --- |
| RPC | `26657 + 10N` |
| REST | `1317 + N` |
| gRPC | `9090 + N` |
| Application metrics | `9464 + N` |
| CometBFT metrics | `26660 + 10N` |

Node 0 therefore serves RPC at `localhost:26657`, REST at `localhost:1317`, application metrics at
`localhost:9464/metrics`, and CometBFT metrics at `localhost:26660/metrics`. The Compose file is the authority for exact
bindings. Container links explicitly select plaintext transport; [pricefeed operations](../../docs/operations/PRICEFEED_OPERATIONS.md)
explains deployment outside this network.

## State sync

Node 0 keeps snapshots every 20 blocks. On the four-validator network:

```sh
make localnet-statesync
```

The state-sync target starts `sync0` using the `statesync` profile and checks RPC at `localhost:26697`. Its entrypoint,
[statesync.sh](statesync.sh), waits up to `WAIT_SECONDS` (300) for node0 to pass the snapshot interval
(`SNAPSHOT_INTERVAL`, 20), takes the trust height and hash from node0's RPC, writes them and `RPC_SERVERS` into the
`[statesync]` table of config.toml, and starts the node. Success requires
an earliest block above 1 and progress beyond that restored height, not merely a node that caught up by replaying genesis.
`localnet-stop` also stops this profile.

The [script guide](../scripts/README.md) covers the probes and the upgrade rehearsal.

[localnet.yml](../../.github/workflows/localnet.yml) defines the automated localnet checks. The liveness probe requires
both block progress and at least one oracle rate; block production alone can continue while sidecars fail.
