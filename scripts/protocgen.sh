#!/usr/bin/env bash

set -eo pipefail

echo "Formatting protobuf files"
find ./proto -name "*.proto" -exec clang-format -i {} \;

echo "Cleaning API directory"
find ./api -type f \( -iname \*.pulsar.go -o -iname \*.pb.go -o -iname \*.pb.gw.go \) -delete 2>/dev/null || true
find ./api -empty -type d -delete 2>/dev/null || true

echo "Generating protobuf code"
cd proto
buf generate
cd ..

echo "Done"
