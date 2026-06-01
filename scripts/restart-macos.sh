#!/bin/zsh
set -euo pipefail

LABEL="${AGENTDOCK_LAUNCHD_LABEL:-com.uvwt.agentdock}"
USER_ID="$(id -u)"

launchctl kickstart -k "gui/$USER_ID/$LABEL"
sleep 2
curl -fsS --max-time 5 http://127.0.0.1:18766/healthz
printf '\nrestarted: %s\n' "$LABEL"
