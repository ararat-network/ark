# Release runbook

For maintainers cutting a chain (`vX.Y.Z`) or sidecar (`pricefeed/vX.Y.Z`) release: dispatching the signed release
workflow, reviewing and publishing its draft, image publication, failure handling, and local packaging.
[Release verification](../operations/RELEASE_VERIFICATION.md) owns the checks consumers run on what this publishes;
[third-party notices](../../THIRD_PARTY_NOTICES.md#maintainer-release-obligations) own licence and corresponding-source
obligations.

## Prerequisites

- The repository is public. The [release workflow](../../.github/workflows/release.yml) and the
  [image workflow](../../.github/workflows/docker-push.yml) run only from a public repository, since GitHub
  attestations are unavailable to private repositories on the Free plan. The GHCR package's visibility is set
  separately from the repository's.
- CI has passed on the intended `main` revision, and it was reviewed before tagging.
- The tag is `vX.Y.Z` or `pricefeed/vX.Y.Z`, optionally with a SemVer prerelease suffix such as `-rc.1`. It already
  exists remotely, includes the release workflow, and points to a commit reachable from `main`.
- The release workflow exists on the default branch, for manual dispatch.

No manual signing key or personal email identity is required.

## Cut a standalone release

Invoke the workflow against the tag itself:

```bash
gh workflow run release.yml --repo ararat-network/ark --ref vX.Y.Z
# Independent sidecar release:
gh workflow run release.yml --repo ararat-network/ark --ref pricefeed/vX.Y.Z
```

The workflow selects that exact tag and runs its [local packaging](#local-packaging) target. Build/source
verification, per-archive SPDX generation and checksums complete before it stages assets for signing. The node builds
Linux amd64/arm64; the sidecar also builds macOS amd64/arm64.

It creates one signed provenance bundle covering every binary archive, SPDX file, checksum file and application/runtime
source or notice asset. It uploads a draft, downloads the complete asset set, verifies every attestation and checksum,
and compares the downloads with the staged bytes. It leaves the draft unpublished. It refuses an existing draft or
published release rather than replacing its assets.

## Review and publish

After a successful run, review the draft's tag, source revision, CI, notices, source assets and verification result.
QEMU is build support for the arm64 node, not native-hardware runtime validation. Require runtime smoke tests on both
native architectures separately before recommending the node binaries.

Publish through GitHub Releases using your account; that user-generated publication event triggers the
[image workflow](#images) for chain tags. Do not mark sidecar releases latest. Mark a stable chain release latest only
when it is the intended recommended version. Do not replace assets after review; consumers must still verify the files
they actually download.

Every chain tag also pushes the proto module to the Buf Schema Registry ([consumers](../../proto/README.md#consumers)).
The downstream pins do not move by themselves: after tagging, open one pull request in
[ark-registry](https://github.com/ararat-network/ark-registry) moving `ARK_VERSION` to the tag and regenerating, and
one in [ark-sdk](https://github.com/ararat-network/ark-sdk) moving its proto and fixture pins. Each repository's check
refuses a pin whose files do not match the tag, and the registry's weekly check fails when a newer tag would change
its files, so a forgotten bump shows within a week.

## Images

The [image workflow](../../.github/workflows/docker-push.yml) publishes on every published chain release, nightly when
`main` has moved, and on manual dispatch; it skips sidecar releases. It builds from the verified committed source export
and uploads the image by digest, with explicit BuildKit provenance. Pinned Syft scans that registry digest; GitHub
Actions signs provenance and SBOM claims and stores them in GHCR. Registry verification must pass before the workflow
moves its release, branch or nightly tags. The tag-promotion result must retain the verified digest.

## Failure handling

A failed upload or check can leave an incomplete standalone draft marked verification pending; inspect it before
removing it and retrying. Do not publish a failed run's draft.

An image failure before tag promotion leaves existing tags unchanged, but the uploaded digest and any completed
attestations can remain accessible in the registry. Promotion of multiple tags is not atomic; a registry failure can
leave only some tags updated. Treat the run as failed, inspect the tag digests, and rerun from the intended revision.
Do not bypass attestation verification to compensate for a failed run or an older unsigned image.

## First release

The native source rebuilds are substantial. Rehearse the complete release workflow with a prerelease tag before the
first supported release; available runner disk, memory and the six-hour job limit may require adjustment. Validate one
complete image run before relying on that path. Public attestations record workflow and source metadata; review that
metadata during the first public run.

## Local packaging

`make release` and `make release-pricefeed` package locally without uploading or signing. Each checks a clean tree and
its tag on HEAD, and selects that tag explicitly. They need Python 3.12+, Docker able to run both Linux architectures
(natively or through emulation), and the
[source-packaging prerequisites](../../THIRD_PARTY_NOTICES.md#preparing-and-checking-a-source-bundle).

The [chain configuration](../../.goreleaser.yml) builds static Linux binaries for amd64 and arm64. GoReleaser passes its
build command to [the release adapter](../../contrib/scripts/release-build.py), which builds the committed source export
with each architecture's compiler in the pinned base from the
[image Dockerfile](../../contrib/images/arkd-env/Dockerfile) and verifies the binary against its runtime sources before
returning it to GoReleaser; [third-party notices](../../THIRD_PARTY_NOTICES.md#standalone-node-runtime-sources)
describe what it packages and checks. macOS node use remains a source build.

The sidecar target skips GoReleaser's bare-semver validation, which the `pricefeed/` tag prefix would fail. The
[sidecar configuration](../../.goreleaser.pricefeed.yml) derives version strings from `.Tag` and cross-compiles without
cgo.

Both configurations prepare a corresponding source bundle, verify builds from it, and generate per-archive SPDX
inventories with pinned Syft. Checksums cover the archives, SPDX files and every attached source/notice asset.

## References

- [Docker image export by digest](https://docs.docker.com/build/exporters/image-registry/)
- [Docker manifest promotion](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/)
