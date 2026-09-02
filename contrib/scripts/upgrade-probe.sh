#!/bin/sh
# Polls one node through a coordinated upgrade. Passes once the chain reports
# the plan as applied and has committed blocks beyond it; fails if the chain
# sails past the upgrade height without applying it, or when the polls run
# out. The applied-plan query is the signal rather than the version string,
# because rehearsing uncommitted work builds old and new from the same
# `git describe` output.
#
#   contrib/scripts/upgrade-probe.sh <iterations> <sleep-seconds> <upgrade-height> <plan-name> <rpc-url> <api-url>
set -u

if [ "$#" -ne 6 ]; then
  echo "usage: $0 <iterations> <sleep-seconds> <upgrade-height> <plan-name> <rpc-url> <api-url>" >&2
  exit 2
fi

ITERATIONS=$1
SLEEP=$2
UPGRADE_HEIGHT=$3
PLAN=$4
RPC=${5%/}
API=${6%/}

count=0
while [ "$count" -lt "$ITERATIONS" ]; do
  height=$(curl -sf "$RPC/status" | jq -r '.result.sync_info.latest_block_height // empty' 2>/dev/null)
  version=$(curl -sf "$API/cosmos/base/tendermint/v1beta1/node_info" | jq -r '.application_version.version // empty' 2>/dev/null)
  applied=$(curl -sf "$API/cosmos/upgrade/v1beta1/applied_plan/$PLAN" | jq -r '.height // "0"' 2>/dev/null)
  echo "poll $((count + 1))/$ITERATIONS: height=${height:-none} version=${version:-none} applied_at=${applied:-none}"

  if [ "${applied:-0}" -gt 0 ] && [ -n "$height" ] && [ "$height" -gt "$applied" ]; then
    echo "ok: $PLAN applied at height $applied, chain at $height on ${version:-unknown}"
    exit 0
  fi
  if [ -n "$height" ] && [ "$height" -gt "$UPGRADE_HEIGHT" ] && [ "${applied:-0}" -eq 0 ]; then
    echo "failed: height $height passed upgrade height $UPGRADE_HEIGHT without applying $PLAN" >&2
    exit 1
  fi

  count=$((count + 1))
  sleep "$SLEEP"
done

echo "failed: $PLAN not applied after $ITERATIONS polls" >&2
exit 1
