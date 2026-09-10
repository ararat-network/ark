#!/bin/sh
# Rehearse offline multisig emergency submission through the localnet carrier, checking fee refusal,
# privacy, and inclusion. Each rerun needs an unused active asset for the term; see README.md.
set -eu

DATA=${DATA:-/data}
CHAIN_ID=${CHAIN_ID:-localark}
VALIDATORS=${VALIDATORS:-4}
[ "$VALIDATORS" -ge 2 ] || { echo "the runbook needs a carrier and at least one public node; VALIDATORS=$VALIDATORS" >&2; exit 2; }
# The last validator is the dark carrier init.sh configured; the rest are the
# public nodes whose mempools must never see the transaction.
CARRIER=${CARRIER:-node$((VALIDATORS - 1))}
PUBLIC_NODES=${PUBLIC_NODES:-$(i=0; while [ "$i" -lt $((VALIDATORS - 1)) ]; do printf 'node%d ' "$i"; i=$((i + 1)); done)}
DENOM=${DENOM:-}
GAS=${GAS:-500000}
INCLUSION_POLLS=${INCLUSION_POLLS:-60}
# NumInjectedTxs in abci/types: the oracle commit the proposer injects at
# index 0, so the committee transaction lands at index 1.
INJECTED_TXS=1

CARRIER_RPC="tcp://$CARRIER:26657"
KEYRING="--keyring-backend test --home $DATA/committee"
WORK="$DATA/committee/ceremony-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$WORK"

fail() { echo "FAIL: $*" >&2; exit 1; }
step() { echo; echo "== $*"; }
q() { arkd query "$@" --node "$CARRIER_RPC" --output json; }

tx_hash_of() {
  # sha256 of a base64 transaction, upper-case hex as arkd prints it.
  printf '%s' "$1" | base64 -d | sha256sum | cut -d' ' -f1 | tr 'a-f' 'A-F'
}

step "mandate and asset"
committee=$(arkd keys show committee -a $KEYRING)
mandate=$(q asset emergency-mandate)
term=$(printf '%s' "$mandate" | jq -r '.mandate.envelope.term')
[ "$(printf '%s' "$mandate" | jq -r '.active')" = "true" ] || fail "mandate is not active: $mandate"
[ "$(printf '%s' "$mandate" | jq -r '.mandate.envelope.committee')" = "$committee" ] \
  || fail "mandate committee is not the localnet committee $committee"
if [ -z "$DENOM" ]; then
  DENOM=$(q asset assets | jq -r '[.priced_assets[].asset | select(.status == "ASSET_STATUS_ACTIVE")][0].denom // empty')
  [ -n "$DENOM" ] || fail "no ACTIVE asset left to suspend"
fi
echo "committee=$committee term=$term denom=$DENOM"

# proto3 JSON omits zero fields, so a fresh account has no sequence key.
info=$(q auth account-info "$committee")
account_number=$(printf '%s' "$info" | jq -r '.info.account_number // "0"')
sequence=$(printf '%s' "$info" | jq -r '.info.sequence // "0"')

# Treasury's live consensus price is the fee floor; Ark does not consult
# node-local minimum-gas-prices. Round upward and allow controller movement.
chain_price=$(q treasury gas-price anoah | jq -r '.gas_price.gas_price')
floor=$(( ${chain_price%%.*} + 1 ))
fees="$(( floor * GAS * 11 / 10 ))anoah"
# Zero is strictly below the positive Treasury floor, without decimal rounding.
subfloor_fees="0anoah"
echo "account_number=$account_number sequence=$sequence chain_price=${chain_price}anoah/gas fees=$fees"

# ceremony <fees> <out>: unsigned tx, three offline member signatures, assembly.
ceremony() {
  arkd tx asset emergency-suspend-asset "$DENOM" "$term" \
    --from committee --fees "$1" --gas "$GAS" --chain-id "$CHAIN_ID" \
    --generate-only $KEYRING > "$WORK/unsigned.json"
  sigs=""
  for member in member0 member1 member2; do
    arkd tx sign "$WORK/unsigned.json" --multisig committee --from "$member" \
      --sign-mode amino-json --offline --account-number "$account_number" --sequence "$sequence" \
      --chain-id "$CHAIN_ID" $KEYRING --output-document "$WORK/$member.sig.json"
    sigs="$sigs $WORK/$member.sig.json"
  done
  # shellcheck disable=SC2086
  arkd tx multi-sign "$WORK/unsigned.json" committee $sigs \
    --offline --account-number "$account_number" --sequence "$sequence" \
    --chain-id "$CHAIN_ID" $KEYRING --output-document "$2"
}

broadcast() {
  result=$(arkd tx broadcast "$1" --broadcast-mode sync --node "$CARRIER_RPC" --output json 2> "$WORK/broadcast.err" || true)
  if [ -n "$result" ]; then printf '%s\n' "$result"; else cat "$WORK/broadcast.err"; fi
}

step "sub-floor fee is rejected at CheckTx"
ceremony "$subfloor_fees" "$WORK/subfloor.json"
resp=$(broadcast "$WORK/subfloor.json")
code=$(printf '%s' "$resp" | jq -r '.code // empty' 2>/dev/null || true)
[ -n "$code" ] && [ "$code" != "0" ] || fail "sub-floor transaction was not rejected: $resp"
# SDK ErrInsufficientFee is codespace sdk, code 13; raw logs may be omitted.
printf '%s' "$resp" | jq -e '.codespace == "sdk" and .code == 13' > /dev/null \
  || fail "sub-floor rejection was not a fee failure: $resp"
echo "rejected with code $code"

step "signing ceremony"
ceremony "$fees" "$WORK/signed.json"
echo "assembled $WORK/signed.json"

step "preflight against the carrier"
# Use tx simulate --gas auto to load the multisig key and construct its K-signature placeholder.
# --dry-run cannot access the keyring. Simulation stays on the private carrier RPC.
estimate=$(arkd tx simulate "$WORK/unsigned.json" --from committee --gas auto \
  --chain-id "$CHAIN_ID" --node "$CARRIER_RPC" $KEYRING --output json 2>/dev/null \
  | jq -r '.gas_info.gas_used // empty')
[ -n "$estimate" ] || fail "simulation against the carrier failed"
[ "$estimate" -le "$GAS" ] || fail "gas estimate $estimate exceeds gas limit $GAS"
echo "simulated gas $estimate within limit $GAS"

step "broadcast to the carrier"
resp=$(broadcast "$WORK/signed.json")
code=$(printf '%s' "$resp" | jq -r '.code // empty' 2>/dev/null || true)
txhash=$(printf '%s' "$resp" | jq -r '.txhash // empty' 2>/dev/null || true)
[ "$code" = "0" ] && [ -n "$txhash" ] || fail "CheckTx did not accept the transaction: $resp"
echo "accepted txhash=$txhash"

step "not visible on public mempools"
for node in $PUBLIC_NODES; do
  for b64 in $(curl -sf "http://$node:26657/unconfirmed_txs" | jq -r '.result.txs[]? // empty'); do
    [ "$(tx_hash_of "$b64")" != "$txhash" ] || fail "transaction leaked to $node's mempool"
  done
  echo "$node: absent"
done

step "inclusion"
polls=0
while ! result=$(q tx "$txhash" 2>/dev/null); do
  polls=$((polls + 1))
  [ "$polls" -lt "$INCLUSION_POLLS" ] || fail "not included after $INCLUSION_POLLS polls"
  sleep 1
done
[ "$(printf '%s' "$result" | jq -r '.code')" = "0" ] || fail "transaction failed: $(printf '%s' "$result" | jq -r '.raw_log')"
height=$(printf '%s' "$result" | jq -r '.height')
gas_used=$(printf '%s' "$result" | jq -r '.gas_used')
[ "$gas_used" -le "$GAS" ] || fail "delivery used $gas_used gas, above the limit $GAS"
printf '%s' "$result" | jq -e --arg denom "$DENOM" \
  '.events[] | select(.type == "ark.asset.v1.EventEmergencySuspended") | .attributes[] | select(.key == "denom" and (.value | fromjson? // .) == $denom)' \
  > /dev/null || fail "EventEmergencySuspended for $DENOM missing from the result"
echo "included at height $height with EventEmergencySuspended, gas used $gas_used"

step "top of block, proposed by the carrier"
block=$(curl -sf "http://$CARRIER:26657/block?height=$height")
index=0
found=""
for b64 in $(printf '%s' "$block" | jq -r '.result.block.data.txs[]'); do
  if [ "$(tx_hash_of "$b64")" = "$txhash" ]; then found=$index; break; fi
  index=$((index + 1))
done
[ -n "$found" ] || fail "transaction not found in block $height"
[ "$found" -eq "$INJECTED_TXS" ] || fail "transaction at index $found, expected $INJECTED_TXS (after the injected oracle commit)"
proposer=$(printf '%s' "$block" | jq -r '.result.block.header.proposer_address')
carrier_address=$(jq -r '.address' "$DATA/$CARRIER/arkd/config/priv_validator_key.json")
[ "$proposer" = "$carrier_address" ] || fail "block $height proposed by $proposer, not the carrier $carrier_address"
echo "index $found in block $height, proposed by $CARRIER"

step "asset suspended"
status=$(q asset asset "$DENOM" | jq -r '.priced_asset.asset.status')
[ "$status" = "ASSET_STATUS_SUSPENDED" ] || fail "$DENOM status is $status"
echo "$DENOM is ASSET_STATUS_SUSPENDED"

echo
echo "runbook rehearsal passed: $DENOM suspended under term $term via $CARRIER at height $height"
echo "ceremony files: $WORK"
