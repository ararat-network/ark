#!/bin/sh
# Generates the Compose localnet under /data: one arkd home per validator and
# one price-feed config each. Runs inside ark/arkd via `make localnet-init`.
set -eu

VALIDATORS=${VALIDATORS:-4}
CHAIN_ID=${CHAIN_ID:-localark}
STARTING_IP=${STARTING_IP:-192.168.10.2}
DATA=${DATA:-/data}
PUBLIC_MEMPOOL_SIZE=${PUBLIC_MEMPOOL_SIZE:-5000}
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
    -e '/^\[pricefeed\.tls\]/,/^\[/ s|^mode = .*|mode = "plaintext"|' \
    -e '/^\[prometheus\]/,/^\[/ s|^enabled = .*|enabled = true|' \
    -e '/^\[prometheus\]/,/^\[/ s|^address = .*|address = "0.0.0.0:9464"|' \
    "$app"

  if [ "$i" -lt "$CARRIER" ]; then
    sed -i '/^\[mempool\]/,/^\[/ s|^max-txs = .*|max-txs = '"$PUBLIC_MEMPOOL_SIZE"'|' "$app"
  fi

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

# Multi-validator rehearsal: the carrier has no public-network interface and
# dials only its dedicated sentry. Both disable transaction broadcast. PEX is
# off so peer discovery cannot recreate a public transaction path.
if [ "$VALIDATORS" -gt 1 ]; then
  sentry_home="$DATA/carrier-sentry/arkd"
  arkd init carrier-sentry --chain-id "$CHAIN_ID" --home "$sentry_home" > /dev/null
  cp "$genesis" "$sentry_home/config/genesis.json"
  sentry_id=$(arkd comet show-node-id --home "$sentry_home")
  carrier_home="$DATA/node$CARRIER/arkd"
  carrier_id=$(arkd comet show-node-id --home "$carrier_home")
  peers=""
  i=0
  while [ "$i" -lt "$CARRIER" ]; do
    peer_id=$(arkd comet show-node-id --home "$DATA/node$i/arkd")
    peers="${peers:+$peers,}$peer_id@node$i:26656"
    i=$((i + 1))
  done
  # Keep the public validator mesh and replace only the carrier connection.
  i=0
  while [ "$i" -lt "$CARRIER" ]; do
    public_peers="$sentry_id@carrier-sentry:26656"
    j=0
    while [ "$j" -lt "$CARRIER" ]; do
      if [ "$j" -ne "$i" ]; then
        peer_id=$(arkd comet show-node-id --home "$DATA/node$j/arkd")
        public_peers="$public_peers,$peer_id@node$j:26656"
      fi
      j=$((j + 1))
    done
    sed -i \
      -e '/^\[p2p\]/,/^\[/ s|^persistent_peers = .*|persistent_peers = "'"$public_peers"'"|' \
      -e '/^\[p2p\]/,/^\[/ s|^pex = .*|pex = false|' \
      "$DATA/node$i/arkd/config/config.toml"
    i=$((i + 1))
  done
  sed -i \
    -e '/^\[p2p\]/,/^\[/ s|^persistent_peers = .*|persistent_peers = "'"$sentry_id"'@carrier-sentry:26656"|' \
    -e '/^\[p2p\]/,/^\[/ s|^pex = .*|pex = false|' \
    -e '/^\[p2p\]/,/^\[/ s|^max_num_inbound_peers = .*|max_num_inbound_peers = 0|' \
    -e '/^\[p2p\]/,/^\[/ s|^external_address = .*|external_address = ""|' \
    -e '/^\[mempool\]/,/^\[/ s|^broadcast = .*|broadcast = false|' \
    "$carrier_home/config/config.toml"
  sed -i \
    -e '/^\[p2p\]/,/^\[/ s|^persistent_peers = .*|persistent_peers = "'"$peers"'"|' \
    -e '/^\[p2p\]/,/^\[/ s|^private_peer_ids = .*|private_peer_ids = "'"$carrier_id"'"|' \
    -e '/^\[p2p\]/,/^\[/ s|^addr_book_strict = .*|addr_book_strict = false|' \
    -e '/^\[p2p\]/,/^\[/ s|^pex = .*|pex = false|' \
    -e '/^\[mempool\]/,/^\[/ s|^broadcast = .*|broadcast = false|' \
    "$sentry_home/config/config.toml"
fi

echo "wrote $VALIDATORS validator homes for chain-id $CHAIN_ID under $DATA"
if [ "$VALIDATORS" -gt 1 ]; then
  echo "dark carrier: node$CARRIER via carrier-sentry"
fi
echo "emergency committee: $committee (3-of-4, keyring $committee_home)"
