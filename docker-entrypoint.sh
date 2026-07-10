#!/usr/bin/env sh
set -eu

BROWSER_RUNNER_DIR="browser-runner"
BROWSER_ARTIFACT_DIR="browser-artifacts"

AGENTDOCK_HOME_DIR="$HOME/.agentdock"
AGENTDOCK_DEFAULT_DIR="$HOME/AgentDock"

mkdir -p "$AGENTDOCK_HOME_DIR" "$AGENTDOCK_DEFAULT_DIR"
mkdir -p "$AGENTDOCK_HOME_DIR/$BROWSER_ARTIFACT_DIR"
mkdir -p "$AGENTDOCK_HOME_DIR/tmp"

# 浏览器增强镜像会内置 /opt/agentdock/browser-runner。
# 如果内部状态目录里还没有 runner，则启动时初始化一份，避免用户手动 cp/npm install。
if [ "${AGENTDOCK_BROWSER_ENABLED:-false}" = "true" ] && [ -d /opt/agentdock/browser-runner ]; then
  if [ ! -f "$AGENTDOCK_HOME_DIR/$BROWSER_RUNNER_DIR/browser-runner.js" ]; then
    mkdir -p "$AGENTDOCK_HOME_DIR/$BROWSER_RUNNER_DIR"
    cp -R /opt/agentdock/browser-runner/. "$AGENTDOCK_HOME_DIR/$BROWSER_RUNNER_DIR/"
  fi
fi

exec "$@"
