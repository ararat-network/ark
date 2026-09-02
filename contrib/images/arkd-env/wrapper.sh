#!/bin/sh
# Container entrypoint: runs arkd against this node's home. ID selects
# /data/node<ID>/arkd, the layout `arkd testnet init-files` writes; set
# ARKD_HOME to use any other mounted home.
set -eu

ID=${ID:-0}
ARKD_HOME=${ARKD_HOME:-/data/node${ID}/arkd}

exec arkd --home "$ARKD_HOME" "$@"
