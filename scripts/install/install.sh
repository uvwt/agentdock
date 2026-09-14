#!/bin/sh
set -eu

# AgentDock Unix 公开 bootstrap。
# 只负责：平台/架构识别、Release 载荷下载与校验、必要 OS bootstrap，随后交给 Go Installer Engine。
# 安装事务、配置保留、服务、health、Skill、Tunnel 与 rollback 只能由 agentdock install/uninstall 决策。

umask 077

DEFAULT_BASE_URL="https://github.com/uvwt/agentdock/releases/latest/download"
GITHUB_RELEASES_URL="https://github.com/uvwt/agentdock/releases"
CLOUDFLARED_BASE_URL="${AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL:-https://github.com/cloudflare/cloudflared/releases/latest/download}"
BASE_URL="${AGENTDOCK_INSTALLER_BASE_URL:-$DEFAULT_BASE_URL}"
RELEASE_VERSION="${AGENTDOCK_RELEASE_VERSION:-latest}"
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
  sh install.sh --tunnel none|quick|named [--server-url URL] [--tunnel-token-file FILE]
  sh install.sh --register-service [--no-start]
  sh install.sh --version latest|vX.Y.Z
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
    printf '%s' "$target"
    return
  fi
  if [ -z "$source" ]; then
    discovered="$(command -v cloudflared 2>/dev/null || true)"
    if valid_cloudflared "$discovered"; then
      source="$discovered"
    fi
  fi
  if [ -z "$source" ]; then
    case "$PLATFORM" in
      linux)
        source="$TMP_ROOT/cloudflared"
        log "下载 cloudflared-linux-$ARCH"
        download_file "$CLOUDFLARED_BASE_URL/cloudflared-linux-$ARCH" "$source"
        chmod 700 "$source"
        ;;
      darwin)
        archive="$TMP_ROOT/cloudflared.tgz"
        cloud_dir="$TMP_ROOT/cloudflared-extract"
        log "下载 cloudflared-darwin-$ARCH.tgz"
        download_file "$CLOUDFLARED_BASE_URL/cloudflared-darwin-$ARCH.tgz" "$archive"
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

公网访问：
1) 仅本机
2) 临时公网地址
3) Cloudflare 固定地址
CHOICE
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1|none) TUNNEL_MODE=none ;;
    2|quick) TUNNEL_MODE=quick ;;
    3|named) TUNNEL_MODE=named ;;
    *) die "无效选择：$choice" ;;
  esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || die "--version 缺少值"
      RELEASE_VERSION="$2"; shift 2 ;;
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

case "$RELEASE_VERSION" in
  latest|'') ;;
  v[0-9]*.[0-9]*.[0-9]*)
    if [ -z "${AGENTDOCK_INSTALLER_BASE_URL:-}" ]; then
      BASE_URL="$GITHUB_RELEASES_URL/download/$RELEASE_VERSION"
    fi
    ;;
  *) die "版本必须是 latest 或 vX.Y.Z：$RELEASE_VERSION" ;;
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
    CLOUDFLARED_TARGET="${AGENTDOCK_CLOUDFLARED_INSTALL_PATH:-/usr/local/bin/cloudflared}"
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

if [ -z "$TUNNEL_MODE" ] && [ ! -x "$STABLE_BINARY" ]; then
  if is_true "$NONINTERACTIVE"; then
    TUNNEL_MODE=none
  else
    choose_tunnel_mode
  fi
fi
case "$TUNNEL_MODE" in
  ''|none|quick|named) ;;
  *) die "Tunnel 模式必须是 none、quick 或 named。" ;;
esac

if [ "$TUNNEL_MODE" = named ]; then
  if [ -z "$SERVER_URL" ]; then
    is_true "$NONINTERACTIVE" && die "Named Tunnel 必须提供 --server-url"
    SERVER_URL="$(prompt_value 'HTTPS 公网地址')"
  fi
  case "$SERVER_URL" in https://*) ;; *) die "Named Tunnel 公网地址必须是 https:// URL" ;; esac
  if [ -z "${AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN:-}" ] && [ -z "$TUNNEL_TOKEN_FILE" ]; then
    is_true "$NONINTERACTIVE" && die "Named Tunnel 必须通过环境变量或 --tunnel-token-file 提供 Token"
    AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN="$(prompt_secret 'Cloudflare Tunnel Token')"
    export AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN
  fi
fi

prepare_payload
CLOUDFLARED_PATH=""
if [ "$TUNNEL_MODE" = quick ] || [ "$TUNNEL_MODE" = named ]; then
  CLOUDFLARED_PATH="$(install_cloudflared "$CLOUDFLARED_TARGET")"
elif valid_cloudflared "$CLOUDFLARED_TARGET"; then
  CLOUDFLARED_PATH="$CLOUDFLARED_TARGET"
fi

case "$PLATFORM" in
  linux)
    if [ "$SERVICE_MANAGER" != none ]; then
      ensure_linux_service_user "$SERVICE_USER" "$DATA_DIR"
      SERVICE_GROUP="${AGENTDOCK_SERVICE_GROUP:-$(id -gn "$SERVICE_USER")}"
      run_root mkdir -p "$DATA_DIR"
      run_root chown "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
    else
      SERVICE_GROUP="${AGENTDOCK_SERVICE_GROUP:-$SERVICE_USER}"
    fi
    set -- install --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
      --payload-dir "$PAYLOAD_DIR" --service-name "$SERVICE_NAME" \
      --service-user "$SERVICE_USER" --service-group "$SERVICE_GROUP" \
      --service-manager "$SERVICE_MANAGER" --data-dir "$DATA_DIR" \
      --agentdock-home "$AGENTDOCK_HOME_DIR" --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE" \
      --systemd-dir "$SYSTEMD_DIR" --openrc-dir "$OPENRC_DIR"
    if [ "$SERVICE_MANAGER" = none ] || [ "$NO_START" = true ]; then set -- "$@" --no-start --skip-health; fi
    ;;
  darwin)
    set -- install --install-root "$INSTALL_ROOT" --runtime-root "$RUNTIME_ROOT" \
      --payload-dir "$PAYLOAD_DIR" --live-binary "$STABLE_BINARY" \
      --data-dir "$DATA_DIR" --agentdock-home "$AGENTDOCK_HOME_DIR" \
      --agentdock-default-dir "$AGENTDOCK_DEFAULT_DIR_VALUE" \
      --launch-agents-dir "$LAUNCH_AGENTS_DIR"
    if [ "$REGISTER_SERVICE" = true ]; then
      set -- "$@" --register-service
      if [ "$NO_START" = true ]; then set -- "$@" --no-start --skip-health; fi
    else
      set -- "$@" --no-start --skip-health
    fi
    ;;
esac
if [ -n "$HOST_VALUE" ]; then set -- "$@" --host "$HOST_VALUE"; fi
if [ -n "$PORT_VALUE" ]; then set -- "$@" --port "$PORT_VALUE"; fi
if [ -n "$LOG_LEVEL_VALUE" ]; then set -- "$@" --log-level "$LOG_LEVEL_VALUE"; fi
if [ -n "$TUNNEL_MODE" ]; then set -- "$@" --tunnel-mode "$TUNNEL_MODE"; fi
if [ -n "$SERVER_URL" ]; then set -- "$@" --server-url "$SERVER_URL"; fi
if [ -n "$TUNNEL_TOKEN_FILE" ]; then set -- "$@" --token-file "$TUNNEL_TOKEN_FILE"; fi
if [ -n "$CLOUDFLARED_PATH" ]; then set -- "$@" --cloudflared "$CLOUDFLARED_PATH"; fi

RESULT_FILE="$TMP_ROOT/install-result.json"
case "$PLATFORM" in
  linux) run_engine_root "$ENGINE" "$@" >"$RESULT_FILE" ;;
  darwin) "$ENGINE" "$@" >"$RESULT_FILE" ;;
esac

if [ -n "$TUNNEL_TOKEN_FILE" ]; then rm -f "$TUNNEL_TOKEN_FILE"; fi

{
  printf '\nAgentDock 安装完成。\n'
  printf '安装目录：%s\n' "$INSTALL_ROOT"
  printf '运行配置：%s\n' "$RUNTIME_ROOT"
  case "$PLATFORM" in
    linux) printf '服务：%s（%s）\n' "$SERVICE_NAME" "$SERVICE_MANAGER" ;;
    darwin)
      if [ "$REGISTER_SERVICE" = true ]; then
        printf 'LaunchAgent：已注册\n'
      else
        printf 'CLI：%s\n' "$STABLE_BINARY"
      fi
      ;;
  esac
  if [ "$TUNNEL_MODE" = quick ]; then
    printf '临时公网地址由 Tunnel 启动后写入运行配置。\n'
  elif [ "$TUNNEL_MODE" = named ]; then
    printf '公网地址：%s/mcp\n' "${SERVER_URL%/}"
  fi
} >>"$TTY_OUT"
