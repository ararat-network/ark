#!/bin/sh
# Build from a source bundle with an empty dependency cache and networking disabled.
set -eu

prepare() {
    platform=${1:-$(docker info --format '{{.Architecture}}')}
    case "$platform" in aarch64|arm64) platform=arm64 ;; x86_64|amd64) platform=amd64 ;; *) exit 1 ;; esac
    image="ark-source-verifier:$platform"
    # General-purpose build tools are prerequisites, not part of the source bundle.
    docker build --platform "linux/$platform" -t "$image" - >&2 <<'DOCKERFILE'
FROM rust:1.82.0-alpine@sha256:2f42ce0d00c0b14f7fd84453cdc93ff5efec5da7ce03ead6e0b41adb1fbe834e AS rust
FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125
RUN apk add --no-cache build-base git linux-headers eudev-dev clang22-static llvm22-dev python3 perl bash cmake nasm
# Match wasmvm v3.0.7's builders/Dockerfile.alpine. Newer Rust releases removed
# __rust_probestack, which its Wasmer version still requires on x86_64.
COPY --from=rust /usr/local/cargo /usr/local/cargo
COPY --from=rust /usr/local/rustup /usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo RUSTUP_HOME=/usr/local/rustup
ENV PATH=/usr/local/cargo/bin:$PATH
ENV LLVM_CONFIG_PATH=/usr/lib/llvm22/bin/llvm-config
DOCKERFILE
    printf '%s\n' "$image"
}

if [ "${1:-}" = --prepare ]; then
    prepare "${2:-$(docker info --format '{{.Architecture}}')}"
    exit
fi

if [ "${1:-}" = --inside ]; then
    bundle=$2
    kind=$3
    arch=$4
    cd "$bundle/ark"
    export GOPROXY="file://$bundle/go-proxy" GOSUMDB=off GOTOOLCHAIN=local
    export GOENV=off GOWORK=off GOFLAGS=-mod=readonly GOVCS='*:off'
    export GOMODCACHE=/verify-modules GOCACHE=/verify-cache
    revision=$(python3 -c 'import json; print(json.load(open("../SOURCE-MANIFEST.json"))["revision"])')
    # Module resolution is restricted to the bundled file proxy. Downloads from it
    # still verify against go.sum; only the external checksum server is disabled.
    if [ "$kind" != pricefeed ]; then
        wasm_version=$(go list -m -f '{{.Version}}' github.com/CosmWasm/wasmvm/v3)
        herumi_version=$(go list -m -f '{{.Version}}' github.com/herumi/bls-eth-go-binary)
        go mod download "github.com/herumi/bls-eth-go-binary@$herumi_version" "github.com/CosmWasm/wasmvm/v3@$wasm_version"
        case "$arch" in amd64) native_arch=x86_64 ;; arm64) native_arch=aarch64 ;; *) exit 1 ;; esac
        # Rebuild the native libraries, rather than satisfying the check with the
        # precompiled libraries supplied in upstream Go module zips.
        (cd "$bundle/native/wasmvm/libwasmvm" && cargo build --offline --locked --release --example wasmvmstatic)
        cp "$bundle/native/wasmvm/libwasmvm/target/release/examples/libwasmvmstatic.a" "/usr/local/lib/libwasmvm_muslc.$native_arch.a"
        make -C "$bundle/native/herumi" -j2
        herumi_module="$GOMODCACHE/github.com/herumi/bls-eth-go-binary@$herumi_version"
        chmod -R u+w "$herumi_module"
        cp "$bundle/native/herumi/bls/lib/linux/$arch/libbls384_256.a" "$herumi_module/bls/lib/linux/$arch/libbls384_256.a"
        CGO_ENABLED=1 GOOS=linux GOARCH="$arch" go build -trimpath -tags netgo,ledger,muslc \
            -ldflags "-X github.com/cosmos/cosmos-sdk/version.Commit=$revision -linkmode=external -extldflags '-Wl,-z,muslsymversions -static'" \
            -o /verify-arkd ./cmd/arkd
        # Source completeness is a build check. CPU-specific cryptography must
        # be exercised on native hardware, not a foreign-architecture emulator.
    fi
    if [ "$kind" = pricefeed ] || [ "$kind" = image ]; then
        for os in linux darwin; do
            CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -tags netgo \
                -ldflags "-X github.com/cosmos/cosmos-sdk/version.Commit=$revision" \
                -o "/verify-pricefeed-$os-$arch" ./cmd/pricefeed
        done
    fi
    exit
fi

bundle=${1:?usage: verify-source.sh BUNDLE.tar.gz [arkd|pricefeed|image]}
kind=${2:-arkd}
architectures=${3:-all}
case "$architectures" in all) architectures='amd64 arm64' ;; amd64|arm64) ;; *) echo 'invalid architecture' >&2; exit 1 ;; esac
case "$kind" in arkd|pricefeed|image) ;; *) echo 'invalid bundle kind' >&2; exit 1 ;; esac
bundle=$(realpath "$bundle")
# Protect against stale files or a mix-up between a node and sidecar source bundle.
python3 - "$bundle" "$kind" <<'PY'
import hashlib, json, pathlib, sys, tarfile
p = pathlib.Path(sys.argv[1])
expected = pathlib.Path(str(p) + '.sha256').read_text().split()[0]
with p.open('rb') as f:
    if hashlib.file_digest(f, 'sha256').hexdigest() != expected:
        raise SystemExit('source bundle checksum mismatch')
with tarfile.open(p) as t:
    manifest = json.load(t.extractfile('SOURCE-MANIFEST.json'))
    if manifest['kind'] != sys.argv[2]:
        raise SystemExit('source bundle kind mismatch')
    for m in t:
        name = pathlib.PurePosixPath(m.name)
        if name.is_absolute() or '..' in name.parts:
            raise SystemExit('unsafe archive path')
        if not (m.isfile() or m.isdir()):
            raise SystemExit('source archive must contain only files and directories')
PY
for arch in $architectures; do
    runtime_arch=$arch
    if [ "$kind" = pricefeed ]; then
        # Pure Go cross-compilation needs no CPU emulation or second toolchain.
        runtime_arch=$(docker info --format '{{.Architecture}}')
        case "$runtime_arch" in aarch64) runtime_arch=arm64 ;; x86_64) runtime_arch=amd64 ;; esac
    fi
    image=$(prepare "$runtime_arch")
    # Stream input instead of mounting host paths: this also works when the release
    # command itself is inside a container connected to the host Docker daemon.
    docker run --rm -i --network none --platform "linux/$runtime_arch" "$image" \
        sh -c 'mkdir /verify; tar -xzf - -C /verify; exec sh /verify/ark/contrib/scripts/verify-source.sh --inside /verify "$1" "$2"' \
        sh "$kind" "$arch" < "$bundle"
done
printf '%s\n' "Source verification passed: $bundle"
