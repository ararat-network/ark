# this script is for generating protobuf files for the new google.golang.org/protobuf API
set -eo pipefail

echo "Cleaning API directory"
find ./api -type f \( -iname \*.pulsar.go -o -iname \*.pb.go -o -iname \*.cosmos_orm.go -o -iname \*.pb.gw.go \) -delete 2>/dev/null || true
find ./api -empty -type d -delete 2>/dev/null || true

echo "Generating API module"
(cd proto; buf generate --template buf.gen.yaml)
