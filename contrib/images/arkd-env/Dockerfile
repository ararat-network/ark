# Static musl builds of arkd and pricefeed on Alpine. Built by
# `make -C contrib/images arkd-env` (or `make localnet-build-env`), which
# passes the version stamps so .git can stay out of the build context.

# Base images are pinned by digest; the tag beside each digest names what
# the digest was resolved from. Dependabot bumps the pair together.
FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder-base
WORKDIR /work
# Record actual static-library owners; installed build tools alone do not prove linkage.
RUN apk add --no-cache build-base git file linux-headers eudev-dev python3

# libwasmvm static archives. wasmvm's muslc link directive resolves
# -lwasmvm_muslc.<arch> from the default library path, so the files keep
# their release names. Bump the three ARGs together with the go.mod
# requirement; the grep below fails the build when they drift, and
# contrib/scripts/check-wasmvm-libs.sh holds release builds to the same ARGs.
ARG WASMVM_VERSION=v3.0.7
ARG WASMVM_SHA256_AARCH64=ef3e3125e1ce588a9bc698f695a1a4793432b9903a0928018c0d7872e874cc97
ARG WASMVM_SHA256_X86_64=49ecd70da281b6ee08b31770a54bb529d1de96b3e54013e025df9f0c39fff7f4
ADD https://github.com/CosmWasm/wasmvm/releases/download/${WASMVM_VERSION}/libwasmvm_muslc.aarch64.a /lib/libwasmvm_muslc.aarch64.a
ADD https://github.com/CosmWasm/wasmvm/releases/download/${WASMVM_VERSION}/libwasmvm_muslc.x86_64.a /lib/libwasmvm_muslc.x86_64.a
RUN echo "${WASMVM_SHA256_AARCH64}  /lib/libwasmvm_muslc.aarch64.a" | sha256sum -c - \
 && echo "${WASMVM_SHA256_X86_64}  /lib/libwasmvm_muslc.x86_64.a" | sha256sum -c -

COPY go.mod go.sum ./
RUN grep -q "github.com/CosmWasm/wasmvm/v3 ${WASMVM_VERSION}$" go.mod \
 || { echo "go.mod requires a different wasmvm than WASMVM_VERSION=${WASMVM_VERSION}; update the ARGs" >&2; exit 1; }
RUN go mod download

COPY . .

FROM builder-base AS builder
ARG VERSION
ARG PRICEFEED_VERSION
ARG COMMIT
# Same tags and link flags as the release builds in .goreleaser.yml and
# .goreleaser.pricefeed.yml.
RUN mkdir -p build \
 && BUILD_TAGS=netgo,ledger,muslc \
    LDFLAGS='-w -s -linkmode=external -extldflags "-Wl,-z,muslsymversions -Wl,-Map,/work/build/arkd.link-map -static"' \
    make build VERSION="${VERSION}" COMMIT="${COMMIT}"
RUN file build/arkd | grep -q "statically linked"
RUN python3 contrib/scripts/package-os-source.py link-inputs \
    --installed /lib/apk/db/installed --map build/arkd.link-map --output build/static-runtime.json
RUN LDFLAGS='-w -s' make build-pricefeed PRICEFEED_VERSION="${PRICEFEED_VERSION}" COMMIT="${COMMIT}"

FROM builder-base AS release-builder
RUN contrib/scripts/check-wasmvm-libs.sh \
 && python3 contrib/scripts/release-build.py --inside

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime
RUN apk add --no-cache ca-certificates curl jq \
 && addgroup -g 1025 nonroot \
 && adduser -D -u 1025 -G nonroot nonroot
COPY --from=builder /work/build/arkd /work/build/pricefeed /usr/local/bin/
COPY --chmod=755 contrib/images/arkd-env/wrapper.sh /usr/local/bin/wrapper.sh

# Pins the SDK config-registry key; see docs/operations/NODE_OPERATIONS.md
# "Configuration and process environment".
ENV COSMOS_SDK_CONFIG_SCOPE=arkd
EXPOSE 26656 26657 1317 9090 9464 8080
STOPSIGNAL SIGTERM
VOLUME /data
WORKDIR /data
USER nonroot
ENTRYPOINT ["/usr/local/bin/wrapper.sh"]
CMD ["start"]

# Collect against the exact runtime and builder outputs, never a second apk resolution.
# Recipe execution has no host mounts, build secrets, or root privileges.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS source-tools
RUN apk add --no-cache python3 git abuild xz zstd bzip2 curl spdx-licenses-text \
 && adduser -D collector && mkdir /output && chown collector /output
COPY contrib/scripts/package-os-source.py /collector.py
USER collector

FROM source-tools AS os-source
COPY --from=runtime /lib/apk/db/installed /inputs/runtime-installed
COPY --from=builder /work/build/static-runtime.json /inputs/static-runtime.json
RUN python3 /collector.py collect --runtime /inputs/runtime-installed \
    --linked /inputs/static-runtime.json --output /output

FROM source-tools AS release-runtime-source
COPY --from=release-builder /work/build/static-runtime.json /inputs/static-runtime.json
RUN python3 /collector.py collect --static-only \
    --linked /inputs/static-runtime.json --output /output

# GoReleaser exports these files locally; this target is never published as an image.
FROM scratch AS release-export
COPY --from=release-builder /work/build/release/arkd /arkd
COPY --from=release-builder /work/build/BUILD.json /work/build/static-runtime.json /
COPY --from=release-runtime-source /output/ /runtime-source/

# Only this target is published. Its context is the committed source export
# prepared and verified by package-source.py, including nightly/manual builds.
FROM runtime AS distribution
COPY LICENSE COPYING NOTICE THIRD_PARTY_NOTICES.md /usr/share/ark/
COPY corresponding-source.tar.gz third-party-notices.tar.gz /usr/share/ark/
COPY --from=os-source /output/ /usr/share/ark/os/
LABEL org.opencontainers.image.licenses="GPL-3.0-only"

# Keep localnet builds usable with uncommitted working-tree changes. This target
# is for local development; the publishing workflow explicitly uses distribution.
FROM runtime AS local
