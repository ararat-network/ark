#!/bin/sh
# Run on an otherwise idle four-validator localnet initialised with
# PUBLIC_MEMPOOL_SIZE=32. Pause two validators to hold pending transactions,
# fill a public pool, then submit privately and restore consensus for inclusion.
set -eu
compose() { docker compose -f contrib/localnet/docker-compose.yml --profile rehearsal "$@"; }
log=$(mktemp)
paused=false
runner=""
cleanup() {
  if [ "$paused" = true ]; then docker unpause ark-node1 ark-node2 > /dev/null; fi
  if [ -n "$runner" ]; then wait "$runner" || true; fi
  rm -f "$log"
}
trap cleanup EXIT INT TERM
# This affects only the explicitly named disposable localnet validators.
docker pause ark-node1 ark-node2 > /dev/null
paused=true
compose run --rm --entrypoint sh runbook /fill-mempool.sh
compose run --rm runbook > "$log" 2>&1 &
runner=$!
queued=false
i=0
while [ "$i" -lt 90 ]; do
  if ! kill -0 "$runner" 2>/dev/null; then break; fi
  count=$(curl -sf http://127.0.0.1:9467/metrics | awk '/^ark_mempool_transactions\{/ { n += $NF } END { print n+0 }' || true)
  if [ "${count:-0}" -gt 0 ]; then queued=true; break; fi
  sleep 1
  i=$((i + 1))
done
docker unpause ark-node1 ark-node2 > /dev/null
paused=false
status=0
wait "$runner" || status=$?
runner=""
cat "$log"
[ "$queued" = true ] || { echo "FAIL: no private admission while public pool was full" >&2; exit 1; }
[ "$status" -eq 0 ] || exit "$status"
echo "saturation rehearsal passed: private admission at public capacity and inclusion after consensus resumed"
