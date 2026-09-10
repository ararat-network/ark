#!/bin/sh
# Require catch-up plus an earliest block above genesis, proving snapshot restore rather than
# replay. Usage and poll arguments: see README.md.
set -u

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <iterations> <sleep-seconds> <rpc-url>" >&2
  exit 2
fi

ITERATIONS=$1
SLEEP=$2
RPC=${3%/}

count=0
while [ "$count" -lt "$ITERATIONS" ]; do
  sync=$(curl -sf "$RPC/status" | jq -r '.result.sync_info' 2>/dev/null)
  catching=$(printf '%s' "$sync" | jq -r '.catching_up // empty' 2>/dev/null)
  latest=$(printf '%s' "$sync" | jq -r '.latest_block_height // empty' 2>/dev/null)
  earliest=$(printf '%s' "$sync" | jq -r '.earliest_block_height // empty' 2>/dev/null)
  echo "poll $((count + 1))/$ITERATIONS: catching_up=${catching:-none} earliest=${earliest:-none} latest=${latest:-none}"

  if [ "${catching:-true}" = "false" ] && [ -n "$earliest" ] && [ -n "$latest" ]; then
    if [ "$earliest" -le 1 ]; then
      echo "failed: earliest block is $earliest, so the node replayed from genesis instead of restoring a snapshot" >&2
      exit 1
    fi
    if [ "$latest" -le "$earliest" ]; then
      echo "failed: restored at $earliest but has not committed a block of its own" >&2
      exit 1
    fi
    echo "ok: restored a snapshot at block $earliest and committed through $latest"
    exit 0
  fi

  count=$((count + 1))
  sleep "$SLEEP"
done

echo "failed: did not catch up from a snapshot after $ITERATIONS polls" >&2
exit 1
