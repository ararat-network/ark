# Probes and rehearsals

Run host commands from the repository root. Host probes use `curl` and `jq`; the Compose rehearsal service supplies
`arkd` and its other utilities inside the image. These scripts report non-zero status on failed assertions.

| Script | Inputs / entry point | Effect and success condition |
| --- | --- | --- |
| [localnet-liveness.sh](localnet-liveness.sh) | `<iterations> <sleep-seconds> <num-blocks> <rpc-url> <api-url>`; `make localnet-liveness` | Read-only polling; height exceeds the requested bound and at least one oracle rate exists. |
| [localnet-statesync.sh](localnet-statesync.sh) | `<iterations> <sleep-seconds> <rpc-url>`; `make localnet-statesync` starts its node first | Read-only polling; a snapshot was restored and the node committed beyond it. |
| [upgrade-probe.sh](upgrade-probe.sh) | `<iterations> <sleep-seconds> <upgrade-height> <plan-name> <rpc-url> <api-url>` | Read-only polling; the plan is applied and blocks progress beyond it. Passing the scheduled height without applying fails. |
| [runbook-emergency-suspend.sh](runbook-emergency-suspend.sh) | `make localnet-runbook` on the four-validator shape | Signs/broadcasts a real fixture emergency transaction and suspends an asset; checks sub-floor rejection, isolation, inclusion, and event/state. |
| [localnet-fill-mempool.sh](localnet-fill-mempool.sh) | Internal helper of saturation rehearsal | Submits fee-paying normal transactions while consensus is held; checks admission overflow and empty carrier storage. |
| [localnet-saturation.sh](localnet-saturation.sh) | `make localnet-saturation` | Pauses node1/node2, fills the public pool, submits privately, resumes consensus, and checks inclusion. |
| [upgrade-rehearsal.sh](upgrade-rehearsal.sh) | `make upgrade-rehearsal` | Builds/selects old and new binaries, starts a temporary cosmovisor node, submits an upgrade, observes halt/switch, and runs the upgrade probe. |

## Emergency and saturation fixtures

Use the [Docker localnet](../localnet/README.md). The runbook driver reads keys from the generated committee home and
keeps ceremony files there. `DENOM` chooses an asset; otherwise a rerun selects an eligible active asset. `CARRIER`,
`PUBLIC_NODES`, `GAS`, and `INCLUSION_POLLS` customise the driver; inspect its header for exact defaults.

Saturation requires a disposable four-validator network initialised with `PUBLIC_MEMPOOL_SIZE=32`. The fill helper rejects
large pool sizes to bound its work. The parent script restores paused validators in cleanup. This tests isolation and
admission under a full public pool; it is not a throughput benchmark, inclusion-time guarantee, or sustained-spam proof.

## Upgrade rehearsal

The new binary must register the intended plan through [app/upgrade](../../app/upgrade/README.md). There is no default
versioned handler when that tree contains only `template/`.

| Environment | Meaning |
| --- | --- |
| `UPGRADE_NAME` | Registered plan name; otherwise inferred from the newest `app/upgrade/v*` directory. |
| `OLD_REF` | Git ref used to build the old binary; defaults to HEAD. |
| `OLD_BINARY`, `NEW_BINARY` | Supply binaries instead of building the corresponding version. |
| `UPGRADE_DELAY` | Blocks from proposal to scheduled upgrade. |
| `WORK` | Scratch homes, binaries and logs; defaults to `build/upgrade-rehearsal`. Use a disposable path. |

The script uses the default node ports and refuses to proceed if another node answers there. Stop the Docker localnet
before running it. Read the script before selecting a reused `WORK` directory: it manages its own scratch state and
preserves outputs for inspection. It installs cosmovisor into its tooling location when needed.
The final applied-plan query is authoritative for the rehearsal; a version string can be identical for two uncommitted builds.

[Node operations](../../docs/NODE_OPERATIONS.md) and the [emergency runbook](../../docs/EMERGENCY_SUBMISSION_RUNBOOK.md)
own production procedures; [tests](../../tests/README.md) maps the CI and in-memory verification layers.
