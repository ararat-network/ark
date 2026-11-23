#!/bin/sh
# Fork a one-validator chain's committed state with in-place-testnet and check that the fork runs
# under its own validator. Environment variables and scratch outputs are documented in README.md.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

WORK=${WORK:-$ROOT/build/fork-rehearsal}
SOURCE_CHAIN_ID=ark-source
FORK_CHAIN_ID=ark-fork
SOURCE_BLOCKS=5
RPC=http://localhost:26657

# The nodes bind the default ports, so a Docker localnet or any other node on
# this host would silently answer the queries below in their place.
if curl -sf "$RPC/status" >/dev/null 2>&1; then
  echo "another node is already serving $RPC; stop it (make localnet-stop) before rehearsing" >&2
  exit 2
fi

rm -rf "$WORK"
mkdir -p "$WORK/bin"

stop_node() {
  if [ -n "${NODE_PID:-}" ]; then
    kill "$NODE_PID" 2>/dev/null || true
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=
  fi
}
trap stop_node EXIT INT TERM

fail() {
  echo "$*" >&2
  exit 1
}

if [ -z "${ARKD:-}" ]; then
  echo "building arkd from the working tree"
  go build -trimpath -o "$WORK/bin/arkd" ./cmd/arkd
  ARKD=$WORK/bin/arkd
fi
export COSMOS_SDK_CONFIG_SCOPE=arkd

# start_node <log> <arkd args...>: one node at a time, in the background.
start_node() {
  log=$1
  shift
  "$ARKD" "$@" > "$log" 2>&1 &
  NODE_PID=$!
}

height() { curl -sf "$RPC/status" | jq -r '.result.sync_info.latest_block_height // empty' 2>/dev/null; }
network() { curl -sf "$RPC/status" | jq -r '.result.node_info.network // empty' 2>/dev/null; }
validators() { curl -sf "$RPC/validators" | jq -r '.result.validators[].address' 2>/dev/null; }
# key_address <home>: the consensus address of the home's validator key.
key_address() { jq -r .address "$1/config/priv_validator_key.json"; }

# wait_for_height <seconds> <height> <log>: polls once a second, and gives up
# at once when the node has exited.
wait_for_height() {
  n=$1
  while [ "$n" -gt 0 ]; do
    h=$(height || true)
    if [ -n "$h" ] && [ "$h" -ge "$2" ]; then return 0; fi
    if ! kill -0 "$NODE_PID" 2>/dev/null; then break; fi
    n=$((n - 1))
    sleep 1
  done
  fail "node did not reach height $2; see $3"
}

# The source chain: one validator from the testnet artefact, so the state forked
# carries seats, vesting accounts, and vote extensions from height 1.
"$ARKD" testnet init-files --genesis app/genesis/testnet.json \
  --validator-count 1 \
  --output-dir "$WORK/testnet" \
  --chain-id "$SOURCE_CHAIN_ID" \
  --keyring-backend test \
  --node-daemon-home arkd \
  --commit-timeout 1s
SOURCE=$WORK/testnet/node0/arkd
SOURCE_VALIDATOR=$(key_address "$SOURCE")

echo "starting $SOURCE_CHAIN_ID"
start_node "$WORK/source.log" start --home "$SOURCE"
wait_for_height 90 "$SOURCE_BLOCKS" "$WORK/source.log"
SOURCE_HEIGHT=$(height)
stop_node
echo "$SOURCE_CHAIN_ID stopped at height $SOURCE_HEIGHT or just past it"

# fresh-key is a full node's home: the fork's validator is a key the source
# chain never had, so the fork commits only if the validator set, the last
# commit, and the extended commit were all rewritten to it. same-key is the
# validator's own home, whose signing state has already signed these heights
# for the old chain.
for variant in fresh-key same-key; do
  FORK=$WORK/fork-$variant
  cp -R "$SOURCE" "$FORK"
  if [ "$variant" = fresh-key ]; then
    rm "$FORK/config/priv_validator_key.json" "$FORK/data/priv_validator_state.json"
  fi
  OPERATOR=$("$ARKD" keys show node0 -a --keyring-backend test --home "$FORK")

  echo "$variant: forking as $FORK_CHAIN_ID"
  start_node "$WORK/fork-$variant.log" in-place-testnet "$FORK_CHAIN_ID" "$OPERATOR" --home "$FORK" --skip-confirmation
  wait_for_height 90 $((SOURCE_HEIGHT + 3)) "$WORK/fork-$variant.log"

  got=$(network)
  [ "$got" = "$FORK_CHAIN_ID" ] || fail "$variant: the fork reports chain ID $got, not $FORK_CHAIN_ID"
  FORK_VALIDATOR=$(key_address "$FORK")
  got=$(validators)
  [ "$got" = "$FORK_VALIDATOR" ] || fail "$variant: the validator set is not the fork's key $FORK_VALIDATOR alone: $got"
  if [ "$variant" = fresh-key ] && [ "$FORK_VALIDATOR" = "$SOURCE_VALIDATOR" ]; then
    fail "$variant: the fork kept the source validator's key"
  fi
  FORK_HEIGHT=$(height)
  stop_node

  # Once stopped, the fork is an ordinary chain: the plain start command runs it.
  start_node "$WORK/fork-$variant-restart.log" start --home "$FORK"
  wait_for_height 90 $((FORK_HEIGHT + 2)) "$WORK/fork-$variant-restart.log"
  got=$(network)
  [ "$got" = "$FORK_CHAIN_ID" ] || fail "$variant: the restarted fork reports chain ID $got, not $FORK_CHAIN_ID"
  echo "$variant: fork passed height $((FORK_HEIGHT + 2)) across a restart under validator $FORK_VALIDATOR"
  stop_node
done

echo "fork rehearsal passed"
