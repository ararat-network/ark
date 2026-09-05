#!/bin/sh
# Generates the Compose localnet under /data: one arkd home per validator and
# one price-feed config each. Runs inside ark/arkd via `make localnet-init`.
set -eu

VALIDATORS=${VALIDATORS:-4}
CHAIN_ID=${CHAIN_ID:-localark}
STARTING_IP=${STARTING_IP:-192.168.10.2}
DATA=${DATA:-/data}
# The last validator is the dark carrier the emergency runbook submits through.
CARRIER=$((VALIDATORS - 1))

# Wipe from inside the container: on a Linux host the previous run's files
# belong to this uid, not the host user's.
rm -rf "$DATA"/* "$DATA"/.[!.]* 2>/dev/null || true

arkd testnet init-files \
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
    -e '/^\[pricefeed\]/,/^\[/ s|^enabled = .*|enabled = "true"|' \
    -e '/^\[pricefeed\]/,/^\[/ s|^sidecar_addresses = .*|sidecar_addresses = ["pricefeed'"$i"':8080"]|' \
    "$app"

  # Sidecar runtime config, polling this node for the feed registry.
  cfg="$DATA/node$i/pricefeed/config.json"
  pricefeed init --config "$cfg"
  jq '.client.addresses = ["node'"$i"':9090"]' "$cfg" > "$cfg.tmp"
  mv "$cfg.tmp" "$cfg"

  i=$((i + 1))
done

# Snapshot server for the "statesync" profile: node0 keeps a few recent
# snapshots so sync0 has something to restore from. Validators would not
# normally do this; on a localnet node0 is the only full node there is.
sed -i '/^\[state-sync\]/,/^\[/ {s|^snapshot-interval = .*|snapshot-interval = 20|;s|^snapshot-keep-recent = .*|snapshot-keep-recent = 3|}' \
  "$DATA/node0/arkd/config/app.toml"

# Dark carrier (runbook Variant A): accepts transactions over RPC but relays
# them to no peer, so a committee transaction first appears in a block it
# proposes. Inbound gossip is unaffected.
sed -i '/^\[mempool\]/,/^\[/ s|^broadcast = .*|broadcast = false|' \
  "$DATA/node$CARRIER/arkd/config/config.toml"

# Emergency committee: a 3-of-4 legacy amino multisig of fresh member keys,
# seated in the asset module's mandate from height 1 and funded for fees.
# The shape is left zero: at genesis nothing has been observed about the
# account, which is exactly what a zero shape means.
committee_home="$DATA/committee"
keyring="--keyring-backend test --home $committee_home"
m=0
while [ "$m" -lt 4 ]; do
  arkd keys add "member$m" $keyring --output json > /dev/null
  m=$((m + 1))
done
arkd keys add committee --multisig member0,member1,member2,member3 --multisig-threshold 3 $keyring --output json > /dev/null
committee=$(arkd keys show committee -a $keyring)

genesis="$DATA/node0/arkd/config/genesis.json"
arkd genesis add-genesis-account "$committee" 1000000000000000000000anoah --home "$DATA/node0/arkd"
jq --arg committee "$committee" '
  .app_state.asset.emergency_mandate.envelope |= (
    .term = "1" | .committee = $committee | .activation_height = "1" | .expiry_height = "1000000000"
  )' "$genesis" > "$genesis.tmp"
mv "$genesis.tmp" "$genesis"
i=1
while [ "$i" -lt "$VALIDATORS" ]; do
  cp "$genesis" "$DATA/node$i/arkd/config/genesis.json"
  i=$((i + 1))
done

echo "wrote $VALIDATORS validator homes for chain-id $CHAIN_ID under $DATA"
echo "dark carrier: node$CARRIER; emergency committee: $committee (3-of-4, keyring $committee_home)"
