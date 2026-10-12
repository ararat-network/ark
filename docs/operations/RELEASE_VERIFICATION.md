# Release verification

Use this procedure before running an Ark image or installing a standalone binary
produced by the signed release workflows; an artifact without attestations fails it.
The [release runbook](../releases/RELEASE_RUNBOOK.md) owns how releases are cut and
published. [Third-party notices](../../THIRD_PARTY_NOTICES.md)
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

If either command fails, do not run the image, and do not fall back to an older
unsigned one. A failed publication run can leave tags partially promoted; use only
the verified digest.

To inspect the verified SBOM, repeat the second command with `--format json`.
The SPDX document is under `verificationResult.statement.predicate`. It is a
scanner inventory of the shipped distribution image, including its bundled source
material where detected.

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
release assets rather than separate SPDX-predicate attestations, and come from
scanning each distribution archive.

To check a complete downloaded release, first verify its `SHA256SUMS-*.txt` file with
the command above. Then run `sha256sum --check THE_VERIFIED_CHECKSUM_FILE` from the
asset directory (`shasum -a 256 --check` on macOS).

## What a pass proves

An attestation is evidence of origin and integrity: the artifact was signed by Ark's
release or image workflow on a GitHub-hosted runner, from the selected commit and ref.
It is not a security audit or proof of reproducibility. A checksum check by itself
does not establish the publisher's identity; it extends a verified checksum file to
the files it lists.

The SPDX inventories do not guarantee that every statically linked native component
was discovered. Keep the source manifests with the artifact when investigating
dependencies: those inside `/usr/share/ark/` for an image, and the application and
per-architecture runtime sources for a standalone node archive, as the
[third-party notices](../../THIRD_PARTY_NOTICES.md#standalone-node-runtime-sources)
describe.

## References

- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
- [GitHub CLI verification flags](https://cli.github.com/manual/gh_attestation_verify)
