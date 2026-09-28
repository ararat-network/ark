# Release verification

Use this procedure before running an Ark image or installing a standalone binary.
It covers artifacts produced by the signed workflows after this support was added;
older artifacts may lack attestations. [Third-party notices](../../THIRD_PARTY_NOTICES.md)
owns corresponding-source availability and licence instructions.

## Verify an image

Use a current GitHub CLI supporting the flags below, and authenticate it to GitHub.
Authenticate Docker to GHCR if the package requires it. Select the full source commit,
Git ref, and image digest from the intended, reviewed release or successful workflow
run. Do not substitute whichever revision a moving tag happens to resolve to.

The following Bash example requires replacing all three placeholders. A chain
release uses `refs/tags/vX.Y.Z`; a nightly from `main` uses `refs/heads/main`.

```bash
ARK_IMAGE='ghcr.io/ararat-network/ark@sha256:REPLACE_WITH_IMAGE_DIGEST'
ARK_REVISION='REPLACE_WITH_FULL_COMMIT_SHA'
ARK_REF='refs/tags/REPLACE_WITH_RELEASE_TAG'
ARK_POLICY=(
  --repo ararat-network/ark
  --signer-workflow ararat-network/ark/.github/workflows/docker-push.yml
  --source-digest "$ARK_REVISION"
  --source-ref "$ARK_REF"
  --signer-digest "$ARK_REVISION"
  --deny-self-hosted-runners
  --bundle-from-oci
)

gh attestation verify "oci://$ARK_IMAGE" "${ARK_POLICY[@]}"
gh attestation verify "oci://$ARK_IMAGE" "${ARK_POLICY[@]}" \
  --predicate-type https://spdx.dev/Document/v2.3
```

Both commands must succeed. The first checks signed build provenance; the second
checks a signed SPDX dependency inventory for the same image digest. They constrain
the signer to Ark's publishing workflow and the selected source commit and ref.
Pull and run that exact digest after verification, using the
[node operations guide](NODE_OPERATIONS.md) for configuration.

To inspect the verified SBOM, repeat the second command with `--format json`.
The SPDX document is under `verificationResult.statement.predicate`. This is a
scanner inventory of the shipped distribution image, including its bundled source
material where detected. It is not a guarantee that every statically linked native
component was discovered. Keep the application and system-runtime source manifests
inside `/usr/share/ark/` with the image when investigating dependencies. An attestation
is evidence of origin and integrity, not a security audit or proof of reproducibility.

## Publication and failure handling

The workflow builds from the verified committed source export and uploads the image
by digest, with explicit BuildKit provenance. Pinned Syft scans that registry digest;
GitHub Actions signs provenance and SBOM claims and stores them in GHCR. Registry
verification must pass before the workflow moves its release, branch or nightly tags.
The tag-promotion result must retain the verified digest.

The workflow publishes only from a public repository, since GitHub attestations are
unavailable to private repositories on the Free plan. Validate one complete run
before relying on this path; the GHCR package's visibility is set separately from
the repository's.

A failure before tag promotion leaves existing tags unchanged, but the uploaded digest
and any completed attestations can remain accessible in the registry. Promotion of
multiple tags is not atomic; a registry failure can leave only some tags updated.
Treat the run as failed, inspect the tag digests, and rerun from the intended revision.
Consumers should always use the verified digest. Do not bypass attestation verification
to compensate for a failed run or an older unsigned image.

No manual signing key or personal email identity is required. Public attestations
record workflow and source metadata; review that metadata during the first public run.

## Prepare a standalone release

Complete CI and review the intended `main` revision before tagging it. The supported
tag forms are `vX.Y.Z` and `pricefeed/vX.Y.Z`, optionally with a SemVer prerelease
suffix such as `-rc.1`. The tag must already exist remotely, include the release
workflow, and point to a commit reachable from `main`. The workflow must also exist
on the default branch for manual dispatch. Invoke it against the tag itself:

```bash
gh workflow run release.yml --repo ararat-network/ark --ref vX.Y.Z
# Independent sidecar release:
gh workflow run release.yml --repo ararat-network/ark --ref pricefeed/vX.Y.Z
```

These commands start a release build; they are not ordinary verification commands.
The workflow runs only from a public repository: GitHub attestations need one on the Free plan.
`make release` and `make release-pricefeed` only package locally, without signing or
uploading. CI selects its exact tag explicitly. Build/source verification, per-archive
SPDX generation and checksums complete before the workflow stages assets for signing.
The node builds Linux amd64/arm64; the sidecar also builds macOS amd64/arm64.

The workflow creates one signed provenance bundle covering every binary archive,
SPDX file, checksum file and application/runtime source or notice asset. It uploads
a draft, downloads the complete asset set, verifies every attestation and checksum,
and compares the downloads with the staged bytes. It leaves the draft unpublished.
It refuses an existing draft or published release rather than replacing its assets.
A failed upload or check can leave an incomplete draft marked verification pending;
inspect it before removing it and retrying. Do not publish a failed run's draft.

The native source rebuilds are substantial. Rehearse the complete workflow with a
prerelease tag before the first supported release; available runner disk, memory and
the six-hour job limit may require adjustment. QEMU is build support for the arm64
node, not native-hardware runtime validation. Require runtime smoke tests on both
native architectures separately before recommending the node binaries.

After a successful run, review the draft's tag, source revision, CI, notices, source
assets and verification result. Publish through GitHub Releases using your account;
that user-generated publication event triggers the existing Docker workflow for
chain tags. Do not mark sidecar releases latest. Mark a stable chain release latest
only when it is the intended recommended version. Do not replace assets after review;
consumers must still verify the files they actually download.

## Verify a standalone download

Download the desired asset and `provenance.sigstore.json` from the same release.
Choose the full commit and tag from the reviewed release independently of the bundle.
Replace these Bash placeholders and verify before extracting or running the binary:

```bash
ARK_ASSET='REPLACE_WITH_DOWNLOADED_ARCHIVE'
ARK_REVISION='REPLACE_WITH_FULL_COMMIT_SHA'
ARK_REF='refs/tags/REPLACE_WITH_RELEASE_TAG'
ARK_POLICY=(
  --repo ararat-network/ark
  --signer-workflow ararat-network/ark/.github/workflows/release.yml
  --source-digest "$ARK_REVISION"
  --source-ref "$ARK_REF"
  --signer-digest "$ARK_REVISION"
  --deny-self-hosted-runners
  --bundle provenance.sigstore.json
)
gh attestation verify "$ARK_ASSET" "${ARK_POLICY[@]}"
```

Use the same command on the matching `.spdx.json` file, the checksum file and each
source/notice asset you download. The bundle is untrusted input until verification
succeeds. Each file has a signed build-provenance subject; SPDX files are signed as
release assets rather than separate SPDX-predicate attestations. These inventories
come from scanning each distribution archive and do not guarantee complete discovery
of static native components; retain the matching source manifests as well.

To check a complete downloaded release, first verify its `SHA256SUMS-*.txt` file with
the command above. Then run `sha256sum --check THE_VERIFIED_CHECKSUM_FILE` from the
asset directory (`shasum -a 256 --check` on macOS). A checksum check by itself does not
establish the publisher's identity. Keep both application and per-architecture runtime
sources with standalone node archives as described in the third-party notices.

## References

- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
- [GitHub CLI verification flags](https://cli.github.com/manual/gh_attestation_verify)
- [Docker image export by digest](https://docs.docker.com/build/exporters/image-registry/)
- [Docker manifest promotion](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/)
