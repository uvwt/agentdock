#!/bin/zsh
set -euo pipefail

LABEL="${AGENTDOCK_LAUNCHD_LABEL:-com.uvwt.agentdock}"
AGENTDOCK_ENV="$HOME/Library/Application Support/AgentDock/agentdock.env"
TARGET="$HOME/.local/bin/agentdock"

[[ "$(uname -s)" == "Darwin" ]] || { print -u2 -- "ERROR: 此脚本只支持 macOS"; exit 1; }
[[ -f "$AGENTDOCK_ENV" && ! -L "$AGENTDOCK_ENV" ]] || { print -u2 -- "ERROR: 缺少服务配置：$AGENTDOCK_ENV"; exit 1; }
[[ -x "$TARGET" && ! -L "$TARGET" ]] || { print -u2 -- "ERROR: 缺少二进制：$TARGET"; exit 1; }

set -a
source "$AGENTDOCK_ENV"
set +a
: "${AGENTDOCK_HOST:=127.0.0.1}"
: "${AGENTDOCK_PORT:=8765}"
[[ "$AGENTDOCK_PORT" == <1-65535> ]] || { print -u2 -- "ERROR: 端口无效：$AGENTDOCK_PORT"; exit 1; }

normalize_version() {
  print -r -- "${1#v}"
}

launchd_pid() {
  local output
  output="$(launchctl print "gui/$(id -u)/$LABEL" 2>/dev/null)" || return 1
  print -r -- "$output" | sed -n 's/^[[:space:]]*pid = \([0-9][0-9]*\).*$/\1/p' | head -n 1
}

case "$AGENTDOCK_HOST" in
  0.0.0.0|::) HEALTH_HOST="127.0.0.1" ;;
  *:*) HEALTH_HOST="[$AGENTDOCK_HOST]" ;;
  *) HEALTH_HOST="$AGENTDOCK_HOST" ;;
esac
HEALTH_URL="http://$HEALTH_HOST:$AGENTDOCK_PORT/healthz"
EXPECTED_VERSION="$("$TARGET" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
OLD_PID="$(launchd_pid || true)"
[[ -n "$OLD_PID" && "$OLD_PID" != "0" ]] || { print -u2 -- "ERROR: LaunchAgent 未运行：$LABEL"; exit 1; }

launchctl kickstart -k "gui/$(id -u)/$LABEL"
attempts=60
while (( attempts-- > 0 )); do
  NEW_PID="$(launchd_pid || true)"
  if [[ -n "$NEW_PID" && "$NEW_PID" != "0" && "$NEW_PID" != "$OLD_PID" ]]; then
    PROCESS_COMMAND="$(ps -p "$NEW_PID" -o command= 2>/dev/null || true)"
    LISTENERS="$(lsof -nP -iTCP:"$AGENTDOCK_PORT" -sTCP:LISTEN -t 2>/dev/null || true)"
    if [[ "$PROCESS_COMMAND" == "$TARGET" || "$PROCESS_COMMAND" == "$TARGET "* ]] && print -r -- "$LISTENERS" | grep -qx "$NEW_PID"; then
      HEALTH_BODY="$(curl -fsS --max-time 2 "$HEALTH_URL" 2>/dev/null || true)"
      HEALTH_OK=false
      if print -r -- "$HEALTH_BODY" | grep -Eq '"ok"[[:space:]]*:[[:space:]]*true'; then
        HEALTH_OK=true
      fi
      HEALTH_VERSION="$(print -r -- "$HEALTH_BODY" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
      if [[ "$HEALTH_OK" == true && "$(normalize_version "$HEALTH_VERSION")" == "$(normalize_version "$EXPECTED_VERSION")" ]]; then
        print -- "restarted: $LABEL"
        print -- "pid: $NEW_PID"
        print -- "healthz: $HEALTH_URL"
        print -- "version: $EXPECTED_VERSION"
        exit 0
      fi
    fi
  fi
  sleep 0.5
done

print -u2 -- "ERROR: 重启后未确认新 PID、目标二进制、端口监听和目标版本 healthz"
exit 1
