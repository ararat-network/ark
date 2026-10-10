#!/bin/sh

# SPDX-License-Identifier: Apache-2.0
# Adapted from Cosmos SDK, scripts/protocgen-pulsar.sh.
# Modified for Ark: POSIX shell, output paths, and schema notices.
# See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

# Generates the google.golang.org/protobuf API module. protocgen.sh runs it after gogo; it also
# runs alone from the repo root.
set -eu

echo "Cleaning API directory"
find ./api -type f \( -iname \*.pulsar.go -o -iname \*.pb.go -o -iname \*.cosmos_orm.go -o -iname \*.pb.gw.go \) -delete
find ./api -empty -type d -delete

echo "Generating API module"
(cd proto; buf generate --template buf.gen.yaml)

# Keep schema attribution prominent in generated Pulsar and gateway files.
sh ./proto/scripts/protocgen-notices.sh
