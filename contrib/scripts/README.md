# Probes and rehearsals

Run host commands from the repository root. Host probes use `curl` and `jq`. These scripts report non-zero status on
failed assertions.

| Script | Inputs / entry point | Effect and success condition |
| --- | --- | --- |
| [localnet-liveness.sh](localnet-liveness.sh) | `<iterations> <sleep-seconds> <num-blocks> <rpc-url> <api-url>`; `make localnet-liveness` | Read-only polling; height exceeds the requested bound and at least one oracle rate exists. |
| [localnet-statesync.sh](localnet-statesync.sh) | `<iterations> <sleep-seconds> <rpc-url>`; `make localnet-statesync` starts its node first | Read-only polling; a snapshot was restored and the node committed beyond it. |
| [localnet-observability.sh](localnet-observability.sh) | `<iterations> <sleep-seconds> <prometheus-url>`; `make localnet-observability` starts the profile first | Read-only polling; Prometheus has loaded the rules and scraped node0. |
| [upgrade-probe.sh](upgrade-probe.sh) | `<iterations> <sleep-seconds> <upgrade-height> <plan-name> <rpc-url> <api-url>` | Read-only polling; the plan is applied and blocks progress beyond it. Passing the scheduled height without applying fails. |
| [upgrade-rehearsal.sh](upgrade-rehearsal.sh) | `make upgrade-rehearsal` | Builds/selects old and new binaries, starts a temporary cosmovisor node, submits an upgrade, observes halt/switch, and runs the upgrade probe. |
| [check-wasmvm-libs.sh](check-wasmvm-libs.sh) | Run by the [image Dockerfile](../images/arkd-env/Dockerfile); `WASMVM_LIBDIR` overrides `/lib` | Read-only; go.mod's wasmvm version matches the image Dockerfile ARGs and both static archives match its digests. |
| [release-build.py](release-build.py) | GoReleaser node build tool; `ARK_RELEASE_SOURCE` names the verified application source bundle | Builds the committed export in the requested Linux platform's container, verifies binary/runtime source identity, and writes GoReleaser's binary plus runtime source assets under `build/source/`. Does not publish. |
| [package-source.py](package-source.py) | `make source-bundle`; `SOURCE_KIND` selects `arkd` or `pricefeed` | Exports the committed revision and its dependency sources as the release's corresponding-source bundle under `build/source/`. |
| [package-os-source.py](package-os-source.py) | Run by the image Dockerfile in an unprivileged build stage | Collects the exact Alpine package recipes and archives the runtime image was built from. |
| [verify-source.sh](verify-source.sh) | `make verify-source`; the bundle path and kind | Builds from the bundle with an empty dependency cache and networking disabled; a successful build is the check. |

[vulncheck.py](vulncheck.py) is `make vulncheck`: the govulncheck gate described in
[tests/README.md](../../tests/README.md#static-analysis-boundaries).

[release-assets.py](release-assets.py) stages the exact binary/source/SBOM asset matrix and checks
its checksums before signing and after upload. It refuses missing, extra, tampered or symlinked
assets and never publishes. The [release runbook](../../docs/releases/RELEASE_RUNBOOK.md) owns the
signed-draft workflow; [release verification](../../docs/operations/RELEASE_VERIFICATION.md) owns
consumer checks.

[Licensing and corresponding source](../../THIRD_PARTY_NOTICES.md) owns release source
coverage, verification, download and retention procedures.

## Upgrade rehearsal

The new binary must register the intended plan through [app/upgrade](../../app/upgrade/README.md). There is no default
versioned handler when that tree contains only `template/`.

| Environment | Meaning |
| --- | --- |
| `UPGRADE_NAME` | Registered plan name; otherwise inferred from the newest `app/upgrade/v*` directory. |
| `OLD_REF` | Git ref used to build the old binary; defaults to the newest `v*` release tag without `app/upgrade/<UPGRADE_NAME>`, or HEAD while the plan is uncommitted. |
| `GENESIS` | Curated genesis the old binary generates the node from; defaults to `OLD_REF`'s `app/genesis/testnet.json`. Without one, or for an old binary lacking `--genesis`, the old binary generates from its defaults. |
| `OLD_BINARY`, `NEW_BINARY` | Supply binaries instead of building the corresponding version. |
| `UPGRADE_DELAY` | Blocks from proposal to scheduled upgrade. |
| `COSMOVISOR_VERSION` | Cosmovisor release installed into the tooling location when needed; defaults to `v1.7.3`. |
| `WORK` | Scratch homes, binaries and logs; defaults to `build/upgrade-rehearsal`. Use a disposable path. |

The script uses the default node ports and refuses to proceed if another node answers there. Stop the Docker localnet
before running it. Read the script before selecting a reused `WORK` directory: it manages its own scratch state and
preserves outputs for inspection. It installs cosmovisor into its tooling location when needed.
The final applied-plan query is authoritative for the rehearsal; a version string can be identical for two uncommitted builds.

[Node operations](../../docs/operations/NODE_OPERATIONS.md) owns production procedures; [tests](../../tests/README.md)
maps the CI and in-memory verification layers.
