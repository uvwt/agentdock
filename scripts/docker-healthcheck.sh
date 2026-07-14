#!/usr/bin/env sh
set -eu

host="${AGENTDOCK_HEALTHCHECK_HOST:-127.0.0.1}"
port="${AGENTDOCK_PORT:-8765}"
curl --fail --silent --show-error --max-time 3 "http://${host}:${port}/healthz" >/dev/null
