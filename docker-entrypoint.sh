#!/usr/bin/env sh
set -eu

umask 077
mkdir -p \
  "$HOME/.agentdock/browser-artifacts" \
  "$HOME/.agentdock/tmp" \
  "$HOME/AgentDock"

exec "$@"
