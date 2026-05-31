#!/usr/bin/env sh
set -eu

: "${AGENTDOCK_DIR:=/agent-dock}"
: "${AGENTDOCK_PLUGIN_DIR:=plugins}"
: "${AGENTDOCK_BROWSER_RUNNER_DIR:=browser-runner}"
: "${AGENTDOCK_BROWSER_ARTIFACT_DIR:=browser-artifacts}"

mkdir -p /workspace
mkdir -p "$AGENTDOCK_DIR"
mkdir -p "$AGENTDOCK_DIR/$AGENTDOCK_PLUGIN_DIR"
mkdir -p "$AGENTDOCK_DIR/$AGENTDOCK_BROWSER_ARTIFACT_DIR"
mkdir -p "$AGENTDOCK_DIR/state" "$AGENTDOCK_DIR/cache" "$AGENTDOCK_DIR/runbooks"

# 浏览器增强镜像会内置 /opt/agentdock/browser-runner。
# 如果 AgentDock 挂载目录里还没有 runner，则启动时自动初始化一份，避免用户手动 cp/npm install。
if [ "${AGENTDOCK_BROWSER_ENABLED:-false}" = "true" ] && [ -d /opt/agentdock/browser-runner ]; then
  if [ ! -f "$AGENTDOCK_DIR/$AGENTDOCK_BROWSER_RUNNER_DIR/browser-runner.js" ]; then
    mkdir -p "$AGENTDOCK_DIR/$AGENTDOCK_BROWSER_RUNNER_DIR"
    cp -R /opt/agentdock/browser-runner/. "$AGENTDOCK_DIR/$AGENTDOCK_BROWSER_RUNNER_DIR/"
  fi
fi

exec "$@"
