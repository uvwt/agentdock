#!/bin/zsh
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "desktop smoke is macOS-only" >&2
  exit 1
fi

printf '==> healthz\n'
curl -fsS --max-time 5 http://127.0.0.1:18766/healthz >/dev/null
printf 'healthz ok\n'

printf '==> dependency checks\n'
command -v osascript >/dev/null
command -v screencapture >/dev/null
command -v pbcopy >/dev/null
command -v pbpaste >/dev/null
command -v cliclick >/dev/null
printf 'dependencies ok\n'

printf '==> AppleScript app visibility\n'
osascript -e 'tell application "System Events" to get name of every process whose background only is false' >/dev/null
printf 'applescript ok\n'

printf '==> screenshot permission\n'
TMP="$(mktemp -t agentdock-smoke-screenshot).png"
SCREENSHOT_ERR="$(mktemp -t agentdock-smoke-screenshot-err)"
SCREENSHOT_OK=false
for attempt in 1 2 3; do
  : >"$SCREENSHOT_ERR"
  if screencapture -x "$TMP" 2>"$SCREENSHOT_ERR" && [[ -s "$TMP" ]]; then
    SCREENSHOT_OK=true
    break
  fi
  sleep 1
done
if [[ "$SCREENSHOT_OK" != "true" ]]; then
  echo "screenshot failed after 3 attempts: $(cat "$SCREENSHOT_ERR")" >&2
  rm -f "$TMP" "$SCREENSHOT_ERR"
  exit 1
fi
rm -f "$TMP" "$SCREENSHOT_ERR"
printf 'screenshot ok\n'

printf 'desktop smoke ok\n'
