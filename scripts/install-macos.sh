#!/bin/zsh
set -euo pipefail

RELEASE_VERSION="${AGENTDOCK_RELEASE_VERSION:-latest}"
INSTALL_DIR="${AGENTDOCK_INSTALL_DIR:-$HOME/.local/bin}"
BACKUP_DIR="${AGENTDOCK_BACKUP_DIR:-$HOME/.agentdock/backups/bin}"
RELEASE_BASE_URL="${AGENTDOCK_RELEASE_BASE_URL:-}"
REGISTER_SERVICE=false
NO_START=false
SERVICE_HOST="${AGENTDOCK_HOST:-127.0.0.1}"
SERVICE_PORT="${AGENTDOCK_PORT:-8765}"
SERVICE_LOG_LEVEL="${AGENTDOCK_LOG_LEVEL:-info}"
AUTH_TOKEN_ARG=""
HOST_EXPLICIT=false
PORT_EXPLICIT=false

LABEL="com.uvwt.agentdock"
APP_SUPPORT_DIR="$HOME/Library/Application Support/AgentDock"
AGENTDOCK_ENV="$APP_SUPPORT_DIR/agentdock.env"
START_SCRIPT="$APP_SUPPORT_DIR/start-agentdock.sh"
LAUNCH_AGENTS_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$LAUNCH_AGENTS_DIR/$LABEL.plist"
LOG_DIR="$HOME/Library/Logs/AgentDock"
STDOUT_LOG="$LOG_DIR/agentdock.out.log"
STDERR_LOG="$LOG_DIR/agentdock.err.log"
WORK_DIR="$HOME/AgentDock"
STATE_DIR="$HOME/.agentdock"
TARGET="$INSTALL_DIR/agentdock"
SERVICE_WAS_LOADED=false
PREVIOUS_SERVICE_PID=""
PREVIOUS_SERVICE_STOPPED=false
SERVICE_BACKUP_DIR=""

usage() {
  cat <<'USAGE'
AgentDock macOS 预编译版本安装脚本。

用法：
  zsh install-macos.sh [选项]

选项：
  --version latest|vX.Y.Z  Release 版本，默认 latest
  --install-dir PATH       二进制安装目录，默认 ~/.local/bin
  --register-service       生成、注册并启动用户级 LaunchAgent
  --host HOST              服务监听地址，默认 127.0.0.1
  --port PORT              服务监听端口，默认 8765
  --auth-token TOKEN       首次创建 agentdock.env 时写入 Token；已有 Token 永不覆盖
  --no-start               只生成服务文件和 plist，不加载或启动 LaunchAgent
  -h, --help               显示帮助

环境变量：
  AGENTDOCK_RELEASE_VERSION   Release 版本，默认 latest
  AGENTDOCK_INSTALL_DIR       二进制安装目录，默认 ~/.local/bin
  AGENTDOCK_BACKUP_DIR        旧二进制备份目录，默认 ~/.agentdock/backups/bin
  AGENTDOCK_RELEASE_BASE_URL  自定义 Release 下载根地址，主要用于镜像或测试

服务文件：
  ~/Library/Application Support/AgentDock/agentdock.env
  ~/Library/Application Support/AgentDock/start-agentdock.sh
  ~/Library/LaunchAgents/com.uvwt.agentdock.plist
  ~/Library/Logs/AgentDock/
USAGE
}

die() {
  print -u2 -- "ERROR: $*"
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

validate_port() {
  [[ "$1" == <1-65535> ]] || die "端口必须是 1-65535：$1"
}

release_url() {
  if [[ -n "$RELEASE_BASE_URL" ]]; then
    print -r -- "${RELEASE_BASE_URL%/}"
    return
  fi

  if [[ "$RELEASE_VERSION" == "latest" ]]; then
    print -r -- "https://github.com/uvwt/agentdock/releases/latest/download"
    return
  fi

  local normalized="$RELEASE_VERSION"
  [[ "$normalized" == v* ]] || normalized="v$normalized"
  print -r -- "https://github.com/uvwt/agentdock/releases/download/$normalized"
}

next_backup_path() {
  local base="$BACKUP_DIR/agentdock.$(date +%Y%m%d%H%M%S)"
  local candidate="$base"
  local suffix=1
  while [[ -e "$candidate" ]]; do
    candidate="$base.$suffix"
    (( suffix++ ))
  done
  print -r -- "$candidate"
}

write_env_key() {
  local key="$1"
  local value="$2"
  local replace_existing="$3"
  local quoted
  local pattern="^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*="
  printf -v quoted '%q' "$value"

  if grep -Eq "$pattern" "$AGENTDOCK_ENV"; then
    local match_count="$(grep -Ec "$pattern" "$AGENTDOCK_ENV")"
    if [[ "$replace_existing" == false && "$match_count" == 1 ]]; then
      return 0
    fi

    # 兼容用户已有的 `export KEY=...` 和重复定义。显式替换时写入规范键；
    # 仅去重时保留最后一条原始定义，维持 shell source 的既有效果和行序。
    local tmp_file="$AGENTDOCK_ENV.tmp.$$"
    local line
    local written=false
    local remaining="$match_count"
    : > "$tmp_file"
    while IFS= read -r line || [[ -n "$line" ]]; do
      if print -r -- "$line" | grep -Eq "$pattern"; then
        remaining=$(( remaining - 1 ))
        if [[ "$replace_existing" == true && "$written" == false ]]; then
          printf '%s=%s\n' "$key" "$quoted" >> "$tmp_file"
          written=true
        elif [[ "$replace_existing" == false && "$remaining" == 0 ]]; then
          printf '%s\n' "$line" >> "$tmp_file"
        fi
      else
        printf '%s\n' "$line" >> "$tmp_file"
      fi
    done < "$AGENTDOCK_ENV"
    chmod 0600 "$tmp_file"
    mv -f "$tmp_file" "$AGENTDOCK_ENV"
    return 0
  fi

  printf '%s=%s\n' "$key" "$quoted" >> "$AGENTDOCK_ENV"
}

snapshot_service_file() {
  local name="$1"
  local file_path="$2"
  if [[ -e "$file_path" || -L "$file_path" ]]; then
    [[ -f "$file_path" && ! -L "$file_path" ]] || die "服务文件必须是普通文件：$file_path"
    cp -p "$file_path" "$SERVICE_BACKUP_DIR/$name" || return 1
    : > "$SERVICE_BACKUP_DIR/$name.present" || return 1
  fi
}

snapshot_service_files() {
  SERVICE_BACKUP_DIR="$tmp_dir/service-files"
  mkdir -p "$SERVICE_BACKUP_DIR" || return 1
  snapshot_service_file agentdock.env "$AGENTDOCK_ENV" || return 1
  snapshot_service_file start-agentdock.sh "$START_SCRIPT" || return 1
  snapshot_service_file launch-agent.plist "$PLIST_PATH" || return 1
}

restore_service_file() {
  local name="$1"
  local file_path="$2"
  if [[ -f "$SERVICE_BACKUP_DIR/$name.present" ]]; then
    mkdir -p "${file_path:h}" || return 1
    local restore_tmp="$file_path.restore.$$"
    cp -p "$SERVICE_BACKUP_DIR/$name" "$restore_tmp" || return 1
    mv -f "$restore_tmp" "$file_path" || return 1
  else
    rm -f "$file_path" || return 1
  fi
}

restore_service_files() {
  restore_service_file agentdock.env "$AGENTDOCK_ENV" || return 1
  restore_service_file start-agentdock.sh "$START_SCRIPT" || return 1
  restore_service_file launch-agent.plist "$PLIST_PATH" || return 1
}

write_service_env() {
  if [[ -e "$APP_SUPPORT_DIR" || -L "$APP_SUPPORT_DIR" ]]; then
    [[ -d "$APP_SUPPORT_DIR" && ! -L "$APP_SUPPORT_DIR" ]] || die "服务配置目录必须是普通目录：$APP_SUPPORT_DIR"
  fi
  mkdir -p "$APP_SUPPORT_DIR"
  chmod 0700 "$APP_SUPPORT_DIR"
  if [[ ! -f "$AGENTDOCK_ENV" ]]; then
    umask 077
    cat > "$AGENTDOCK_ENV" <<'ENV'
# AgentDock macOS LaunchAgent 的唯一服务配置文件。
# 修改后执行 launchctl kickstart -k "gui/$(id -u)/com.uvwt.agentdock" 使配置生效。
ENV
  fi
  [[ -f "$AGENTDOCK_ENV" && ! -L "$AGENTDOCK_ENV" ]] || die "agentdock.env 必须是普通文件：$AGENTDOCK_ENV"
  chmod 0600 "$AGENTDOCK_ENV"

  write_env_key AGENTDOCK_HOST "$SERVICE_HOST" "$HOST_EXPLICIT"
  write_env_key AGENTDOCK_PORT "$SERVICE_PORT" "$PORT_EXPLICIT"
  write_env_key AGENTDOCK_LOG_LEVEL "$SERVICE_LOG_LEVEL" false
  write_env_key AGENTDOCK_AUTH_TOKEN "$AUTH_TOKEN_ARG" false
  write_env_key AGENTDOCK_NEXUS_ENDPOINT "${AGENTDOCK_NEXUS_ENDPOINT:-}" false
  write_env_key AGENTDOCK_NEXUS_TOKEN "${AGENTDOCK_NEXUS_TOKEN:-}" false

  # 稳定签名参数只在调用方明确提供时写入，避免把任何本机证书路径写死进安装器。
  if [[ -n "${AGENTDOCK_CODESIGN_IDENTITY:-}" ]]; then
    write_env_key AGENTDOCK_CODESIGN_IDENTITY "$AGENTDOCK_CODESIGN_IDENTITY" false
  fi
  if [[ -n "${AGENTDOCK_CODESIGN_KEYCHAIN:-}" ]]; then
    write_env_key AGENTDOCK_CODESIGN_KEYCHAIN "$AGENTDOCK_CODESIGN_KEYCHAIN" false
  fi
  if [[ -n "${AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD:-}" ]]; then
    write_env_key AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD "$AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD" false
  fi
  if [[ -n "${AGENTDOCK_CODESIGN_IDENTIFIER:-}" ]]; then
    write_env_key AGENTDOCK_CODESIGN_IDENTIFIER "$AGENTDOCK_CODESIGN_IDENTIFIER" false
  fi
  if [[ -n "${AGENTDOCK_CODESIGN_HOME:-}" ]]; then
    write_env_key AGENTDOCK_CODESIGN_HOME "$AGENTDOCK_CODESIGN_HOME" false
  fi
  chmod 0600 "$AGENTDOCK_ENV"
}

write_start_script() {
  if [[ -e "$START_SCRIPT" || -L "$START_SCRIPT" ]]; then
    [[ -f "$START_SCRIPT" && ! -L "$START_SCRIPT" ]] || die "启动脚本必须是普通文件：$START_SCRIPT"
  fi
  cat > "$START_SCRIPT" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail

USER_HOME="$HOME"
APP_SUPPORT_DIR="$USER_HOME/Library/Application Support/AgentDock"
AGENTDOCK_ENV="$APP_SUPPORT_DIR/agentdock.env"
[[ -r "$AGENTDOCK_ENV" ]] || { print -u2 -- "AgentDock agentdock.env 不可读：$AGENTDOCK_ENV"; exit 1; }

set -a
source "$AGENTDOCK_ENV"
set +a

# 服务配置只提供 AgentDock 参数，不得改变 LaunchAgent 的用户目录或命令搜索路径。
export HOME="$USER_HOME"
export PATH="$USER_HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
exec "$USER_HOME/.local/bin/agentdock" \
  --host "${AGENTDOCK_HOST:-127.0.0.1}" \
  --port "${AGENTDOCK_PORT:-8765}" \
  --log-level "${AGENTDOCK_LOG_LEVEL:-info}"
SCRIPT
  chmod 0700 "$START_SCRIPT"
}

xml_escape() {
  print -nr -- "$1" | sed \
    -e 's/&/\&amp;/g' \
    -e 's/</\&lt;/g' \
    -e 's/>/\&gt;/g' \
    -e 's/"/\&quot;/g' \
    -e "s/'/\&apos;/g"
}

write_launch_agent() {
  mkdir -p "$LAUNCH_AGENTS_DIR" "$LOG_DIR" "$WORK_DIR" "$STATE_DIR"
  chmod 0700 "$LOG_DIR" "$WORK_DIR" "$STATE_DIR"
  touch "$STDOUT_LOG" "$STDERR_LOG"
  chmod 0600 "$STDOUT_LOG" "$STDERR_LOG"

  local plist_tmp="$PLIST_PATH.tmp.$$"
  local start_script_xml="$(xml_escape "$START_SCRIPT")"
  local work_dir_xml="$(xml_escape "$WORK_DIR")"
  local stdout_xml="$(xml_escape "$STDOUT_LOG")"
  local stderr_xml="$(xml_escape "$STDERR_LOG")"
  cat > "$plist_tmp" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$start_script_xml</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$work_dir_xml</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$stdout_xml</string>
  <key>StandardErrorPath</key>
  <string>$stderr_xml</string>
</dict>
</plist>
PLIST
  plutil -lint "$plist_tmp" >/dev/null
  chmod 0600 "$plist_tmp"
  mv -f "$plist_tmp" "$PLIST_PATH"
}

launchd_pid() {
  local domain="$1"
  local output
  output="$(launchctl print "$domain/$LABEL" 2>/dev/null)" || return 1
  print -r -- "$output" | sed -n 's/^[[:space:]]*pid = \([0-9][0-9]*\).*$/\1/p' | head -n 1
}

stop_service_if_loaded() {
  local domain="$1"
  local bootout_output
  if ! launchctl print "$domain/$LABEL" >/dev/null 2>&1; then
    return 0
  fi
  if ! bootout_output="$(launchctl bootout "$domain/$LABEL" 2>&1)"; then
    print -u2 -- "停止 LaunchAgent 失败：$LABEL ${bootout_output:-unknown error}"
    return 1
  fi
  if launchctl print "$domain/$LABEL" >/dev/null 2>&1; then
    print -u2 -- "LaunchAgent 在 bootout 后仍处于加载状态：$LABEL"
    return 1
  fi
}

prepare_service_rollback() {
  local domain="$1"
  if [[ "$SERVICE_WAS_LOADED" == true && "$PREVIOUS_SERVICE_STOPPED" == false ]]; then
    # 原服务从未成功停止，保持它继续运行；磁盘文件恢复后再按旧地址验证。
    return 0
  fi
  stop_service_if_loaded "$domain"
}

read_service_address() {
  local env_file="$1"
  [[ -f "$env_file" && ! -L "$env_file" ]] || return 1
  /bin/zsh -c '
    set -e
    unset AGENTDOCK_HOST AGENTDOCK_PORT
    source "$1" >/dev/null
    printf "%s\t%s\n" "${AGENTDOCK_HOST:-127.0.0.1}" "${AGENTDOCK_PORT:-8765}"
  ' _ "$env_file"
}

health_host() {
  local service_host="$1"
  case "$service_host" in
    0.0.0.0|::) print -r -- "127.0.0.1" ;;
    *:*) print -r -- "[$service_host]" ;;
    *) print -r -- "$service_host" ;;
  esac
}

normalize_version() {
  print -r -- "${1#v}"
}

wait_for_service() {
  local domain="$1"
  local previous_pid="$2"
  local expected_version="$3"
  local service_host="$4"
  local service_port="$5"
  local pid=""
  local host="$(health_host "$service_host")"
  local health_url="http://$host:$service_port/healthz"
  local attempts=60

  while (( attempts-- > 0 )); do
    pid="$(launchd_pid "$domain" || true)"
    if [[ -n "$pid" && "$pid" != "0" && "$pid" != "$previous_pid" ]]; then
      local process_command
      local listeners
      process_command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
      listeners="$(lsof -nP -iTCP:"$service_port" -sTCP:LISTEN -t 2>/dev/null || true)"
      if [[ "$process_command" == "$TARGET" || "$process_command" == "$TARGET "* ]] && print -r -- "$listeners" | grep -qx "$pid"; then
        local health_body
        health_body="$(curl -fsS --max-time 2 "$health_url" 2>/dev/null || true)"
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

  print -u2 -- "LaunchAgent 验证失败：未确认新 PID、端口监听和目标版本 healthz"
  return 1
}

register_and_start_service() {
  local domain="gui/$(id -u)"
  local previous_pid="$PREVIOUS_SERVICE_PID"
  local expected_version service_address service_host service_port
  expected_version="$("$TARGET" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
  [[ -n "$expected_version" ]] || { print -u2 -- "无法读取目标二进制版本"; return 1; }
  service_address="$(read_service_address "$AGENTDOCK_ENV")" || { print -u2 -- "无法读取新服务监听地址：$AGENTDOCK_ENV"; return 1; }
  service_host="${service_address%%$'\t'*}"
  service_port="${service_address#*$'\t'}"
  [[ "$service_port" == <1-65535> ]] || { print -u2 -- "新服务端口无效：$service_port"; return 1; }

  stop_service_if_loaded "$domain" || return 1
  if [[ "$SERVICE_WAS_LOADED" == true ]]; then
    PREVIOUS_SERVICE_STOPPED=true
  fi
  if ! launchctl bootstrap "$domain" "$PLIST_PATH"; then
    print -u2 -- "无法加载新 LaunchAgent：$PLIST_PATH"
    return 1
  fi
  if ! launchctl kickstart -k "$domain/$LABEL"; then
    print -u2 -- "无法启动新 LaunchAgent：$LABEL"
    return 1
  fi

  local new_pid
  new_pid="$(wait_for_service "$domain" "$previous_pid" "$expected_version" "$service_host" "$service_port")" || return 1
  print -- "==> LaunchAgent 已启动：label=$LABEL pid=$new_pid port=$service_port version=$expected_version"
}

restore_previous_service() {
  local old_version="$1"
  local failed_pid="$2"
  local domain="gui/$(id -u)"
  local old_address old_host old_port

  if [[ "$SERVICE_WAS_LOADED" == false ]]; then
    return 0
  fi
  [[ -f "$PLIST_PATH" && ! -L "$PLIST_PATH" ]] || return 1
  [[ -f "$AGENTDOCK_ENV" && ! -L "$AGENTDOCK_ENV" ]] || return 1
  [[ -n "$old_version" ]] || return 1

  # 回滚验证必须使用旧 env 的监听地址，不能沿用本次安装请求的新 host/port。
  old_address="$(read_service_address "$AGENTDOCK_ENV")" || return 1
  old_host="${old_address%%$'\t'*}"
  old_port="${old_address#*$'\t'}"
  [[ "$old_port" == <1-65535> ]] || return 1

  if [[ "$PREVIOUS_SERVICE_STOPPED" == false ]]; then
    # 第一次 bootout 已失败时，旧进程仍在运行；恢复磁盘文件后直接验证，避免二次中断。
    wait_for_service "$domain" "" "$old_version" "$old_host" "$old_port" >/dev/null
    return
  fi

  launchctl bootstrap "$domain" "$PLIST_PATH" || return 1
  launchctl kickstart -k "$domain/$LABEL" || return 1
  wait_for_service "$domain" "$failed_pid" "$old_version" "$old_host" "$old_port" >/dev/null
}

while (( $# > 0 )); do
  case "$1" in
    --version)
      (( $# >= 2 )) || die "--version 需要值"
      RELEASE_VERSION="$2"
      shift 2
      ;;
    --install-dir)
      (( $# >= 2 )) || die "--install-dir 需要值"
      INSTALL_DIR="$2"
      TARGET="$INSTALL_DIR/agentdock"
      shift 2
      ;;
    --register-service)
      REGISTER_SERVICE=true
      shift
      ;;
    --host)
      (( $# >= 2 )) || die "--host 需要值"
      SERVICE_HOST="$2"
      HOST_EXPLICIT=true
      shift 2
      ;;
    --port)
      (( $# >= 2 )) || die "--port 需要值"
      SERVICE_PORT="$2"
      PORT_EXPLICIT=true
      shift 2
      ;;
    --auth-token)
      (( $# >= 2 )) || die "--auth-token 需要值"
      AUTH_TOKEN_ARG="$2"
      shift 2
      ;;
    --no-start)
      NO_START=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "未知参数：$1"
      ;;
  esac
done

[[ "$(uname -s)" == "Darwin" ]] || die "此脚本只支持 macOS"
validate_port "$SERVICE_PORT"
[[ "$NO_START" == false || "$REGISTER_SERVICE" == true ]] || die "--no-start 必须与 --register-service 一起使用"
if [[ "$REGISTER_SERVICE" == true && "$INSTALL_DIR" != "$HOME/.local/bin" ]]; then
  die "注册 LaunchAgent 时二进制必须安装到 $HOME/.local/bin"
fi
if [[ -e "$TARGET" || -L "$TARGET" ]]; then
  [[ -f "$TARGET" && ! -L "$TARGET" ]] || die "现有安装目标必须是普通文件：$TARGET"
fi

for command_name in chmod cp curl grep install mkdir mktemp mv rm sed shasum tar touch uname; do
  require_command "$command_name"
done
if [[ "$REGISTER_SERVICE" == true ]]; then
  for command_name in launchctl plutil; do
    require_command "$command_name"
  done
  if [[ "$NO_START" == false ]]; then
    require_command lsof
    require_command ps
  fi

  if [[ "$NO_START" == false ]]; then
    domain="gui/$(id -u)"
    if launchctl print "$domain/$LABEL" >/dev/null 2>&1; then
      SERVICE_WAS_LOADED=true
      PREVIOUS_SERVICE_PID="$(launchd_pid "$domain" || true)"
      [[ -f "$PLIST_PATH" && ! -L "$PLIST_PATH" ]] || die "已有 LaunchAgent 正在加载，但标准 plist 不可用：$PLIST_PATH"
      [[ -x "$TARGET" && ! -L "$TARGET" ]] || die "已有 LaunchAgent 正在加载，但当前生产二进制不可用：$TARGET"
    fi
  fi
fi

case "$(uname -m)" in
  arm64|aarch64) release_arch="arm64" ;;
  x86_64|amd64) release_arch="amd64" ;;
  *) die "不支持的 macOS 架构：$(uname -m)" ;;
esac

asset="agentdock_darwin_${release_arch}.tar.gz"
base_url="$(release_url)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-install.XXXXXX")"
staged_target=""
cleanup() {
  rm -rf "$tmp_dir"
  [[ -z "$staged_target" ]] || rm -f "$staged_target"
}
trap cleanup EXIT

if [[ "$REGISTER_SERVICE" == true ]]; then
  snapshot_service_files
fi

print -- "==> 下载 $asset"
curl -fL --retry 3 --retry-delay 1 "$base_url/$asset" -o "$tmp_dir/$asset"
curl -fL --retry 3 --retry-delay 1 "$base_url/$asset.sha256" -o "$tmp_dir/$asset.sha256"

print -- "==> 校验 SHA-256"
(
  cd "$tmp_dir"
  shasum -a 256 -c "$asset.sha256"
)

mkdir -p "$tmp_dir/extract"
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir/extract"
source_binary="$tmp_dir/extract/bin/agentdock"
[[ -f "$source_binary" && ! -L "$source_binary" ]] || die "Release 压缩包中的 bin/agentdock 必须是普通文件"

mkdir -p "$INSTALL_DIR" "$STATE_DIR" "$WORK_DIR" "$BACKUP_DIR"
chmod 0700 "$STATE_DIR" "$WORK_DIR" "$BACKUP_DIR"

backup=""
old_version=""
if [[ -f "$TARGET" ]]; then
  backup="$(next_backup_path)"
  cp -p "$TARGET" "$backup"
  old_version="$("$TARGET" --version 2>/dev/null | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p' || true)"
  if [[ "$SERVICE_WAS_LOADED" == true && -z "$old_version" ]]; then
    die "已有 LaunchAgent 正在运行，但无法读取当前生产二进制版本；未替换任何文件"
  fi
  print -- "==> 已备份旧版本到 $backup"
fi

staged_target="$INSTALL_DIR/.agentdock.install.$$"
rm -f "$staged_target"
install -m 0755 "$source_binary" "$staged_target"
"$staged_target" --help >/dev/null 2>&1
mv -f "$staged_target" "$TARGET"

if [[ "$REGISTER_SERVICE" == true ]]; then
  if ! (
    write_service_env
    write_start_script
    write_launch_agent
  ); then
    if ! restore_service_files; then
      die "生成服务文件失败，且旧服务文件恢复失败"
    fi
    if [[ -n "$backup" && -f "$backup" ]]; then
      cp -p "$backup" "$staged_target" || die "生成服务文件失败，且旧二进制复制失败；备份保留在 $backup"
      mv -f "$staged_target" "$TARGET" || die "生成服务文件失败，且旧二进制恢复失败；备份保留在 $backup"
    else
      rm -f "$TARGET"
    fi
    die "生成服务文件失败；已恢复安装前状态"
  fi

  if [[ "$NO_START" == false ]]; then
    if ! register_and_start_service; then
      print -u2 -- "==> 新服务验证失败，恢复安装前状态"
      domain="gui/$(id -u)"
      failed_pid="$(launchd_pid "$domain" || true)"

      # 若新服务已部分加载，必须先确认它停止，再恢复或删除磁盘文件。
      # 原服务从未停止时则保持运行，只恢复安装过程中改写的文件。
      if ! prepare_service_rollback "$domain"; then
        die "新服务验证失败，且无法安全停止部分加载的 LaunchAgent；已保留当前运行文件"
      fi
      if ! restore_service_files; then
        die "新服务验证失败，且旧服务文件恢复失败"
      fi
      if [[ -n "$backup" && -f "$backup" ]]; then
        cp -p "$backup" "$staged_target" || die "新服务验证失败，且旧二进制复制失败；备份保留在 $backup"
        mv -f "$staged_target" "$TARGET" || die "新服务验证失败，且旧二进制恢复失败；备份保留在 $backup"
        if ! restore_previous_service "$old_version" "$failed_pid"; then
          die "新服务验证失败；旧文件已恢复，但旧 LaunchAgent 恢复验证失败，备份保留在 $backup"
        fi
        print -u2 -- "==> 已恢复安装前二进制、服务文件和 LaunchAgent"
      else
        rm -f "$TARGET"
      fi
      exit 1
    fi
  else
    print -- "==> 已生成服务文件和 plist，按 --no-start 要求未加载 LaunchAgent"
  fi
fi

print -- "installed: $TARGET"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  print -- "PATH 尚未包含 $INSTALL_DIR，可执行："
  print -- "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zprofile"
  print -- "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi

cat <<STATUS

状态目录：
  $STATE_DIR
  $WORK_DIR
STATUS
if [[ "$REGISTER_SERVICE" == true ]]; then
  cat <<STATUS
服务配置：
  $AGENTDOCK_ENV
LaunchAgent：
  $PLIST_PATH
日志目录：
  $LOG_DIR
STATUS
else
  cat <<STATUS

启动示例：
  $TARGET --host 127.0.0.1 --port 8765

注册后台服务：
  zsh install-macos.sh --register-service
STATUS
fi
