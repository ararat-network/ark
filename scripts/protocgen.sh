#!/bin/sh

# How to run manually:
# docker build --pull --rm -f "contrib/devtools/Dockerfile" -t cosmossdk-proto:latest "contrib/devtools"
# docker run --rm -v $(pwd):/workspace --workdir /workspace cosmossdk-proto sh ./scripts/protocgen.sh

echo "Formatting protobuf files"
find ./proto -name "*.proto" -exec clang-format -i {} \;

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

# move proto files to the right places
#
# gocosmos generates files based on go_package into a directory tree starting
# from the module path. For go_package = "ark/x/market/types", the output
# lands in ark/x/market/types/ relative to the `out` directory (repo root).
# We need to move these into the actual repo structure.
if [ -d "ark" ]; then
  cp -r ark/* ./
  rm -rf ark
fi

./scripts/protocgen-pulsar.sh
