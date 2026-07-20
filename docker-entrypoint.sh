#!/usr/bin/env sh
set -eu

umask 077
mkdir -p \
  "$HOME/.agentdock/browser-artifacts" \
  "$HOME/.agentdock/tmp" \
  "$HOME/AgentDock"

if [ "${1:-}" = "agentdock" ]; then
  case "${2:-}" in
    --version|update|skill) ;;
    *) agentdock skill bootstrap --bundle /usr/local/share/agentdock/core-skills ;;
  esac
fi

exec "$@"
