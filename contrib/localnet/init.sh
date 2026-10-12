#!/bin/sh
# Generates the Compose localnet under /data: one arkd home per validator and
# one price-feed config each, from the testnet artefact with every validator
# seated from its community pool. Runs inside ark/arkd via `make localnet-init`.
set -eu

VALIDATORS=${VALIDATORS:-4}
CHAIN_ID=${CHAIN_ID:-localark}
GENESIS=${GENESIS:-/genesis/testnet.json}
STARTING_IP=${STARTING_IP:-192.168.10.2}
DATA=${DATA:-/data}

# Wipe from inside the container: on a Linux host the previous run's files
# belong to this uid, not the host user's.
rm -rf "$DATA"/* "$DATA"/.[!.]* 2>/dev/null || true

arkd testnet init-files \
  --genesis "$GENESIS" \
  --validator-count "$VALIDATORS" \
  --output-dir "$DATA" \
  --chain-id "$CHAIN_ID" \
  --starting-ip-address "$STARTING_IP" \
  --node-daemon-home arkd \
  --keyring-backend test \
  --commit-timeout 1s

i=0
while [ "$i" -lt "$VALIDATORS" ]; do
  home="$DATA/node$i/arkd"
  app="$home/config/app.toml"

  # Bind REST and gRPC beyond loopback: the host reaches REST through the
  # port map, and the sidecar reads the feed registry over gRPC from its own
  # container. Then point the price-feed client at this node's sidecar.
  sed -i \
    -e '/^\[api\]/,/^\[/ s|^address = .*|address = "tcp://0.0.0.0:1317"|' \
    -e '/^\[grpc\]/,/^\[/ s|^address = .*|address = "0.0.0.0:9090"|' \
    -e '/^\[pricefeed\]/,/^\[/ s|^enabled = .*|enabled = true|' \
    -e '/^\[pricefeed\]/,/^\[/ s|^sidecar_addresses = .*|sidecar_addresses = ["pricefeed'"$i"':8080"]|' \
    -e '/^\[pricefeed\.tls\]/,/^\[/ s|^mode = .*|mode = "plaintext"|' \
    -e '/^\[prometheus\]/,/^\[/ s|^enabled = .*|enabled = true|' \
    -e '/^\[prometheus\]/,/^\[/ s|^address = .*|address = "0.0.0.0:9464"|' \
    "$app"

  # Sidecar runtime config, polling this node for the feed registry.
  cfg="$DATA/node$i/pricefeed/pricefeed.toml"
  pricefeed init --config "$cfg"
  sed -i '/^\[client\]/,/^\[/ s|^addresses = .*|addresses = ["node'"$i"':9090"]|' "$cfg"

  # Container-to-container transport is explicitly plaintext on this localnet.
  sed -i '/^\[client\.tls\]/,/^\[/ s|^mode = .*|mode = "plaintext"|' "$cfg"

  i=$((i + 1))
done

# Snapshot server for the "statesync" profile: node0 keeps a few recent
# snapshots so sync0 has something to restore from. Validators would not
# normally do this; on a localnet node0 is the only full node there is.
sed -i '/^\[state-sync\]/,/^\[/ {s|^snapshot-interval = .*|snapshot-interval = 20|;s|^snapshot-keep-recent = .*|snapshot-keep-recent = 3|}' \
  "$DATA/node0/arkd/config/app.toml"

echo "wrote $VALIDATORS validator homes for chain-id $CHAIN_ID under $DATA"
