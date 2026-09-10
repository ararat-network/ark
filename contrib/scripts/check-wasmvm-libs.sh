#!/bin/sh
# Check the wasmvm static archives a release links: go.mod must require the version that
# contrib/images/arkd-env/Dockerfile records, and the archives must match its digests. Runs as the
# chain release's goreleaser before hook; WASMVM_LIBDIR overrides /lib.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DOCKERFILE=$ROOT/contrib/images/arkd-env/Dockerfile
LIBDIR=${WASMVM_LIBDIR:-/lib}

arg() {
  sed -n "s/^ARG $1=//p" "$DOCKERFILE"
}

recorded=$(arg WASMVM_VERSION)
required=$(cd "$ROOT" && go list -m -f '{{.Version}}' github.com/CosmWasm/wasmvm/v3)
if [ "$recorded" != "$required" ]; then
  echo "go.mod requires wasmvm $required but $DOCKERFILE records $recorded" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  set -- sha256sum -c -
else
  set -- shasum -a 256 -c -
fi
printf '%s  %s\n%s  %s\n' \
  "$(arg WASMVM_SHA256_X86_64)" "$LIBDIR/libwasmvm_muslc.x86_64.a" \
  "$(arg WASMVM_SHA256_AARCH64)" "$LIBDIR/libwasmvm_muslc.aarch64.a" |
  "$@"
