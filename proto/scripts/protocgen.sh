#!/bin/sh

# SPDX-License-Identifier: Apache-2.0
# Adapted from Cosmos SDK, scripts/protocgen.sh.
# Modified for Ark: schema selection, output paths, and dual generation.
# See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

# Run from the repo root inside the proto-builder image the Makefile pins:
#   make proto-gen

echo "Formatting protobuf files"
buf format -w proto

set -e

echo "Generating gogo proto code"
cd proto
proto_dirs=$(find ./ark -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
  for file in $(find "${dir}" -maxdepth 1 -name '*.proto'); do
    # gogo proto files SHOULD ONLY be generated if go_package does NOT point to ark/api
    # we don't want gogo proto to run for proto files which are natively built for google.golang.org/protobuf
    if grep -q "option go_package" "$file" && grep -H -o -c 'option go_package.*ark/api' "$file" | grep -q ':0$'; then
      buf generate --template buf.gen.gogo.yaml $file
    fi
  done
done

cd ..

# Move gocosmos output from the go_package module-path tree into repository-relative package
# directories.
if [ -d "github.com" ]; then
  cp -r github.com/ararat-network/ark/* ./
  rm -rf github.com
fi

./proto/scripts/protocgen-pulsar.sh
