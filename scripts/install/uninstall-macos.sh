#!/bin/zsh
set -euo pipefail

LABEL="com.uvwt.agentdock"
TUNNEL_LABEL="com.uvwt.agentdock.cloudflared"
APP_SUPPORT_DIR="$HOME/Library/Application Support/AgentDock"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
TUNNEL_PLIST_PATH="$HOME/Library/LaunchAgents/$TUNNEL_LABEL.plist"
LOG_DIR="$HOME/Library/Logs/AgentDock"
BINARY_PATH="$HOME/.local/bin/agentdock"
CLOUDFLARED_BINARY_PATH="$HOME/.local/bin/cloudflared"
STATE_DIR="$HOME/.agentdock"
WORK_DIR="$HOME/AgentDock"
APP_PATH="${AGENTDOCK_APP_PATH:-/Applications/AgentDock.app}"
PURGE_DATA=false

die() {
  print -u2 -- "ERROR: $*"
  exit 1
}

usage() {
  cat <<'USAGE'
AgentDock macOS 卸载脚本。

用法：
  zsh uninstall-macos.sh [--remove-binary] [--purge-data]

默认行为：
  注销 AgentDock.app 管理的后台服务，并停止/删除旧 LaunchAgent、服务支持文件和日志；
  删除 AgentDock.app 与旧 ~/.local/bin 运行时；保留 ~/.agentdock 与 ~/AgentDock。

选项：
  --remove-binary  兼容旧调用；当前默认已经删除程序二进制
  --purge-data     彻底卸载，同时删除程序、~/.agentdock 与 ~/AgentDock
  -h, --help       显示帮助
USAGE
}

while (( $# > 0 )); do
  case "$1" in
    --remove-binary)
      shift
      ;;
    --purge-data)
      PURGE_DATA=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      print -u2 -- "ERROR: 未知参数：$1"
      exit 2
      ;;
  esac
done

[[ "$(uname -s)" == "Darwin" ]] || { print -u2 -- "ERROR: 此脚本只支持 macOS"; exit 1; }

unregister_app_background_services() {
  local executable="$APP_PATH/Contents/MacOS/AgentDock"
  if [[ ! -e "$APP_PATH" ]]; then
    return 0
  fi
  [[ -d "$APP_PATH" && ! -L "$APP_PATH" ]] || die "AgentDock.app 路径不是普通应用目录：$APP_PATH"
  [[ -f "$executable" && -x "$executable" && ! -L "$executable" ]] || die "AgentDock.app 后台服务注销入口不存在或不可执行：$executable"
  local unregister_error
  unregister_error="$("$executable" --unregister-background-services 2>&1)" || \
    die "注销 AgentDock.app 后台服务失败：${unregister_error:-unknown error}"
}

remove_app_bundle() {
  if [[ ! -e "$APP_PATH" ]]; then
    return 0
  fi
  [[ -d "$APP_PATH" && ! -L "$APP_PATH" ]] || die "AgentDock.app 路径不是普通应用目录：$APP_PATH"
  [[ "${APP_PATH:t}" == *.app ]] || die "拒绝删除非 .app 路径：$APP_PATH"
  rm -rf "$APP_PATH"
  [[ ! -e "$APP_PATH" ]] || die "AgentDock.app 删除失败：$APP_PATH"
  print -- "removed app: $APP_PATH"
}

stop_launch_agent() {
  local domain="$1"
  local label="$2"
  local bootout_error
  if launchctl print "$domain/$label" >/dev/null 2>&1; then
    bootout_error="$(launchctl bootout "$domain/$label" 2>&1)" || die "停止 LaunchAgent 失败：$label ${bootout_error:-unknown error}"
    if launchctl print "$domain/$label" >/dev/null 2>&1; then
      die "LaunchAgent 仍在运行，未删除任何服务文件：$label"
    fi
  fi
}

domain="gui/$(id -u)"
unregister_app_background_services
if [[ -x "$BINARY_PATH" && "$("$BINARY_PATH" install --engine-ready 2>/dev/null || true)" == *agentdock-installer-engine* ]]; then
  # 不传 --purge-data：install-root 是 ~/.local/bin，不能整目录删掉。
  "$BINARY_PATH" uninstall \
    --install-root "$(dirname "$BINARY_PATH")" \
    --runtime-root "$APP_SUPPORT_DIR" \
    --launch-agents-dir "$(dirname "$PLIST_PATH")" >/dev/null || \
    die "Go Installer Engine 卸载失败"
fi
stop_launch_agent "$domain" "$TUNNEL_LABEL"
stop_launch_agent "$domain" "$LABEL"
rm -f "$PLIST_PATH" "$TUNNEL_PLIST_PATH"
rm -rf "$APP_SUPPORT_DIR" "$LOG_DIR"
remove_app_bundle
print -- "removed service: $LABEL"

rm -f "$BINARY_PATH" "$CLOUDFLARED_BINARY_PATH"
print -- "removed binary: $BINARY_PATH"
print -- "removed binary: $CLOUDFLARED_BINARY_PATH"

if [[ "$PURGE_DATA" == true ]]; then
  rm -rf "$STATE_DIR" "$WORK_DIR"
  print -- "removed data: $STATE_DIR"
  print -- "removed work directory: $WORK_DIR"
else
  print -- "preserved data: $STATE_DIR"
  print -- "preserved work directory: $WORK_DIR"
fi
