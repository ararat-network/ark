# Static musl builds of arkd and pricefeed on Alpine. Built by
# `make -C contrib/images arkd-env` (or `make localnet-build-env`), which
# passes the version stamps so .git can stay out of the build context.

FROM golang:1.27-alpine AS builder
WORKDIR /work
# eudev-dev supplies the static libudev the ledger tag's HID library links.
RUN apk add --no-cache build-base git file linux-headers eudev-dev

# libwasmvm static archives. wasmvm's muslc link directive resolves
# -lwasmvm_muslc.<arch> from the default library path, so the files keep
# their release names. Bump the three ARGs together with the go.mod
# requirement; the grep below fails the build when they drift.
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
ARG VERSION
ARG COMMIT
# Same tags and link flags as the release builds in .goreleaser.yml.
RUN BUILD_TAGS=netgo,ledger,muslc \
    LDFLAGS='-w -s -linkmode=external -extldflags "-Wl,-z,muslsymversions -static"' \
    make build VERSION="${VERSION}" COMMIT="${COMMIT}"
RUN file build/arkd | grep -q "statically linked"
RUN CGO_ENABLED=0 go build -trimpath -mod=readonly -tags netgo \
    -ldflags "-w -s -X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} -X github.com/cosmos/cosmos-sdk/version.Commit=${COMMIT}" \
    -o build/pricefeed ./cmd/pricefeed

FROM alpine:3.22
RUN apk add --no-cache ca-certificates curl jq \
 && addgroup -g 1025 nonroot \
 && adduser -D -u 1025 -G nonroot nonroot
COPY --from=builder /work/build/arkd /work/build/pricefeed /usr/local/bin/
COPY --chmod=755 contrib/images/arkd-env/wrapper.sh /usr/local/bin/wrapper.sh

# Pins the SDK config-registry key; see README "Recommended node environment".
ENV COSMOS_SDK_CONFIG_SCOPE=arkd
EXPOSE 26656 26657 1317 9090 9464 8080
STOPSIGNAL SIGTERM
VOLUME /data
WORKDIR /data
USER nonroot
ENTRYPOINT ["/usr/local/bin/wrapper.sh"]
CMD ["start"]
