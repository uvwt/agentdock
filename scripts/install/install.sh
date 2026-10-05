#!/bin/sh
set -eu

# AgentDock Unix 公开 bootstrap。
# 只负责：平台/架构识别、Release 载荷下载与校验、必要 OS bootstrap，随后交给 Go Installer Engine。
# 安装事务、配置保留、服务、health、Skill、Tunnel 与 rollback 只能由 agentdock install/uninstall 决策。

umask 077

DEFAULT_BASE_URL="https://download.nexusdock.co/latest"
CLOUDFLARED_BASE_URL="${AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL:-https://github.com/cloudflare/cloudflared/releases/latest/download}"
OFFICIAL_NEXUS_ENDPOINT="${AGENTDOCK_NEXUS_OFFICIAL_ENDPOINT:-https://mcp.nexusdock.co}"
OFFICIAL_NEXUS_DEVICES_URL="${AGENTDOCK_NEXUS_OFFICIAL_DEVICES_URL:-https://mcp.nexusdock.co/workspace/devices}"
BASE_URL="${AGENTDOCK_INSTALLER_BASE_URL:-$DEFAULT_BASE_URL}"
TMP_ROOT=""
TTY_IN="${AGENTDOCK_TTY_IN:-/dev/tty}"
TTY_OUT="${AGENTDOCK_TTY_OUT:-/dev/tty}"
NONINTERACTIVE="${AGENTDOCK_NONINTERACTIVE:-false}"
UNINSTALL=false
PURGE_CONFIG=false
PURGE_DATA=false
REGISTER_SERVICE=false
NO_START=false
TUNNEL_MODE="${AGENTDOCK_TUNNEL_MODE:-}"
SERVER_URL="${AGENTDOCK_SERVER_URL:-}"
TUNNEL_TOKEN_FILE=""
NEXUS_MODE="${AGENTDOCK_NEXUS_MODE:-}"
NEXUS_ENDPOINT="${AGENTDOCK_NEXUS_ENDPOINT:-}"
NEXUS_PAIR_CODE="${AGENTDOCK_NEXUS_PAIR_CODE:-}"
NEXUS_PAIR_CODE_FILE=""
HOST_VALUE="${AGENTDOCK_HOST:-}"
PORT_VALUE="${AGENTDOCK_PORT:-}"
LOG_LEVEL_VALUE="${AGENTDOCK_LOG_LEVEL:-}"
INSTALLER_AGENTDOCK_HOME="${AGENTDOCK_INSTALLER_AGENTDOCK_HOME:-}"
INSTALLER_DEFAULT_DIR="${AGENTDOCK_INSTALLER_DEFAULT_DIR:-}"
INSTALLER_AGENTDOCK_HOME_EXPLICIT=false
INSTALLER_DEFAULT_DIR_EXPLICIT=false
[ -n "$INSTALLER_AGENTDOCK_HOME" ] && INSTALLER_AGENTDOCK_HOME_EXPLICIT=true
[ -n "$INSTALLER_DEFAULT_DIR" ] && INSTALLER_DEFAULT_DIR_EXPLICIT=true

cleanup() {
  if [ -n "$TMP_ROOT" ] && [ -d "$TMP_ROOT" ]; then
    rm -rf "$TMP_ROOT"
  fi
}
trap cleanup EXIT HUP INT TERM

is_true() {
  case "${1:-false}" in
    1|true|TRUE|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

log() { printf '==> %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

if ! ( : <"$TTY_IN" ) 2>/dev/null; then
  TTY_IN="/dev/stdin"
fi
if ! ( : >"$TTY_OUT" ) 2>/dev/null; then
  TTY_OUT="/dev/stderr"
fi

usage() {
  cat <<'USAGE'
AgentDock Unix 安装与维护入口。

用法：
  sh install.sh
  sh install.sh --nexus official|self-hosted|none [--nexus-endpoint URL] [--nexus-pair-code-file FILE]
  sh install.sh --tunnel none|quick|named [--server-url URL] [--tunnel-token-file FILE]
  sh install.sh --register-service [--no-start]
  sh install.sh --uninstall [--purge-config|--purge-data]
  sh install.sh --agentdock-home DIR --agentdock-default-dir DIR

说明：
  Linux 默认安装 systemd/OpenRC 服务；macOS CLI 默认只安装运行时，使用
  --register-service 才注册 LaunchAgent。图形版 macOS 用户优先使用 DMG。
USAGE
}

download_file() {
  url="$1"
  destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --retry-delay 1 "$url" -o "$destination"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$destination" "$url"
    return
  fi
  die "缺少 curl 或 wget，无法下载 Release 载荷。"
}

download_file_with_progress() {
  url="$1"
  destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --progress-bar --retry 3 --retry-delay 1 "$url" -o "$destination"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -O "$destination" "$url"
    return
  fi
  die "缺少 curl 或 wget，无法下载 cloudflared。"
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
  else
    die "缺少 SHA-256 校验工具（sha256sum、shasum 或 openssl）。"
  fi
}

verify_checksum() {
  file="$1"
  checksum_file="$2"
  expected="$(awk 'NF { print $1; exit }' "$checksum_file" | tr '[:upper:]' '[:lower:]')"
  actual="$(sha256_file "$file" | tr '[:upper:]' '[:lower:]')"
  [ -n "$expected" ] || die "校验文件为空：$checksum_file"
  [ "$actual" = "$expected" ] || die "SHA-256 校验失败：$(basename "$file")"
}

trim_trailing_slashes() {
  value="$1"
  while [ "$value" != "/" ] && [ "${value%/}" != "$value" ]; do
    value="${value%/}"
  done
  printf '%s' "$value"
}

# 所有 macOS 递归删除目标先经过同一安全边界。公开入口允许自定义布局，
# 但绝不能因为一个环境变量误配就把整个 HOME 或系统顶层目录交给 rm -rf。
require_safe_removal_path() {
  label="$1"
  path="$2"

  case "$path" in
    /*) ;;
    *) die "$label 必须是绝对路径：$path" ;;
  esac
  case "$path" in
    *"/../"*|*"/.."|*"/./"*|*"/.") die "$label 不能包含路径跳转：$path" ;;
  esac

  normalized="$(trim_trailing_slashes "$path")"
  case "$normalized" in
    /|/Applications|/Library|/System|/Users|/Volumes|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/opt|/private|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/var)
      die "$label 不能是系统顶层目录：$normalized"
      ;;
  esac
  case "$normalized" in
    /Users/*|/home/*)
      account_relative="${normalized#/*/}"
      case "$account_relative" in
        */*) ;;
        *) die "$label 不能是整个用户主目录：$normalized" ;;
      esac
      ;;
  esac
  [ "$normalized" != "$(trim_trailing_slashes "$HOME")" ] || die "$label 不能是整个用户主目录：$normalized"
  [ ! -L "$normalized" ] || die "$label 不能是符号链接：$normalized"
  printf '%s' "$normalized"
}

# purge-data 的用户态目录还不能反向包含 Installer 自己的 install/runtime root。
require_safe_user_data_path() {
  label="$1"
  path="$2"
  install_root="$3"
  runtime_root="$4"
  normalized="$(require_safe_removal_path "$label" "$path")"

  for protected in "$install_root" "$runtime_root"; do
    protected="$(trim_trailing_slashes "$protected")"
    case "$protected/" in
      "$normalized/"*) die "${label} 不能包含 Installer 管理目录 ${protected}：${normalized}" ;;
    esac
  done
  printf '%s' "$normalized"
}

require_safe_app_path() {
  normalized="$(require_safe_removal_path "$1" "$2")"
  case "$(basename "$normalized")" in
    *.app) ;;
    *) die "$1 必须指向 .app 应用目录：$normalized" ;;
  esac
  printf '%s' "$normalized"
}

run_root() {
  if is_true "${AGENTDOCK_NO_SUDO:-false}" || [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    die "需要 root 权限，请安装 sudo 或使用 root 运行。"
  fi
}

run_engine_root() {
  if is_true "${AGENTDOCK_NO_SUDO:-false}" || [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo --preserve-env=AGENTDOCK_AUTH_TOKEN,AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN,AGENTDOCK_OAUTH_PASSWORD,AGENTDOCK_OAUTH_TOKEN_SECRET "$@"
  else
    die "需要 root 权限，请安装 sudo 或使用 root 运行。"
  fi
}

prompt_choice() {
  label="$1"
  default_value="$2"
  answer=""
  printf '%s [%s]: ' "$label" "$default_value" >>"$TTY_OUT"
  IFS= read -r answer <"$TTY_IN" || true
  printf '%s' "${answer:-$default_value}"
}

prompt_value() {
  label="$1"
  default_value="${2:-}"
  answer=""
  if [ -n "$default_value" ]; then
    printf '%s [%s]: ' "$label" "$default_value" >>"$TTY_OUT"
  else
    printf '%s: ' "$label" >>"$TTY_OUT"
  fi
  IFS= read -r answer <"$TTY_IN" || true
  printf '%s' "${answer:-$default_value}"
}

prompt_secret() {
  label="$1"
  answer=""
  printf '%s（输入不回显）: ' "$label" >>"$TTY_OUT"
  stty -echo <"$TTY_IN" 2>/dev/null || true
  IFS= read -r answer <"$TTY_IN" || true
  stty echo <"$TTY_IN" 2>/dev/null || true
  printf '\n' >>"$TTY_OUT"
  [ -n "$answer" ] || die "$label 不能为空。"
  printf '%s' "$answer"
}

normalize_arch() {
  case "$1" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) die "暂不支持的 CPU 架构：$1" ;;
  esac
}

detect_service_manager() {
  requested="${AGENTDOCK_SERVICE_MANAGER:-auto}"
  case "$requested" in
    systemd|openrc|none) printf '%s' "$requested"; return ;;
    auto) ;;
    *) die "AGENTDOCK_SERVICE_MANAGER 必须是 auto/systemd/openrc/none。" ;;
  esac
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    printf 'systemd'
  elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    printf 'openrc'
  else
    printf 'none'
  fi
}

ensure_linux_service_user() {
  user="$1"
  home_dir="$2"
  if id "$user" >/dev/null 2>&1; then
    return
  fi
  log "创建运行用户：$user"
  nologin="$(command -v nologin 2>/dev/null || true)"
  [ -n "$nologin" ] || nologin=/usr/sbin/nologin
  if command -v useradd >/dev/null 2>&1; then
    run_root useradd --system --home-dir "$home_dir" --create-home --shell "$nologin" "$user"
  elif command -v adduser >/dev/null 2>&1; then
    run_root addgroup -S "$user" >/dev/null 2>&1 || true
    run_root adduser -S -D -h "$home_dir" -s "$nologin" -G "$user" "$user"
    run_root mkdir -p "$home_dir"
  else
    die "未找到 useradd/adduser，无法创建运行用户：$user"
  fi
}

valid_cloudflared() {
  candidate="$1"
  [ -n "$candidate" ] && [ -f "$candidate" ] && [ -x "$candidate" ] && "$candidate" --version >/dev/null 2>&1
}

install_cloudflared() {
  target="$1"
  source="${AGENTDOCK_CLOUDFLARED_BINARY:-}"
  if valid_cloudflared "$target"; then
    log "复用已有 cloudflared：$target"
    printf '%s' "$target"
    return
  fi
  if [ -z "$source" ]; then
    discovered="$(command -v cloudflared 2>/dev/null || true)"
    if valid_cloudflared "$discovered"; then
      log "复用已有 cloudflared：$discovered"
      source="$discovered"
    fi
  elif valid_cloudflared "$source"; then
    log "使用指定 cloudflared：$source"
  fi
  if [ -z "$source" ]; then
    case "$PLATFORM" in
      linux)
        source="$TMP_ROOT/cloudflared"
        log "下载 cloudflared-linux-$ARCH"
        download_file_with_progress "$CLOUDFLARED_BASE_URL/cloudflared-linux-$ARCH" "$source"
        chmod 700 "$source"
        ;;
      darwin)
        archive="$TMP_ROOT/cloudflared.tgz"
        cloud_dir="$TMP_ROOT/cloudflared-extract"
        log "下载 cloudflared-darwin-$ARCH.tgz"
        download_file_with_progress "$CLOUDFLARED_BASE_URL/cloudflared-darwin-$ARCH.tgz" "$archive"
        mkdir -p "$cloud_dir"
        tar -xzf "$archive" -C "$cloud_dir"
        source="$cloud_dir/cloudflared"
        ;;
    esac
  fi
  valid_cloudflared "$source" || die "cloudflared 载荷无效：$source"
  case "$PLATFORM" in
    linux)
      run_root mkdir -p "$(dirname "$target")"
      run_root install -m 0755 "$source" "$target"
      ;;
    darwin)
      mkdir -p "$(dirname "$target")"
      install -m 0755 "$source" "$target"
      ;;
  esac
  valid_cloudflared "$target" || die "cloudflared 安装失败：$target"
  printf '%s' "$target"
}

prepare_payload() {
  asset="agentdock_${PLATFORM}_${ARCH}.tar.gz"
  archive="$TMP_ROOT/$asset"
  checksum="$archive.sha256"
  payload="$TMP_ROOT/payload"
  log "下载 $asset"
  download_file "$BASE_URL/$asset" "$archive"
  download_file "$BASE_URL/$asset.sha256" "$checksum"
  verify_checksum "$archive" "$checksum"
  mkdir -p "$payload"
  tar -xzf "$archive" -C "$payload"
  ENGINE="$payload/bin/agentdock"
  if [ ! -f "$ENGINE" ] || [ -L "$ENGINE" ]; then
    die "Release archive 缺少 bin/agentdock"
  fi
  chmod 700 "$ENGINE"
  ready="$($ENGINE install --engine-ready 2>/dev/null || true)"
  case "$ready" in
    *agentdock-installer-engine*) ;;
    *) die "Release binary 不支持 Installer Engine；请使用与该版本匹配的旧安装入口。" ;;
  esac
  [ -d "$payload/share/agentdock/core-skills" ] || die "Release archive 缺少 core-skills"
  PAYLOAD_DIR="$payload"
}

choose_tunnel_mode() {
  cat >>"$TTY_OUT" <<'CHOICE'

Cloudflare Tunnel：
1) 不配置
2) 临时公网地址（Quick Tunnel）
3) 固定公网地址（Named Tunnel）
CHOICE
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1|none) TUNNEL_MODE=none ;;
    2|quick) TUNNEL_MODE=quick ;;
    3|named) TUNNEL_MODE=named ;;
    *) die "无效选择：$choice" ;;
  esac
}

choose_nexus_mode() {
  cat >>"$TTY_OUT" <<'CHOICE'

NexusDock 远程连接：
1) 官方服务（推荐）
2) 自托管
3) 暂不连接
CHOICE
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1|official) NEXUS_MODE=official ;;
    2|self-hosted|selfhosted) NEXUS_MODE=self-hosted ;;
    3|none) NEXUS_MODE=none ;;
    *) die "无效选择：$choice" ;;
  esac
}

restart_core_after_pair() {
  if [ "$NO_START" = true ]; then
    log "Nexus 已配对；当前使用 --no-start，配置将在下次启动 AgentDock 时生效。"
    return 0
  fi

  case "$PLATFORM" in
    linux)
      if [ "$SERVICE_MANAGER" = none ]; then
        log "Nexus 已配对；当前未使用服务管理器，配置将在下次启动 AgentDock 时生效。"
        return 0
      fi
      run_root "$STABLE_BINARY" service restart --runtime-root "$RUNTIME_ROOT" >/dev/null
      ;;
    darwin)
      if [ "$REGISTER_SERVICE" != true ]; then
        log "Nexus 已配对；当前未注册 LaunchAgent，配置将在下次启动 AgentDock 时生效。"
        return 0
      fi
      "$STABLE_BINARY" service restart --runtime-root "$RUNTIME_ROOT" >/dev/null
      ;;
  esac

  log "AgentDock 已自动重启并应用 Nexus 配置。"
}

pair_nexus_once() {
  pair_output="$TMP_ROOT/nexus-pair-output.log"
  : >"$pair_output"
  pair_status=0

  case "$PLATFORM" in
    linux)
      if [ "$SERVICE_MANAGER" = none ]; then
        pair_command="current"
      elif command -v runuser >/dev/null 2>&1; then
        pair_command="runuser"
      elif command -v setpriv >/dev/null 2>&1; then
        pair_command="setpriv"
      elif command -v sudo >/dev/null 2>&1; then
        pair_command="sudo"
      else
        die "缺少 runuser/setpriv/sudo，无法安全地以 $SERVICE_USER 身份写入 Nexus identity。"
      fi
      case "$pair_command" in
        current)
          env HOME="$AGENTDOCK_HOME_DIR" AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus pair --endpoint "$NEXUS_ENDPOINT" --code "$NEXUS_PAIR_CODE" >"$pair_output" 2>&1 || pair_status=$?
          ;;
        runuser)
          run_root runuser -u "$SERVICE_USER" -- env HOME="$AGENTDOCK_HOME_DIR" AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus pair --endpoint "$NEXUS_ENDPOINT" --code "$NEXUS_PAIR_CODE" >"$pair_output" 2>&1 || pair_status=$?
          ;;
        setpriv)
          service_uid="$(id -u "$SERVICE_USER")"
          service_gid="$(id -g "$SERVICE_USER")"
          run_root setpriv --reuid "$service_uid" --regid "$service_gid" --init-groups env HOME="$AGENTDOCK_HOME_DIR" AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus pair --endpoint "$NEXUS_ENDPOINT" --code "$NEXUS_PAIR_CODE" >"$pair_output" 2>&1 || pair_status=$?
          ;;
        sudo)
          {
            sudo -u "$SERVICE_USER" env HOME="$AGENTDOCK_HOME_DIR" AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus pair --endpoint "$NEXUS_ENDPOINT" --code "$NEXUS_PAIR_CODE"
          } >"$pair_output" 2>&1 || pair_status=$?
          ;;
      esac
      ;;
    darwin)
      env AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus pair --endpoint "$NEXUS_ENDPOINT" --code "$NEXUS_PAIR_CODE" >"$pair_output" 2>&1 || pair_status=$?
      ;;
  esac

  if [ "$pair_status" -ne 0 ]; then
    cat "$pair_output" >>"$TTY_OUT"
    rm -f "$pair_output"
    return 1
  fi
  # Installer 自己会在配对后应用新配置并重启 Core，成功输出无需重复展示。
  rm -f "$pair_output"

  if ! restart_core_after_pair; then
    log "Nexus 已配对，但 Core 自动重启失败；下次启动时会加载新的 Nexus 身份。"
  fi
  return 0
}

configure_nexus() {
  if [ -z "$NEXUS_MODE" ]; then
    if is_true "$NONINTERACTIVE"; then
      NEXUS_MODE=none
    else
      choose_nexus_mode
    fi
  fi

  case "$NEXUS_MODE" in
    none)
      NEXUS_ENDPOINT=""
      return
      ;;
    official)
      NEXUS_ENDPOINT="$OFFICIAL_NEXUS_ENDPOINT"
      ;;
    self-hosted)
      if [ -z "$NEXUS_ENDPOINT" ]; then
        is_true "$NONINTERACTIVE" && die "自托管 NexusDock 必须提供 --nexus-endpoint"
        NEXUS_ENDPOINT="$(prompt_value 'Nexus Endpoint')"
      fi
      ;;
  esac

  if [ -n "$NEXUS_PAIR_CODE_FILE" ]; then
    [ -f "$NEXUS_PAIR_CODE_FILE" ] || die "NexusDock 配对码文件不存在：$NEXUS_PAIR_CODE_FILE"
    NEXUS_PAIR_CODE="$(sed -n '1p' "$NEXUS_PAIR_CODE_FILE")"
  fi
  if [ -z "$NEXUS_PAIR_CODE" ]; then
    is_true "$NONINTERACTIVE" && die "NexusDock 配对必须通过 AGENTDOCK_NEXUS_PAIR_CODE 或 --nexus-pair-code-file 提供配对码"
    if [ "$NEXUS_MODE" = official ]; then
      printf '\n打开 %s 获取 NexusDock 配对码\n' "$OFFICIAL_NEXUS_DEVICES_URL" >>"$TTY_OUT"
    fi
    NEXUS_PAIR_CODE="$(prompt_value '配对码')"
  fi

  while ! pair_nexus_once; do
    if is_true "$NONINTERACTIVE"; then
      die "Nexus 配对失败。"
    fi
    retry="$(prompt_choice 'NexusDock 配对失败，重新输入配对码？(y/n)' y)"
    case "$retry" in
      y|Y|yes|YES) NEXUS_PAIR_CODE="$(prompt_value '配对码')" ;;
      *)
        log "已跳过 Nexus 配对，AgentDock Core 保持可用。"
        NEXUS_MODE=none
        NEXUS_ENDPOINT=""
        return
        ;;
    esac
  done
}

read_env_value() {
  file="$1"
  key="$2"

  # Installer env 文件由 service user 运行时可读/写。这里只解析目标 KEY，绝不能
  # source/eval 整个文件，否则再次以 root 运行安装器时会把配置内容升级成 shell 代码执行。
  if [ "$PLATFORM" = linux ]; then
    # shellcheck disable=SC2016
    run_root awk -v key="$key" '
      index($0, key "=") == 1 {
        value = substr($0, length(key) + 2)
        if (value == "\047\047") {
          exit
        }
        if (length(value) >= 2) {
          first = substr(value, 1, 1)
          last = substr(value, length(value), 1)
          if ((first == "\047" && last == "\047") || (first == "\"" && last == "\"")) {
            value = substr(value, 2, length(value) - 2)
          }
        }
        output = ""
        escaped = 0
        for (i = 1; i <= length(value); i++) {
          char = substr(value, i, 1)
          if (escaped) {
            output = output char
            escaped = 0
          } else if (char == "\\") {
            escaped = 1
          } else {
            output = output char
          }
        }
        if (escaped) {
          output = output "\\"
        }
        printf "%s", output
        exit
      }
    ' "$file" 2>/dev/null || true
  else
    # shellcheck disable=SC2016
    awk -v key="$key" '
      index($0, key "=") == 1 {
        value = substr($0, length(key) + 2)
        if (value == "\047\047") {
          exit
        }
        if (length(value) >= 2) {
          first = substr(value, 1, 1)
          last = substr(value, length(value), 1)
          if ((first == "\047" && last == "\047") || (first == "\"" && last == "\"")) {
            value = substr(value, 2, length(value) - 2)
          }
        }
        output = ""
        escaped = 0
        for (i = 1; i <= length(value); i++) {
          char = substr(value, i, 1)
          if (escaped) {
            output = output char
            escaped = 0
          } else if (char == "\\") {
            escaped = 1
          } else {
            output = output char
          }
        }
        if (escaped) {
          output = output "\\"
        }
        printf "%s", output
        exit
      }
    ' "$file" 2>/dev/null || true
  fi
}

read_core_env_value() {
  runtime_env="$RUNTIME_ROOT/agentdock.env"
  [ "$PLATFORM" != linux ] || runtime_env="$ENV_FILE"
  read_env_value "$runtime_env" "$1"
}

read_auth_token() {
  read_core_env_value AGENTDOCK_AUTH_TOKEN
}

read_oauth_password() {
  read_core_env_value AGENTDOCK_OAUTH_PASSWORD
}

read_local_mcp_url() {
  host="$(read_core_env_value AGENTDOCK_HOST)"
  port="$(read_core_env_value AGENTDOCK_PORT)"
  [ -n "$host" ] || host=127.0.0.1
  [ -n "$port" ] || port=8765
  case "$host" in ""|0.0.0.0|::|"[::]") host=127.0.0.1 ;; esac
  case "$host" in *:*) host="[$host]" ;; esac
  printf 'http://%s:%s/mcp' "$host" "$port"
}

read_server_url() {
  read_core_env_value AGENTDOCK_SERVER_URL
}

wait_quick_tunnel_url() {
  quick_url_file="$RUNTIME_ROOT/quick-tunnel-url.txt"
  attempts=0
  max_attempts=60
  log "正在获取 Quick Tunnel 公网地址..."
  while [ "$attempts" -lt "$max_attempts" ]; do
    if [ "$PLATFORM" = linux ]; then
      if run_root test -f "$quick_url_file"; then
        public_url="$(run_root sed -n '1p' "$quick_url_file" 2>/dev/null || true)"
      else
        public_url=""
      fi
    else
      if [ -f "$quick_url_file" ]; then
        public_url="$(sed -n '1p' "$quick_url_file" 2>/dev/null || true)"
      else
        public_url=""
      fi
    fi
    if [ -n "$public_url" ]; then
      log "Quick Tunnel 公网地址已就绪。"
      printf '%s' "$public_url"
      return 0
    fi
    attempts=$((attempts + 1))
    if [ $((attempts % 5)) -eq 0 ] && [ "$attempts" -lt "$max_attempts" ]; then
      log "等待 Cloudflare 分配公网地址... ${attempts}s/${max_attempts}s"
    fi
    sleep 1
  done
  log "Quick Tunnel 已启动，但 60 秒内尚未获取到公网地址。"
  return 1
}

validate_linux_cli_link() {
  [ "$PLATFORM" = linux ] || return 0
  if [ -L "$CLI_LINK_PATH" ]; then
    [ "$(readlink "$CLI_LINK_PATH")" = "$STABLE_BINARY" ] || die "CLI 路径已被其他符号链接占用：$CLI_LINK_PATH"
    return
  fi
  [ ! -e "$CLI_LINK_PATH" ] || die "CLI 路径已存在且不是 AgentDock 符号链接：$CLI_LINK_PATH"
}

install_linux_cli_link() {
  [ "$PLATFORM" = linux ] || return 0
  run_root mkdir -p "$(dirname "$CLI_LINK_PATH")"
  run_root ln -sfn "$STABLE_BINARY" "$CLI_LINK_PATH"
}

remove_linux_cli_link() {
  [ "$PLATFORM" = linux ] || return 0
  [ -L "$CLI_LINK_PATH" ] || return 0
  [ "$(readlink "$CLI_LINK_PATH")" = "$STABLE_BINARY" ] || return 0
  run_root rm -f "$CLI_LINK_PATH"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --uninstall) UNINSTALL=true; shift ;;
    --purge-config) PURGE_CONFIG=true; shift ;;
    --purge-data) PURGE_CONFIG=true; PURGE_DATA=true; shift ;;
    --agentdock-home)
      [ "$#" -ge 2 ] || die "--agentdock-home 缺少值"
      INSTALLER_AGENTDOCK_HOME="$2"; INSTALLER_AGENTDOCK_HOME_EXPLICIT=true; shift 2 ;;
    --agentdock-default-dir)
      [ "$#" -ge 2 ] || die "--agentdock-default-dir 缺少值"
      INSTALLER_DEFAULT_DIR="$2"; INSTALLER_DEFAULT_DIR_EXPLICIT=true; shift 2 ;;
    --register-service) REGISTER_SERVICE=true; shift ;;
    --no-start) NO_START=true; shift ;;
    --nexus)
      [ "$#" -ge 2 ] || die "--nexus 缺少值"
      NEXUS_MODE="$2"; shift 2 ;;
    --nexus-endpoint)
      [ "$#" -ge 2 ] || die "--nexus-endpoint 缺少值"
      NEXUS_ENDPOINT="$2"; shift 2 ;;
    --nexus-pair-code-file)
      [ "$#" -ge 2 ] || die "--nexus-pair-code-file 缺少值"
      NEXUS_PAIR_CODE_FILE="$2"; shift 2 ;;
    --tunnel)
      [ "$#" -ge 2 ] || die "--tunnel 缺少值"
      TUNNEL_MODE="$2"; shift 2 ;;
    --server-url)
      [ "$#" -ge 2 ] || die "--server-url 缺少值"
      SERVER_URL="$2"; shift 2 ;;
    --tunnel-token-file)
      [ "$#" -ge 2 ] || die "--tunnel-token-file 缺少值"
      TUNNEL_TOKEN_FILE="$2"; shift 2 ;;
    --host)
      [ "$#" -ge 2 ] || die "--host 缺少值"
      HOST_VALUE="$2"; shift 2 ;;
    --port)
      [ "$#" -ge 2 ] || die "--port 缺少值"
      PORT_VALUE="$2"; shift 2 ;;
    --log-level)
      [ "$#" -ge 2 ] || die "--log-level 缺少值"
      LOG_LEVEL_VALUE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数：$1" ;;
  esac
done

if { [ "$PURGE_CONFIG" = true ] || [ "$PURGE_DATA" = true ]; } && [ "$UNINSTALL" != true ]; then
  die "--purge-config/--purge-data 必须与 --uninstall 一起使用。"
fi

case "$NEXUS_MODE" in
  ''|official|self-hosted|none) ;;
  *) die "Nexus 模式必须是 official、self-hosted 或 none。" ;;
esac

case "$(uname -s 2>/dev/null || true)" in
  Linux) PLATFORM=linux ;;
  Darwin) PLATFORM=darwin ;;
  *) die "当前系统不受 install.sh 支持；Windows 请使用 Setup 或 install.ps1。" ;;
esac
ARCH="$(normalize_arch "$(uname -m)")"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-bootstrap.XXXXXX")"

case "$PLATFORM" in
  linux)
    INSTALL_ROOT="${AGENTDOCK_SOURCE_DIR:-/opt/agentdock}"
    ENV_FILE="${AGENTDOCK_ENV_FILE:-/etc/agentdock/agentdock.env}"
    RUNTIME_ROOT="$(dirname "$ENV_FILE")"
    DATA_DIR="${AGENTDOCK_DATA_DIR:-/srv/agentdock}"
    AGENTDOCK_HOME_DIR="${INSTALLER_AGENTDOCK_HOME:-$DATA_DIR/.agentdock}"
    AGENTDOCK_DEFAULT_DIR_VALUE="${INSTALLER_DEFAULT_DIR:-$DATA_DIR/AgentDock}"
    SERVICE_NAME="${AGENTDOCK_SERVICE_NAME:-agentdock}"
    SERVICE_USER="${AGENTDOCK_SERVICE_USER:-agentdock}"
    SERVICE_MANAGER="$(detect_service_manager)"
    SYSTEMD_DIR="${AGENTDOCK_SYSTEMD_DIR:-/etc/systemd/system}"
    OPENRC_DIR="${AGENTDOCK_OPENRC_DIR:-/etc/init.d}"
    STABLE_BINARY="$INSTALL_ROOT/bin/agentdock"
    CLI_LINK_PATH="${AGENTDOCK_CLI_LINK_PATH:-/usr/local/bin/agentdock}"
    CLOUDFLARED_TARGET="${AGENTDOCK_CLOUDFLARED_INSTALL_PATH:-/usr/local/bin/cloudflared}"
    ONBOARDING_STATE_FILE="${AGENTDOCK_ONBOARDING_STATE_FILE:-$RUNTIME_ROOT/.installer-onboarding}"
    ;;
  darwin)
    INSTALL_ROOT="${AGENTDOCK_INSTALL_DIR:-$HOME/.local/bin}"
    RUNTIME_ROOT="${AGENTDOCK_RUNTIME_ROOT:-$HOME/Library/Application Support/AgentDock}"
    DATA_DIR="${INSTALLER_DEFAULT_DIR:-${AGENTDOCK_DEFAULT_DIR:-$HOME/AgentDock}}"
    AGENTDOCK_HOME_DIR="${INSTALLER_AGENTDOCK_HOME:-${AGENTDOCK_HOME:-$HOME/.agentdock}}"
    AGENTDOCK_DEFAULT_DIR_VALUE="$DATA_DIR"
    LAUNCH_AGENTS_DIR="${AGENTDOCK_LAUNCH_AGENTS_DIR:-$HOME/Library/LaunchAgents}"
    STABLE_BINARY="$INSTALL_ROOT/agentdock"
    CLOUDFLARED_TARGET="${AGENTDOCK_CLOUDFLARED_INSTALL_PATH:-$INSTALL_ROOT/cloudflared}"
    ONBOARDING_STATE_FILE="${AGENTDOCK_ONBOARDING_STATE_FILE:-$RUNTIME_ROOT/.installer-onboarding}"
    DARWIN_CUSTOM_LAYOUT=false
    if [ -n "${AGENTDOCK_INSTALL_DIR:-}${AGENTDOCK_RUNTIME_ROOT:-}${AGENTDOCK_DEFAULT_DIR:-}${AGENTDOCK_HOME:-}${AGENTDOCK_LAUNCH_AGENTS_DIR:-}" ]; then
      DARWIN_CUSTOM_LAYOUT=true
    fi
    if [ "$DARWIN_CUSTOM_LAYOUT" = true ]; then
      APP_PATH="${AGENTDOCK_APP_PATH:-}"
      LOG_DIR="${AGENTDOCK_LOG_DIR:-}"
    else
      APP_PATH="${AGENTDOCK_APP_PATH:-/Applications/AgentDock.app}"
      LOG_DIR="${AGENTDOCK_LOG_DIR:-$HOME/Library/Logs/AgentDock}"
    fi
    ;;
esac

if [ "$UNINSTALL" = true ] && [ "$PLATFORM" = darwin ]; then
  INSTALL_ROOT="$(require_safe_removal_path "macOS 安装目录" "$INSTALL_ROOT")"
  RUNTIME_ROOT="$(require_safe_removal_path "macOS 运行目录" "$RUNTIME_ROOT")"
  if [ -n "$LOG_DIR" ]; then LOG_DIR="$(require_safe_removal_path "macOS 日志目录" "$LOG_DIR")"; fi
  if [ -n "$APP_PATH" ]; then APP_PATH="$(require_safe_app_path "macOS App" "$APP_PATH")"; fi
fi

if [ "$UNINSTALL" = true ] && [ "$PURGE_DATA" = true ] && [ "$PLATFORM" = darwin ]; then
  # AgentDock.app 启动的子进程会继承生产 AGENTDOCK_HOME/DEFAULT_DIR。自定义布局下
  # 这些运行时变量不能自动升级成递归删除目标；必须由 installer 专用参数明确确认。
  if [ "$DARWIN_CUSTOM_LAYOUT" = true ]; then
    [ "$INSTALLER_AGENTDOCK_HOME_EXPLICIT" = true ] || \
      die "自定义/继承的 macOS 环境执行 --purge-data 时必须显式提供 --agentdock-home（或 AGENTDOCK_INSTALLER_AGENTDOCK_HOME）。"
    [ "$INSTALLER_DEFAULT_DIR_EXPLICIT" = true ] || \
      die "自定义/继承的 macOS 环境执行 --purge-data 时必须显式提供 --agentdock-default-dir（或 AGENTDOCK_INSTALLER_DEFAULT_DIR）。"
  fi
  AGENTDOCK_HOME_DIR="$(require_safe_user_data_path "AgentDock HOME" "$AGENTDOCK_HOME_DIR" "$INSTALL_ROOT" "$RUNTIME_ROOT")"
  AGENTDOCK_DEFAULT_DIR_VALUE="$(require_safe_user_data_path "AgentDock 默认工作目录" "$AGENTDOCK_DEFAULT_DIR_VALUE" "$INSTALL_ROOT" "$RUNTIME_ROOT")"
fi

if [ "$UNINSTALL" = true ]; then
  [ -x "$STABLE_BINARY" ] || die "找不到已安装 AgentDock：$STABLE_BINARY"
  case "$PLATFORM" in
    linux)
      set -- uninstall --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
        --service-name "$SERVICE_NAME" --service-manager "$SERVICE_MANAGER" \
        --systemd-dir "$SYSTEMD_DIR" --openrc-dir "$OPENRC_DIR" \
        --agentdock-home "$AGENTDOCK_HOME_DIR" --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE"
      if [ "$PURGE_DATA" = true ]; then
        set -- "$@" --purge-data
      elif [ "$PURGE_CONFIG" = true ]; then
        set -- "$@" --purge-config
      fi
      run_engine_root "$STABLE_BINARY" "$@" >/dev/null
      remove_linux_cli_link
      if [ "$PURGE_DATA" = true ]; then
        # Engine 已按 transaction 中冻结的两个显式子目录完成递归清理；父目录只在空时删除。
        run_root rmdir "$DATA_DIR" >/dev/null 2>&1 || true
      fi
      ;;
    darwin)
      APP_EXEC=""
      if [ -n "$APP_PATH" ]; then APP_EXEC="$APP_PATH/Contents/MacOS/AgentDock"; fi
      if [ -n "$APP_PATH" ] && [ -d "$APP_PATH" ] && [ -x "$APP_EXEC" ]; then
        "$APP_EXEC" --unregister-background-services
      fi
      "$STABLE_BINARY" uninstall --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
        --launch-agents-dir "$LAUNCH_AGENTS_DIR" --purge-config \
        --agentdock-home "$AGENTDOCK_HOME_DIR" --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE" >/dev/null
      rm -f "$INSTALL_ROOT/agentdock" "$INSTALL_ROOT/cloudflared"
      rm -rf "$INSTALL_ROOT/versions" "$RUNTIME_ROOT"
      if [ -n "$LOG_DIR" ]; then rm -rf "$LOG_DIR"; fi
      if [ -n "$APP_PATH" ] && [ -d "$APP_PATH" ] && [ ! -L "$APP_PATH" ]; then rm -rf "$APP_PATH"; fi
      if [ "$PURGE_DATA" = true ]; then
        rm -rf "$AGENTDOCK_HOME_DIR" "$AGENTDOCK_DEFAULT_DIR_VALUE"
      fi
      ;;
  esac
  printf '\nAgentDock 已卸载。\n' >>"$TTY_OUT"
  exit 0
fi

run_install_engine() {
  engine_action="$1"
  engine_tunnel_mode="$2"
  engine_cloudflared="$3"
  result_file="$4"
  rotate_oauth="${5:-false}"

  case "$PLATFORM" in
    linux)
      set -- install --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
        --service-name "$SERVICE_NAME" --service-user "$SERVICE_USER" --service-group "$SERVICE_GROUP" \
        --service-manager "$SERVICE_MANAGER" --data-dir "$DATA_DIR" \
        --agentdock-home "$AGENTDOCK_HOME_DIR" --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE" \
        --systemd-dir "$SYSTEMD_DIR" --openrc-dir "$OPENRC_DIR"
      if [ "$SERVICE_MANAGER" = none ] || [ "$NO_START" = true ]; then
        set -- "$@" --no-start --skip-health
      fi
      ;;
    darwin)
      set -- install --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
        --live-binary "$STABLE_BINARY" --data-dir "$DATA_DIR" \
        --agentdock-home "$AGENTDOCK_HOME_DIR" --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE" \
        --launch-agents-dir "$LAUNCH_AGENTS_DIR"
      if [ "$REGISTER_SERVICE" = true ]; then
        set -- "$@" --register-service
        if [ "$NO_START" = true ]; then set -- "$@" --no-start --skip-health; fi
      else
        set -- "$@" --no-start --skip-health
      fi
      ;;
  esac

  if [ "$engine_action" = install ]; then
    set -- "$@" --payload-dir "$PAYLOAD_DIR"
    engine_binary="$ENGINE"
  else
    # Tunnel 是 Core 之后的第二阶段配置。repair 仍由 Installer Engine 写 env/unit、
    # 执行 service lifecycle 和 rollback；bootstrap 脚本不复制这套状态机。
    set -- "$@" --repair --skip-skills
    engine_binary="$STABLE_BINARY"
  fi

  if [ -n "$HOST_VALUE" ]; then set -- "$@" --host "$HOST_VALUE"; fi
  if [ -n "$PORT_VALUE" ]; then set -- "$@" --port "$PORT_VALUE"; fi
  if [ -n "$LOG_LEVEL_VALUE" ]; then set -- "$@" --log-level "$LOG_LEVEL_VALUE"; fi
  if [ -n "$engine_tunnel_mode" ]; then set -- "$@" --tunnel-mode "$engine_tunnel_mode"; fi
  if [ "$engine_tunnel_mode" = named ]; then
    set -- "$@" --server-url "$SERVER_URL"
    if [ -n "$TUNNEL_TOKEN_FILE" ]; then set -- "$@" --token-file "$TUNNEL_TOKEN_FILE"; fi
  fi
  if [ -n "$engine_cloudflared" ]; then set -- "$@" --cloudflared "$engine_cloudflared"; fi
  if [ "$rotate_oauth" = true ]; then set -- "$@" --rotate-oauth; fi

  case "$PLATFORM" in
    linux) run_engine_root "$engine_binary" "$@" >"$result_file" ;;
    darwin) "$engine_binary" "$@" >"$result_file" ;;
  esac
}

prepare_named_tunnel() {
  if [ -z "$SERVER_URL" ]; then
    is_true "$NONINTERACTIVE" && die "Named Tunnel 必须提供 --server-url"
    SERVER_URL="$(prompt_value 'HTTPS 公网地址')"
  fi
  case "$SERVER_URL" in
    https://*) ;;
    *) die "Named Tunnel 公网地址必须是 https:// URL" ;;
  esac

  if [ -z "${AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN:-}" ] && [ -z "$TUNNEL_TOKEN_FILE" ]; then
    is_true "$NONINTERACTIVE" && die "Named Tunnel 必须通过环境变量或 --tunnel-token-file 提供 Token"
    AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN="$(prompt_secret 'Cloudflare Tunnel Token')"
    export AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN
  fi
}

read_tunnel_mode() {
  tunnel_env="$RUNTIME_ROOT/cloudflared.env"
  if [ "$PLATFORM" = linux ]; then
    if ! run_root test -f "$tunnel_env"; then
      printf 'none'
      return
    fi
  else
    if [ ! -f "$tunnel_env" ]; then
      printf 'none'
      return
    fi
  fi
  mode="$(read_env_value "$tunnel_env" AGENTDOCK_TUNNEL_MODE)"
  [ -n "$mode" ] || mode=none
  printf '%s' "$mode"
}

read_nexus_endpoint() {
  if [ "$PLATFORM" = linux ]; then
    nexus_status="$(run_root env AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus status --json 2>/dev/null || true)"
  else
    nexus_status="$(env AGENTDOCK_HOME="$AGENTDOCK_HOME_DIR" "$STABLE_BINARY" nexus status --json 2>/dev/null || true)"
  fi
  printf '%s' "$nexus_status" | sed -n 's/.*"endpoint":"\([^"]*\)".*/\1/p'
}

read_onboarding_stage() {
  if [ "$PLATFORM" = linux ]; then
    run_root test -f "$ONBOARDING_STATE_FILE" || return 0
    stage="$(run_root sed -n '1p' "$ONBOARDING_STATE_FILE" 2>/dev/null || true)"
  else
    [ -f "$ONBOARDING_STATE_FILE" ] || return 0
    stage="$(sed -n '1p' "$ONBOARDING_STATE_FILE" 2>/dev/null || true)"
  fi
  case "$stage" in
    core|nexus|tunnel) printf '%s' "$stage" ;;
    '') ;;
    *) die "安装恢复状态无效：$stage" ;;
  esac
}

write_onboarding_stage() {
  stage="$1"
  state_tmp="$TMP_ROOT/onboarding-state"
  printf '%s\n' "$stage" >"$state_tmp"
  case "$PLATFORM" in
    linux)
      if ! run_root test -d "$RUNTIME_ROOT"; then
        run_root mkdir -p "$RUNTIME_ROOT"
        run_root chmod 0700 "$RUNTIME_ROOT"
      fi
      run_root install -m 0600 "$state_tmp" "$ONBOARDING_STATE_FILE"
      ;;
    darwin)
      if [ ! -d "$RUNTIME_ROOT" ]; then
        mkdir -p "$RUNTIME_ROOT"
        chmod 0700 "$RUNTIME_ROOT"
      fi
      install -m 0600 "$state_tmp" "$ONBOARDING_STATE_FILE"
      ;;
  esac
}

clear_onboarding_stage() {
  case "$PLATFORM" in
    linux) run_root rm -f "$ONBOARDING_STATE_FILE" ;;
    darwin) rm -f "$ONBOARDING_STATE_FILE" ;;
  esac
}

case "$TUNNEL_MODE" in
  ''|none|quick|named) ;;
  *) die "Tunnel 模式必须是 none、quick 或 named。" ;;
esac

FRESH_INSTALL=false
if [ ! -x "$STABLE_BINARY" ]; then
  FRESH_INSTALL=true
fi

ONBOARDING_STAGE="$(read_onboarding_stage)"
RESTARTING_ONBOARDING=false
if [ -n "$ONBOARDING_STAGE" ]; then
  RESTARTING_ONBOARDING=true
  log "检测到上次安装未完成，重新开始安装流程。"
  write_onboarding_stage core
  ONBOARDING_STAGE=core
elif [ "$FRESH_INSTALL" = true ]; then
  # Core/Nexus/Tunnel 是一个完整的首次安装流程。中断后下次运行会重新从 Core 开始，
  # 不能仅凭 stable binary 已存在就把它误判成普通升级。
  write_onboarding_stage core
  ONBOARDING_STAGE=core
fi

REQUESTED_TUNNEL_MODE="$TUNNEL_MODE"
CORE_TUNNEL_MODE="$TUNNEL_MODE"
if [ -n "$ONBOARDING_STAGE" ]; then
  # onboarding 的 Core 阶段始终保持本地模式，Cloudflare 只在最后一步按需安装。
  CORE_TUNNEL_MODE=none
fi

validate_linux_cli_link

# 普通首次安装会从 Core 开始；只有当前这次执行已经推进到 nexus/tunnel 才跳过 Core。
if [ "$ONBOARDING_STAGE" != nexus ] && [ "$ONBOARDING_STAGE" != tunnel ]; then
  prepare_payload
fi

if [ "$PLATFORM" = linux ]; then
  if [ "$SERVICE_MANAGER" != none ]; then
    ensure_linux_service_user "$SERVICE_USER" "$DATA_DIR"
    SERVICE_GROUP="${AGENTDOCK_SERVICE_GROUP:-$(id -gn "$SERVICE_USER")}"
    run_root mkdir -p "$DATA_DIR"
    run_root chown "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
  else
    SERVICE_GROUP="${AGENTDOCK_SERVICE_GROUP:-$SERVICE_USER}"
  fi
fi

CLOUDFLARED_PATH=""
if [ -z "$ONBOARDING_STAGE" ]; then
  if [ "$CORE_TUNNEL_MODE" = named ]; then
    prepare_named_tunnel
  fi
  if [ "$CORE_TUNNEL_MODE" = quick ] || [ "$CORE_TUNNEL_MODE" = named ]; then
    CLOUDFLARED_PATH="$(install_cloudflared "$CLOUDFLARED_TARGET")"
  elif [ -z "$CORE_TUNNEL_MODE" ] && valid_cloudflared "$CLOUDFLARED_TARGET"; then
    # 普通升级保留现有 cloudflared 路径，但绝不因为“可能会用”而下载。
    CLOUDFLARED_PATH="$CLOUDFLARED_TARGET"
  fi
fi

if [ "$ONBOARDING_STAGE" != nexus ] && [ "$ONBOARDING_STAGE" != tunnel ]; then
  CORE_RESULT_FILE="$TMP_ROOT/install-result.json"
  run_install_engine install "$CORE_TUNNEL_MODE" "$CLOUDFLARED_PATH" "$CORE_RESULT_FILE" false
  install_linux_cli_link
  if [ "$ONBOARDING_STAGE" = core ]; then
    write_onboarding_stage nexus
    ONBOARDING_STAGE=nexus
  fi
else
  # Core 已在上一次运行中提交；补齐可能恰好在中断点前尚未创建的 CLI 链接即可。
  install_linux_cli_link
fi

if [ "$ONBOARDING_STAGE" = nexus ]; then
  configure_nexus
  write_onboarding_stage tunnel
  ONBOARDING_STAGE=tunnel
elif [ -n "$NEXUS_MODE" ]; then
  # 已完成 onboarding 的机器只有显式传入 --nexus/AGENTDOCK_NEXUS_MODE 才重新配对。
  configure_nexus
fi
if [ -n "$NEXUS_PAIR_CODE_FILE" ]; then rm -f "$NEXUS_PAIR_CODE_FILE"; fi

if [ "$ONBOARDING_STAGE" = tunnel ]; then
  TUNNEL_MODE="$REQUESTED_TUNNEL_MODE"

  # 普通同次执行里若 Tunnel 已成功提交、只差清状态，可以沿用；
  # 但若这是中断后的重跑，必须重新进入 Tunnel 选择，保持“从头开始”的交互语义。
  existing_tunnel_mode="$(read_tunnel_mode)"
  if [ "$RESTARTING_ONBOARDING" != true ] && [ -z "$TUNNEL_MODE" ] && { [ "$existing_tunnel_mode" = quick ] || [ "$existing_tunnel_mode" = named ]; }; then
    TUNNEL_MODE="$existing_tunnel_mode"
    log "检测到 Cloudflare Tunnel 已配置，继续完成安装。"
  fi

  if [ -z "$TUNNEL_MODE" ]; then
    if is_true "$NONINTERACTIVE"; then
      TUNNEL_MODE=none
    else
      choose_tunnel_mode
    fi
  fi

  case "$TUNNEL_MODE" in
    none) ;;
    quick|named)
      if [ "$existing_tunnel_mode" != "$TUNNEL_MODE" ]; then
        if [ "$TUNNEL_MODE" = named ]; then prepare_named_tunnel; fi
        CLOUDFLARED_PATH="$(install_cloudflared "$CLOUDFLARED_TARGET")"
        TUNNEL_RESULT_FILE="$TMP_ROOT/tunnel-result.json"
        # 新装首次开启公网时预生成稳定 OAuth 凭据。Quick Tunnel 拿到随机域名后只切换
        # OAuth enabled/Origin，不需要在后台进程里再生成或轮换凭据。
        run_install_engine repair "$TUNNEL_MODE" "$CLOUDFLARED_PATH" "$TUNNEL_RESULT_FILE" true
      fi
      ;;
    *) die "Tunnel 模式必须是 none、quick 或 named。" ;;
  esac

  clear_onboarding_stage
  ONBOARDING_STAGE=""
else
  TUNNEL_MODE="$(read_tunnel_mode)"
fi

if [ -n "$TUNNEL_TOKEN_FILE" ]; then rm -f "$TUNNEL_TOKEN_FILE"; fi

LOCAL_MCP_URL="$(read_local_mcp_url)"
ACCESS_TOKEN="$(read_auth_token)"
NEXUS_ENDPOINT="$(read_nexus_endpoint)"
FINAL_TUNNEL_MODE="$(read_tunnel_mode)"
PUBLIC_MCP_URL=""
OAUTH_PASSWORD=""

case "$FINAL_TUNNEL_MODE" in
  named)
    public_origin="$(read_server_url)"
    if [ -n "$public_origin" ]; then PUBLIC_MCP_URL="${public_origin%/}/mcp"; fi
    OAUTH_PASSWORD="$(read_oauth_password)"
    ;;
  quick)
    quick_url="$(wait_quick_tunnel_url || true)"
    if [ -n "$quick_url" ]; then PUBLIC_MCP_URL="${quick_url%/}/mcp"; fi
    OAUTH_PASSWORD="$(read_oauth_password)"
    ;;
esac

{
  printf '\nAgentDock 安装完成。\n'
  printf '安装目录：%s\n' "$INSTALL_ROOT"
  printf '运行配置：%s\n' "$RUNTIME_ROOT"
  if [ "$PLATFORM" = linux ]; then
    printf 'CLI：%s\n' "$CLI_LINK_PATH"
    printf '服务：%s（%s）\n' "$SERVICE_NAME" "$SERVICE_MANAGER"
  elif [ "$REGISTER_SERVICE" = true ]; then
    printf 'LaunchAgent：已注册\n'
  else
    printf 'CLI：%s\n' "$STABLE_BINARY"
  fi

  printf '\n连接信息：\n'
  printf '本地 MCP：%s\n' "$LOCAL_MCP_URL"
  if [ -n "$NEXUS_ENDPOINT" ]; then
    printf 'NexusDock：%s\n' "$NEXUS_ENDPOINT"
  else
    printf 'NexusDock：未配置\n'
  fi
  printf '访问令牌：%s\n' "$ACCESS_TOKEN"

  if [ "$FINAL_TUNNEL_MODE" = quick ] || [ "$FINAL_TUNNEL_MODE" = named ]; then
    printf '\nCloudflare Tunnel：%s\n' "$FINAL_TUNNEL_MODE"
    if [ -n "$PUBLIC_MCP_URL" ]; then
      printf '公网 MCP：%s\n' "$PUBLIC_MCP_URL"
    else
      printf '公网 MCP：Tunnel 启动后生成\n'
    fi
    printf 'OAuth 密码：%s\n' "$OAUTH_PASSWORD"
  fi
} >>"$TTY_OUT"
