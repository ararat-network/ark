#!/bin/sh
# Start sync0 from localnet genesis using node0's committed snapshot trust anchor. Existing stores
# resume normally; CometBFT state sync requires an empty store. See README.md.
set -eu

DATA=${DATA:-/data}
HOME_DIR=${HOME_DIR:-$DATA/sync0/arkd}
SOURCE=${SOURCE:-$DATA/node0/arkd}
SNAPSHOT_RPC=${SNAPSHOT_RPC:-http://node0:26657}
RPC_SERVERS=${RPC_SERVERS:-node0:26657,node1:26657}
PEER=${PEER:-node0:26656}
SNAPSHOT_INTERVAL=${SNAPSHOT_INTERVAL:-20}
WAIT_SECONDS=${WAIT_SECONDS:-300}

if [ ! -f "$HOME_DIR/config/genesis.json" ]; then
  chain_id=$(jq -r .chain_id "$SOURCE/config/genesis.json")
  arkd init sync0 --chain-id "$chain_id" --home "$HOME_DIR" > /dev/null
  cp "$SOURCE/config/genesis.json" "$HOME_DIR/config/genesis.json"
  # Match the validators' fee floor; `arkd init` leaves it empty and start
  # refuses that.
  min_gas=$(sed -n 's/^minimum-gas-prices = "\(.*\)"/\1/p' "$SOURCE/config/app.toml")
  sed -i "s|^minimum-gas-prices = .*|minimum-gas-prices = \"$min_gas\"|" "$HOME_DIR/config/app.toml"
  # The peers live on a private subnet; strict routability would refuse them.
  sed -i '/^\[p2p\]/,/^\[/ s|^addr_book_strict = .*|addr_book_strict = false|' "$HOME_DIR/config/config.toml"
fi

# A snapshot exists once node0 is past the interval; state sync then retries
# discovery on its own until the async snapshot write has finished.
deadline=$(( $(date +%s) + WAIT_SECONDS ))
while :; do
  status=$(curl -sf "$SNAPSHOT_RPC/status" 2>/dev/null || true)
  latest=$(printf '%s' "$status" | jq -r '.result.sync_info.latest_block_height // empty' 2>/dev/null || true)
  if [ -n "$latest" ] && [ "$latest" -gt $((SNAPSHOT_INTERVAL + 5)) ]; then
    break
  fi
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "sync0: $SNAPSHOT_RPC did not pass height $((SNAPSHOT_INTERVAL + 5)) within ${WAIT_SECONDS}s" >&2
    exit 1
  fi
  sleep 2
done

peer_id=$(printf '%s' "$status" | jq -r '.result.node_info.id')
trust_height=$((latest - 5))
trust_hash=$(curl -sf "$SNAPSHOT_RPC/block?height=$trust_height" | jq -r '.result.block_id.hash')
echo "sync0: trust height $trust_height hash $trust_hash, rpc $RPC_SERVERS, peer $peer_id@$PEER"

# Written into config.toml rather than exported: the SDK prefixes config env
# overrides with the executable name (ARKD_), not the ARK_ client prefix, and
# a file is easier to inspect afterwards. CometBFT ignores the block once the
# store is non-empty, so a stale anchor on restart is harmless.
sed -i \
  -e '/^\[statesync\]/,/^\[/ s|^enable = .*|enable = true|' \
  -e '/^\[statesync\]/,/^\[/ s|^rpc_servers = .*|rpc_servers = "'"$RPC_SERVERS"'"|' \
  -e '/^\[statesync\]/,/^\[/ s|^trust_height = .*|trust_height = '"$trust_height"'|' \
  -e '/^\[statesync\]/,/^\[/ s|^trust_hash = .*|trust_hash = "'"$trust_hash"'"|' \
  "$HOME_DIR/config/config.toml"

exec arkd start --home "$HOME_DIR" \
  --p2p.persistent_peers "$peer_id@$PEER" \
  --rpc.laddr tcp://0.0.0.0:26657
