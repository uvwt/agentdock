#!/bin/zsh
set -euo pipefail

EXECUTION_HOME="$HOME"
EXECUTION_PATH="$PATH"

[[ "$(uname -s)" == "Darwin" ]] || { print -u2 -- "ERROR: 此脚本只支持 macOS"; exit 1; }

SCRIPT_DIR="${0:A:h}"
SRC_DIR="${SCRIPT_DIR:h}"
LABEL="com.uvwt.agentdock"
TARGET="$HOME/.local/bin/agentdock"
BACKUP_DIR="$HOME/.agentdock/backups/bin"
APP_SUPPORT_DIR="$HOME/Library/Application Support/AgentDock"
AGENTDOCK_ENV="$APP_SUPPORT_DIR/agentdock.env"
START_SCRIPT="$APP_SUPPORT_DIR/start-agentdock.sh"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
SIGN_SCRIPT="$SCRIPT_DIR/sign-macos.sh"
INSTALL_DIR="${TARGET:h}"
STAMP="$(date +%Y%m%d%H%M%S)"
TMP_BIN="$INSTALL_DIR/.agentdock.source.$STAMP.$$"
ROLLBACK_BIN="$INSTALL_DIR/.agentdock.rollback.$STAMP.$$"
CORE_SKILL_TEMP_DIR=""
CORE_SKILL_BUNDLE=""

EXPLICIT_SIGN_IDENTITY="${AGENTDOCK_CODESIGN_IDENTITY:-}"
EXPLICIT_SIGN_KEYCHAIN="${AGENTDOCK_CODESIGN_KEYCHAIN:-}"
EXPLICIT_SIGN_KEYCHAIN_PASSWORD="${AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD:-}"
EXPLICIT_SIGN_IDENTIFIER="${AGENTDOCK_CODESIGN_IDENTIFIER:-}"
EXPLICIT_SIGN_HOME="${AGENTDOCK_CODESIGN_HOME:-}"

cleanup() {
  rm -f "$TMP_BIN" "$ROLLBACK_BIN"
  [[ -z "$CORE_SKILL_TEMP_DIR" ]] || rm -rf "$CORE_SKILL_TEMP_DIR"
}
trap cleanup EXIT

die() {
  print -u2 -- "ERROR: $*"
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

next_backup_path() {
  local base="$BACKUP_DIR/agentdock.$STAMP"
  local candidate="$base"
  local suffix=1
  while [[ -e "$candidate" ]]; do
    candidate="$base.$suffix"
    (( suffix++ ))
  done
  print -r -- "$candidate"
}

normalize_version() {
  print -r -- "${1#v}"
}

launchd_pid() {
  local domain="$1"
  local output
  output="$(launchctl print "$domain/$LABEL" 2>/dev/null)" || return 1
  print -r -- "$output" | sed -n 's/^[[:space:]]*pid = \([0-9][0-9]*\).*$/\1/p' | head -n 1
}

health_host() {
  case "$AGENTDOCK_HOST" in
    0.0.0.0|::) print -r -- "127.0.0.1" ;;
    *:*) print -r -- "[$AGENTDOCK_HOST]" ;;
    *) print -r -- "$AGENTDOCK_HOST" ;;
  esac
}

wait_for_service() {
  local domain="$1"
  local previous_pid="$2"
  local expected_version="$3"
  local host="$(health_host)"
  local health_url="http://$host:$AGENTDOCK_PORT/healthz"
  local attempts=60

  while (( attempts-- > 0 )); do
    local pid="$(launchd_pid "$domain" || true)"
    if [[ -n "$pid" && "$pid" != "0" && "$pid" != "$previous_pid" ]]; then
      local process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
      local listeners="$(lsof -nP -iTCP:"$AGENTDOCK_PORT" -sTCP:LISTEN -t 2>/dev/null || true)"
      if [[ "$process_command" == "$TARGET" || "$process_command" == "$TARGET "* ]] && print -r -- "$listeners" | grep -qx "$pid"; then
        local health_body="$(curl -fsS --max-time 2 "$health_url" 2>/dev/null || true)"
        local health_ok=false
        local health_version
        if print -r -- "$health_body" | grep -Eq '"ok"[[:space:]]*:[[:space:]]*true'; then
          health_ok=true
        fi
        health_version="$(print -r -- "$health_body" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
        if [[ "$health_ok" == true && "$(normalize_version "$health_version")" == "$(normalize_version "$expected_version")" ]]; then
          print -r -- "$pid"
          return 0
        fi
      fi
    fi
    sleep 0.5
  done

  print -u2 -- "未确认 LaunchAgent 新 PID、目标二进制、端口监听和目标版本 healthz"
  return 1
}

restart_and_verify() {
  local expected_version="$1"
  local previous_pid="$2"
  local domain="gui/$(id -u)"
  launchctl kickstart -k "$domain/$LABEL"
  wait_for_service "$domain" "$previous_pid" "$expected_version"
}

restore_previous_binary() {
  local backup="$1"
  local old_version="$2"
  local failed_pid="$(launchd_pid "gui/$(id -u)" || true)"

  cp -p "$backup" "$ROLLBACK_BIN"
  chmod 0755 "$ROLLBACK_BIN"
  mv -f "$ROLLBACK_BIN" "$TARGET"
  local rollback_pid
  rollback_pid="$(restart_and_verify "$old_version" "$failed_pid")" || return 1
  print -u2 -- "已恢复旧二进制并重新启动：pid=$rollback_pid version=$old_version"
}

for command_name in curl git go gofmt grep launchctl lsof plutil ps python3 sed; do
  require_command "$command_name"
done

[[ -d "$SRC_DIR/.git" ]] || die "源码目录不是 Git 仓库：$SRC_DIR"
[[ -f "$AGENTDOCK_ENV" && ! -L "$AGENTDOCK_ENV" ]] || die "缺少标准服务配置：$AGENTDOCK_ENV"
[[ -x "$START_SCRIPT" && ! -L "$START_SCRIPT" ]] || die "缺少标准启动脚本：$START_SCRIPT"
[[ -f "$PLIST_PATH" && ! -L "$PLIST_PATH" ]] || die "缺少标准 LaunchAgent：$PLIST_PATH"
[[ -x "$SIGN_SCRIPT" && ! -L "$SIGN_SCRIPT" ]] || die "缺少仓库签名脚本：$SIGN_SCRIPT"
[[ -x "$TARGET" && ! -L "$TARGET" ]] || die "缺少当前生产二进制：$TARGET"
plutil -lint "$PLIST_PATH" >/dev/null

source_status="$(git -C "$SRC_DIR" status --porcelain --untracked-files=normal)"
[[ -z "$source_status" ]] || die "源码工作区不干净，拒绝部署无法追溯的构建：\n$source_status"

grep -Fq 'exec "$HOME/.local/bin/agentdock"' "$START_SCRIPT" || die "启动脚本未指向标准二进制：$START_SCRIPT"
domain="gui/$(id -u)"
old_pid="$(launchd_pid "$domain" || true)"
[[ -n "$old_pid" && "$old_pid" != "0" ]] || die "LaunchAgent 未运行：$domain/$LABEL"

set -a
source "$AGENTDOCK_ENV"
set +a
export HOME="$EXECUTION_HOME"
export PATH="$EXECUTION_PATH"
: "${AGENTDOCK_HOST:=127.0.0.1}"
: "${AGENTDOCK_PORT:=8765}"
: "${AGENTDOCK_LOG_LEVEL:=info}"

# 命令行环境用于一次性部署时优先级最高；agentdock.env 负责让后续自更新继承同一配置。
[[ -n "$EXPLICIT_SIGN_IDENTITY" ]] && export AGENTDOCK_CODESIGN_IDENTITY="$EXPLICIT_SIGN_IDENTITY"
[[ -n "$EXPLICIT_SIGN_KEYCHAIN" ]] && export AGENTDOCK_CODESIGN_KEYCHAIN="$EXPLICIT_SIGN_KEYCHAIN"
[[ -n "$EXPLICIT_SIGN_KEYCHAIN_PASSWORD" ]] && export AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD="$EXPLICIT_SIGN_KEYCHAIN_PASSWORD"
[[ -n "$EXPLICIT_SIGN_IDENTIFIER" ]] && export AGENTDOCK_CODESIGN_IDENTIFIER="$EXPLICIT_SIGN_IDENTIFIER"
[[ -n "$EXPLICIT_SIGN_HOME" ]] && export AGENTDOCK_CODESIGN_HOME="$EXPLICIT_SIGN_HOME"
[[ -n "${AGENTDOCK_CODESIGN_IDENTITY:-}" ]] || die "AGENTDOCK_CODESIGN_IDENTITY 不能为空"
[[ "$AGENTDOCK_PORT" == <1-65535> ]] || die "agentdock.env 中端口无效：$AGENTDOCK_PORT"

mkdir -p "$INSTALL_DIR" "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"

cd "$SRC_DIR"
printf '==> source: %s\n' "$SRC_DIR"
printf '==> target: %s\n' "$TARGET"
printf '==> backup_dir: %s\n' "$BACKUP_DIR"

printf '==> running gofmt check\n'
unformatted="$(gofmt -l ./cmd ./internal)"
[[ -z "$unformatted" ]] || die "以下 Go 文件未格式化：\n$unformatted"

printf '==> running tests\n'
go test ./...

printf '==> running go vet\n'
go vet ./...

printf '==> building temporary binary\n'
BUILD_COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
go build -trimpath \
  -ldflags "-X github.com/uvwt/agentdock/cmd/agentdock/internal/buildinfo.Commit=$BUILD_COMMIT -X github.com/uvwt/agentdock/cmd/agentdock/internal/buildinfo.BuildDate=$BUILD_DATE" \
  -o "$TMP_BIN" ./cmd/agentdock
chmod 0755 "$TMP_BIN"

printf '==> building official core Skill Bundle\n'
CORE_SKILL_TEMP_DIR="$(mktemp -d)"
CORE_SKILL_BUNDLE="$CORE_SKILL_TEMP_DIR/core-skills"
python3 "$SCRIPT_DIR/build-core-skill-bundle.py" \
  --repo-root "$SRC_DIR" \
  --output "$CORE_SKILL_BUNDLE"

printf '==> signing temporary binary\n'
"$SIGN_SCRIPT" "$TMP_BIN"

target_version="$("$TMP_BIN" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
old_version="$("$TARGET" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
[[ -n "$target_version" ]] || die "无法读取新二进制版本"
[[ -n "$old_version" ]] || die "无法读取旧二进制版本"

backup="$(next_backup_path)"
cp -p "$TARGET" "$backup"
printf '==> backed up current binary: %s\n' "$backup"

# 临时二进制与目标位于同一目录，签名和版本验证完成后才原子替换生产路径。
mv -f "$TMP_BIN" "$TARGET"
installed_version="$("$TARGET" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
if [[ "$(normalize_version "$installed_version")" != "$(normalize_version "$target_version")" ]]; then
  if ! restore_previous_binary "$backup" "$old_version"; then
    die "原子替换后的版本不匹配，且旧版本恢复验证失败；备份保留在 $backup"
  fi
  die "原子替换后的版本不匹配：期望 $target_version，实际 $installed_version；已恢复旧版本"
fi

printf '==> restarting %s\n' "$LABEL"
if ! new_pid="$(restart_and_verify "$target_version" "$old_pid")"; then
  print -u2 -- "新版本服务验证失败，开始恢复旧二进制"
  if ! restore_previous_binary "$backup" "$old_version"; then
    die "新版本验证失败，且旧版本恢复验证失败；备份保留在 $backup"
  fi
  die "新版本服务验证失败，已恢复旧版本"
fi

printf '==> installing official core Skills\n'
if ! "$TARGET" skill bootstrap --bundle "$CORE_SKILL_BUNDLE"; then
  print -u2 -- "核心 Skill 初始化失败，开始恢复旧二进制"
  if ! restore_previous_binary "$backup" "$old_version"; then
    die "核心 Skill 初始化失败，且旧版本恢复验证失败；备份保留在 $backup"
  fi
  die "核心 Skill 初始化失败，已恢复旧版本"
fi

printf 'installed: %s\n' "$TARGET"
printf 'backup: %s\n' "$backup"
printf 'launchd: %s pid=%s\n' "$LABEL" "$new_pid"
printf 'healthz: host=%s port=%s version=%s\n' "$AGENTDOCK_HOST" "$AGENTDOCK_PORT" "$target_version"
