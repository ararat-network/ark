#!/bin/sh
# Rehearsal helper: consensus must be held by localnet-saturation.sh. Fill
# node0 with fee-paying normal transactions and prove the carrier stays empty.
set -eu
DATA=${DATA:-/data}
PUBLIC_RPC=tcp://node0:26657
CARRIER=${CARRIER:-node3}
keyring="--home $DATA/node0/arkd --keyring-backend test"
work=$(mktemp -d "$DATA/fill-XXXXXX")
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

# Isolating transaction gossip does not isolate consensus connectivity.
sentry_id=$(arkd comet show-node-id --home "$DATA/carrier-sentry/arkd")
curl -sf "http://$CARRIER:26657/net_info" | jq -e --arg id "$sentry_id" \
  '.result.peers | length == 1 and .[0].node_info.id == $id' > /dev/null \
  || fail "carrier has an unexpected peer"
size=$(sed -n '/^\[mempool\]/,/^\[/ s/^max-txs = //p' "$DATA/node0/arkd/config/app.toml")
[ "$size" -gt 0 ] && [ "$size" -le 100 ] || fail "use PUBLIC_MEMPOOL_SIZE=32 at init for this bounded rehearsal"
# Match the independent 5% committee and governance count reservations;
# normal storage receives each reservation's rounding remainder.
size=$((size - size * 5 / 100 - size * 5 / 100))
name=$(arkd keys list $keyring --output json | jq -r '.[0].name')
addr=$(arkd keys show "$name" -a $keyring)
recipient=$(arkd keys show committee -a --home "$DATA/committee" --keyring-backend test)
info=$(arkd query auth account-info "$addr" --node "$PUBLIC_RPC" --output json)
acc=$(printf '%s' "$info" | jq -r '.info.account_number // "0"')
seq=$(printf '%s' "$info" | jq -r '.info.sequence // "0"')
chain=$(jq -r .chain_id "$DATA/node0/arkd/config/genesis.json")
price=$(arkd query treasury gas-price anoah --node "$PUBLIC_RPC" --output json | jq -r '.gas_price.gas_price')
fees="$(( (${price%%.*} + 1) * 200000 * 11 / 10 ))anoah"
i=0
while [ "$i" -le "$size" ]; do
  arkd tx bank send "$name" "$recipient" 1anoah $keyring --fees "$fees" --gas 200000 \
    --chain-id "$chain" --generate-only > "$work/unsigned.json"
  arkd tx sign "$work/unsigned.json" $keyring --from "$name" --offline --account-number "$acc" \
    --sequence "$((seq + i))" --chain-id "$chain" --output-document "$work/signed.json"
  resp=$(arkd tx broadcast "$work/signed.json" --node "$PUBLIC_RPC" --broadcast-mode sync --output json 2> "$work/broadcast.err" || true)
  [ -n "$resp" ] || resp=$(cat "$work/broadcast.err")
  if [ "$i" -lt "$size" ]; then
    [ "$(printf '%s' "$resp" | jq -r '.code // empty')" = 0 ] || fail "fill transaction $i: $resp"
  else
    # A full pool answers Ark's capacity code without committing fee or sequence state.
    printf '%s' "$resp" | jq -e '.codespace == "lanes" and .code == 3' > /dev/null \
      || fail "public node did not reject at capacity: $resp"
  fi
  i=$((i + 1))
done
# Give outbound gossip enough time to expose a bad sentry broadcast setting.
sleep 3
public=$(curl -sf http://node0:9464/metrics | awk '/^ark_mempool_transactions\{/ && /lane="normal"/ { n += $NF } END { print n+0 }')
private=$(curl -sf "http://$CARRIER:9464/metrics" | awk '/^ark_mempool_transactions\{/ { n += $NF } END { print n+0 }')
[ "$public" -eq "$size" ] || fail "public pool has $public transactions, expected $size"
mirrored=$(curl -sf http://node0:26657/num_unconfirmed_txs | jq -r '.result.n_txs')
[ "$mirrored" -eq "$size" ] || fail "CometBFT's list holds $mirrored transactions, expected the pool's $size"
[ "$private" -eq 0 ] || fail "public gossip filled $private carrier slots"
echo "public pool full ($public), overflow rejected, carrier pool empty"
