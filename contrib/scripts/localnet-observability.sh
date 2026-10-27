#!/bin/sh
# Hold the localnet Prometheus to the rules loaded and node0's arkd target scraped within ITERATIONS polls.
# Usage: see README.md.
set -u

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <iterations> <sleep-seconds> <prometheus-url>" >&2
  exit 2
fi

ITERATIONS=$1
SLEEP=$2
PROM=${3%/}

count=0
while [ "$count" -lt "$ITERATIONS" ]; do
  rules=$(curl -sf "$PROM/api/v1/rules" | jq -r '[.data.groups[].rules[].name] | length' 2>/dev/null)
  health=$(curl -sf "$PROM/api/v1/targets" | jq -r '[.data.activeTargets[] | select(.labels.job == "arkd" and .labels.node == "node0") | .health] | first // empty' 2>/dev/null)
  echo "poll $((count + 1))/$ITERATIONS: rules=${rules:-none} node0=${health:-none}"

  if [ "${rules:-0}" -gt 0 ] && [ "${health:-}" = "up" ]; then
    echo "ok: $rules rules loaded, node0 scraped"
    exit 0
  fi

  count=$((count + 1))
  sleep "$SLEEP"
done

echo "failed: Prometheus did not load rules and scrape node0 after $ITERATIONS polls" >&2
exit 1
