#!/usr/bin/env sh
set -eu

AGENTDOCK_HOME_DIR="${AGENTDOCK_HOME:-$HOME/.agentdock}"
RUNNER_DIR="${1:-$AGENTDOCK_HOME_DIR/browser-runner}"

mkdir -p "$RUNNER_DIR"
cp -R examples/browser-runner/. "$RUNNER_DIR/"
cd "$RUNNER_DIR"
npm install
printf 'browser runner installed at %s\n' "$RUNNER_DIR"
