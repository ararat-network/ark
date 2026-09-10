#!/bin/sh
# Require progress beyond NUM_BLOCKS and at least one Oracle rate within ITERATIONS polls. Height
# alone permits empty vote extensions. Usage: see README.md.
set -u

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <iterations> <sleep-seconds> <num-blocks> <rpc-url> <api-url>" >&2
  exit 2
fi

ITERATIONS=$1
SLEEP=$2
NUM_BLOCKS=$3
RPC=${4%/}
API=${5%/}

count=0
while [ "$count" -lt "$ITERATIONS" ]; do
  height=$(curl -sf "$RPC/status" | jq -r '.result.sync_info.latest_block_height // empty' 2>/dev/null)
  rates=$(curl -sf "$API/ark/oracle/v1/denoms/exchange_rates" | jq -r '.exchange_rates | length' 2>/dev/null)
  echo "poll $((count + 1))/$ITERATIONS: height=${height:-none} exchange_rates=${rates:-none}"

  if [ -n "$height" ] && [ "$height" -gt "$NUM_BLOCKS" ] && [ "${rates:-0}" -gt 0 ]; then
    echo "ok: past block $NUM_BLOCKS with $rates exchange rate(s)"
    exit 0
  fi

  count=$((count + 1))
  sleep "$SLEEP"
done

echo "failed: no height above $NUM_BLOCKS with an exchange rate after $ITERATIONS polls" >&2
exit 1
