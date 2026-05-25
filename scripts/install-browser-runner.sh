#!/usr/bin/env sh
set -eu
: "${WORKSPACE:=$(pwd)}"
RUNNER_DIR="${1:-$WORKSPACE/.mcp/browser-runner}"
mkdir -p "$RUNNER_DIR"
cp -R examples/browser-runner/. "$RUNNER_DIR/"
cd "$RUNNER_DIR"
npm install
npx playwright install chromium
printf 'browser runner installed at %s\n' "$RUNNER_DIR"
