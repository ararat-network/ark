# Contributor tooling

This tree contains container images, disposable local networks, and operational probes/rehearsals. Go code and its tests
stay with their packages; protobuf build scripts stay under [proto/](../proto/README.md).

| Directory | Purpose |
| --- | --- |
| [images/](images/) | Image Makefile, Dockerfile, and process wrapper. |
| [localnet/](localnet/README.md) | Compose topology, generated homes, ports, and start/stop/state-sync workflow. |
| [scripts/](scripts/README.md) | Liveness probes, emergency submission/saturation drivers, and upgrade rehearsal. |

From the root, `make -C contrib/images arkd-env` builds `ark/arkd` with static musl `arkd` and `pricefeed` binaries on
Alpine. [The Dockerfile](images/arkd-env/Dockerfile) and [wrapper](images/arkd-env/wrapper.sh) define its non-root user
(uid 1025) and process environment. [docker-push.yml](../.github/workflows/docker-push.yml) owns image publication.

Start with the [localnet guide](localnet/README.md) for an end-to-end developer environment. Production operational policy
belongs in [docs/](../docs/README.md); rehearsal scripts exercise those policies using disposable keys, balances, and homes.
