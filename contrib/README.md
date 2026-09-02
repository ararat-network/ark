# contrib

Operational tooling that is neither the chain binary nor its tests. Build
tooling stays in `proto/scripts`; Go code stays in the module tree.

- `images/` — Dockerfiles. `make -C contrib/images arkd-env` builds `ark/arkd`:
  static musl `arkd` and `pricefeed` binaries on Alpine, running as `nonroot`
  (uid 1025) with `COSMOS_SDK_CONFIG_SCOPE` pinned.
- `localnet/` — the Docker Compose localnet: four validators, each paired
  with its own price-feed sidecar, plus the init script that generates their
  homes under `.testnets/`. Driven by `make localnet-start`, `localnet-stop`,
  and `localnet-liveness`. Node N exposes RPC on `26657+10N`, REST on
  `1317+N`, gRPC on `9090+N`, and Prometheus metrics on `9464+N`. The init
  script also seats a 3-of-4 multisig emergency committee in the asset
  module's mandate, keyring under `.testnets/committee`, and makes the last
  validator a dark carrier (`broadcast = false`) so the runbook below has a
  private submission path and public nodes to check for leaks. node0 also
  keeps snapshots every 20 blocks, and `sync0`, behind the `statesync`
  profile, is a non-validator that bootstraps from them: start it with
  `docker compose -f contrib/localnet/docker-compose.yml --profile multi
  --profile statesync up -d`, then check `/status` on `26697`. A node
  that really state-synced reports an `earliest_block_height` well above 1;
  one that replayed from genesis reports 1. `make localnet-stop` removes it
  with the rest.
- `scripts/` — probes and drivers run against a live network.
  `localnet-liveness.sh` polls a node until it has passed a block height and
  the oracle holds an exchange rate. `runbook-emergency-suspend.sh` (driven
  by `make localnet-runbook`) rehearses `docs/EMERGENCY_SUBMISSION_RUNBOOK.md`
  against the localnet: offline 3-of-4 signing ceremony, sub-floor fee
  rejected at CheckTx, dry-run and broadcast through the dark carrier, no
  leak to public mempools, inclusion at the top of a carrier-proposed block
  with `EventEmergencySuspended`, asset suspended. `upgrade-rehearsal.sh` (driven by
  `make upgrade-rehearsal`) runs a coordinated upgrade on one node under
  cosmovisor: old binary from a git ref, proposal, halt, switch to the new
  binary; `upgrade-probe.sh` is the assertion it ends with, and stands alone
  for CI.

`.testnets/` is bind-mounted into containers running as uid 1025.
`make localnet-init` makes the directory world-writable and wipes the
previous run from inside a container, so a Linux host needs no manual
ownership fixes. Docker Desktop maps ownership for you anyway.

CI: `.github/workflows/localnet.yml` builds the image, runs the localnet,
and asserts liveness and the runbook on every pull request;
`docker-push.yml` publishes the image to ghcr on releases and nightly.

Planned addition: `audits/` for third-party reports.
