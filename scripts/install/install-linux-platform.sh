#!/usr/bin/env bash
set -Eeuo pipefail

MIN_GO_VERSION="1.22"
DEFAULT_GO_VERSION="${AGENTDOCK_GO_VERSION:-1.22.12}"
DEFAULT_REPO_URL="${AGENTDOCK_REPO_URL:-https://github.com/uvwt/agentdock.git}"
DEFAULT_BRANCH="${AGENTDOCK_BRANCH:-main}"
DEFAULT_SOURCE_DIR="${AGENTDOCK_SOURCE_DIR:-/opt/agentdock}"
DEFAULT_DATA_DIR="${AGENTDOCK_DATA_DIR:-/srv/agentdock}"
DEFAULT_ENV_FILE="${AGENTDOCK_ENV_FILE:-/etc/agentdock/agentdock.env}"
DEFAULT_SERVICE_NAME="${AGENTDOCK_SERVICE_NAME:-agentdock}"
DEFAULT_SERVICE_USER="${AGENTDOCK_SERVICE_USER:-agentdock}"
DEFAULT_HOST="${AGENTDOCK_HOST:-127.0.0.1}"
DEFAULT_PORT="${AGENTDOCK_PORT:-8765}"
DEFAULT_LOG_LEVEL="${AGENTDOCK_LOG_LEVEL:-info}"
DEFAULT_SERVICE_MANAGER="${AGENTDOCK_SERVICE_MANAGER:-auto}"
DEFAULT_INSTALL_MODE="${AGENTDOCK_INSTALL_MODE:-binary}"
DEFAULT_RELEASE_VERSION="${AGENTDOCK_RELEASE_VERSION:-latest}"
DEFAULT_TUNNEL_MODE="${AGENTDOCK_TUNNEL_MODE:-}"
DEFAULT_SERVER_URL="${AGENTDOCK_SERVER_URL:-}"
DEFAULT_CLOUDFLARED_BINARY="${AGENTDOCK_CLOUDFLARED_INSTALL_PATH:-/usr/local/bin/cloudflared}"
CLOUDFLARED_RELEASE_BASE_URL="${AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL:-https://github.com/cloudflare/cloudflared/releases/latest/download}"
CLOUDFLARED_SOURCE_BINARY="${AGENTDOCK_CLOUDFLARED_BINARY:-}"
CORE_SKILL_BUNDLE=""
CORE_SKILL_TEMP_DIR=""
TUNNEL_PUBLIC_URL=""

cleanup_core_skill_bundle() {
  if [[ -n "$CORE_SKILL_TEMP_DIR" ]]; then
    rm -rf "$CORE_SKILL_TEMP_DIR"
  fi
}
trap cleanup_core_skill_bundle EXIT

TTY_IN="/dev/tty"
TTY_OUT="/dev/tty"
if ! ( : <"$TTY_IN" ) 2>/dev/null; then
  TTY_IN="/dev/stdin"
fi
if ! ( : >"$TTY_OUT" ) 2>/dev/null; then
  TTY_OUT="/dev/stderr"
fi

noninteractive_enabled() {
  case "${AGENTDOCK_NONINTERACTIVE:-false}" in
    1|true|TRUE|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

if noninteractive_enabled; then
  TTY_IN="/dev/stdin"
  TTY_OUT="/dev/stderr"
fi

usage() {
  cat <<'USAGE'
AgentDock Linux 问答式一键部署脚本。

用法：
  curl -fsSL https://github.com/uvwt/agentdock/releases/latest/download/install.sh -o /tmp/agentdock-install.sh
  sh /tmp/agentdock-install.sh

Alpine/极简系统如果没有 curl/bash：
  apk add --no-cache bash curl
  curl -fsSL https://github.com/uvwt/agentdock/releases/latest/download/install.sh -o /tmp/agentdock-install.sh
  sh /tmp/agentdock-install.sh

环境变量可覆盖默认值：
  AGENTDOCK_INSTALL_MODE、AGENTDOCK_RELEASE_VERSION、AGENTDOCK_NONINTERACTIVE
  AGENTDOCK_REPO_URL、AGENTDOCK_BRANCH、AGENTDOCK_SOURCE_DIR、AGENTDOCK_DATA_DIR、AGENTDOCK_ENV_FILE
  AGENTDOCK_SERVICE_NAME、AGENTDOCK_SERVICE_USER、AGENTDOCK_HOST、AGENTDOCK_PORT
  AGENTDOCK_AUTH_TOKEN、AGENTDOCK_GO_VERSION、AGENTDOCK_SERVER_URL
  AGENTDOCK_OAUTH_PASSWORD、AGENTDOCK_OAUTH_TOKEN_SECRET
  AGENTDOCK_TUNNEL_MODE、AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN
  AGENTDOCK_CLOUDFLARED_BINARY、AGENTDOCK_CLOUDFLARED_INSTALL_PATH

参数：
  -h, --help    显示帮助，不执行部署
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

log() { printf '==> %s\n' "$*" >&2; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

prompt() {
  local label="$1"
  local default_value="${2:-}"
  local answer=""
  if noninteractive_enabled; then
    printf '%s' "$default_value"
    return
  fi
  if [[ -n "$default_value" ]]; then
    printf '%s [%s]: ' "$label" "$default_value" >"$TTY_OUT"
  else
    printf '%s: ' "$label" >"$TTY_OUT"
  fi
  IFS= read -r answer <"$TTY_IN" || true
  if [[ -z "$answer" ]]; then
    printf '%s' "$default_value"
  else
    printf '%s' "$answer"
  fi
}

prompt_secret() {
  local label="$1"
  local answer=""
  if noninteractive_enabled; then
    printf ''
    return
  fi
  printf '%s（输入不回显，留空自动生成）: ' "$label" >"$TTY_OUT"
  stty -echo <"$TTY_IN" 2>/dev/null || true
  IFS= read -r answer <"$TTY_IN" || true
  stty echo <"$TTY_IN" 2>/dev/null || true
  printf '\n' >"$TTY_OUT"
  printf '%s' "$answer"
}

prompt_required_secret() {
  local label="$1"
  local answer=""
  if noninteractive_enabled; then
    printf ''
    return
  fi
  printf '%s（输入不回显）: ' "$label" >"$TTY_OUT"
  stty -echo <"$TTY_IN" 2>/dev/null || true
  IFS= read -r answer <"$TTY_IN" || true
  stty echo <"$TTY_IN" 2>/dev/null || true
  printf '\n' >"$TTY_OUT"
  printf '%s' "$answer"
}

choose_tunnel_mode() {
  cat >"$TTY_OUT" <<'CHOICE'

请选择公网访问方式：
- 有自己的 Cloudflare 域名：使用固定地址，适合长期运行和 OAuth。
- 没有域名：自动生成临时地址，适合快速体验；Tunnel 重启后地址可能变化。
CHOICE
  if confirm '你是否有已接入 Cloudflare 的域名？' n; then
    printf 'named'
  else
    printf 'quick'
  fi
}

confirm() {
  local label="$1"
  local default_value="${2:-y}"
  local answer=""
  if noninteractive_enabled; then
    [[ "$default_value" == "y" ]]
    return
  fi
  while true; do
    if [[ "$default_value" == "y" ]]; then
      printf '%s [Y/n]: ' "$label" >"$TTY_OUT"
    else
      printf '%s [y/N]: ' "$label" >"$TTY_OUT"
    fi
    IFS= read -r answer <"$TTY_IN" || true
    answer="${answer:-$default_value}"
    case "${answer,,}" in
      y|yes) return 0 ;;
      n|no) return 1 ;;
      *) printf '请输入 y 或 n。\n' >"$TTY_OUT" ;;
    esac
  done
}

require_linux() {
  [[ "$(uname -s)" == "Linux" ]] || die "此脚本只支持 Linux；macOS 请使用 install.sh。"
}

detect_service_manager() {
  local requested="${1:-auto}"
  case "$requested" in
    auto) ;;
    systemd|openrc|none) printf '%s' "$requested"; return ;;
    *) die "服务管理器必须是 auto/systemd/openrc/none：$requested" ;;
  esac
  if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system || -d /etc/systemd/system ]]; then
    printf 'systemd'
  elif [[ -f /etc/alpine-release ]]; then
    printf 'openrc'
  elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    printf 'openrc'
  else
    printf 'none'
  fi
}

is_alpine() {
  [[ -f /etc/alpine-release ]]
}

run_root() {
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

run_as_service_user() {
  local user="$1"
  local home_dir="$2"
  shift 2

  # 安装器可能由另一个 AgentDock 实例启动，不能让父进程的实例目录泄漏到新实例。
  # 核心 Skill 必须始终写入本次部署选择的数据目录。
  if [[ "$(id -u)" == "$(id -u "$user")" ]]; then
    env HOME="$home_dir" \
      AGENTDOCK_HOME="$home_dir/.agentdock" \
      AGENTDOCK_DEFAULT_DIR="$home_dir/AgentDock" \
      "$@"
  elif command -v runuser >/dev/null 2>&1; then
    run_root runuser -u "$user" -- env HOME="$home_dir" \
      AGENTDOCK_HOME="$home_dir/.agentdock" \
      AGENTDOCK_DEFAULT_DIR="$home_dir/AgentDock" \
      "$@"
  elif command -v su >/dev/null 2>&1; then
    # 单引号中的位置参数由 su 启动的 /bin/sh 展开。
    # shellcheck disable=SC2016
    run_root su -s /bin/sh "$user" -c 'HOME="$1"; AGENTDOCK_HOME="$1/.agentdock"; AGENTDOCK_DEFAULT_DIR="$1/AgentDock"; export HOME AGENTDOCK_HOME AGENTDOCK_DEFAULT_DIR; shift; exec "$@"' sh "$home_dir" "$@"
  else
    die "缺少 runuser 或 su，无法以运行用户初始化核心 Skill：$user"
  fi
}

make_core_skill_bundle_readable() {
  local path
  [[ -n "$CORE_SKILL_TEMP_DIR" && -n "$CORE_SKILL_BUNDLE" ]] || die "核心 Skill Bundle 尚未准备"

  # mktemp 默认创建 0700 目录。Bundle 需要由服务用户读取，但临时目录中的
  # Release 压缩包和校验文件不需要写权限，因此只开放目录穿越和包只读权限。
  run_root chmod 0755 "$CORE_SKILL_TEMP_DIR"
  path="$(dirname "$CORE_SKILL_BUNDLE")"
  while [[ "$path" != "$CORE_SKILL_TEMP_DIR" && "$path" != "/" ]]; do
    run_root chmod 0755 "$path"
    path="$(dirname "$path")"
  done
  run_root find "$CORE_SKILL_BUNDLE" -type d -exec chmod 0755 {} +
  run_root find "$CORE_SKILL_BUNDLE" -type f -exec chmod 0644 {} +
}

validate_no_space() {
  local name="$1"
  local value="$2"
  [[ "$value" != *[[:space:]]* ]] || die "$name 不能包含空白字符：$value"
}

validate_abs_path() {
  local name="$1"
  local value="$2"
  [[ "$value" == /* ]] || die "$name 必须是绝对路径：$value"
  validate_no_space "$name" "$value"
}

validate_port() {
  local value="$1"
  [[ "$value" =~ ^[0-9]+$ ]] || die "端口必须是数字：$value"
  (( value >= 1 && value <= 65535 )) || die "端口范围必须是 1-65535：$value"
}

validate_tunnel_mode() {
  case "$1" in
    none|quick|named) ;;
    *) die "公网访问模式必须是 none、quick 或 named：$1" ;;
  esac
}

normalize_server_url() {
  local value="${1%/}"
  [[ "$value" == https://* ]] || die "Named Tunnel 公网地址必须使用 HTTPS：$1"
  local authority="${value#https://}"
  [[ -n "$authority" && "$authority" != */* && "$authority" =~ ^[A-Za-z0-9._:-]+$ ]] || \
    die "公网地址只能填写 HTTPS Origin，不能包含路径或特殊字符：$1"
  printf '%s' "$value"
}

validate_host() {
  [[ "$1" =~ ^[A-Za-z0-9.:-]+$ ]] || die "监听地址包含不支持的字符：$1"
}

validate_cloudflare_token() {
  [[ "$1" =~ ^[A-Za-z0-9._=-]+$ ]] || die "Cloudflare Tunnel Token 格式无效"
}

read_env_assignment() {
  local file_path="$1"
  local key="$2"
  [[ -f "$file_path" ]] || return 0
  # shellcheck disable=SC2016
  run_root awk -F= -v key="$key" '$1 == key {value=substr($0, index($0, "=") + 1)} END {print value}' "$file_path"
}

semver_ge() {
  local current="$1"
  local required="$2"
  local c_major c_minor c_patch r_major r_minor r_patch
  IFS=. read -r c_major c_minor c_patch <<<"$current"
  IFS=. read -r r_major r_minor r_patch <<<"$required"
  c_major="${c_major:-0}"; c_minor="${c_minor:-0}"; c_patch="${c_patch:-0}"
  r_major="${r_major:-0}"; r_minor="${r_minor:-0}"; r_patch="${r_patch:-0}"
  (( c_major > r_major )) && return 0
  (( c_major < r_major )) && return 1
  (( c_minor > r_minor )) && return 0
  (( c_minor < r_minor )) && return 1
  (( c_patch >= r_patch ))
}

current_go_version() {
  if ! command -v go >/dev/null 2>&1; then
    return 1
  fi
  go version | awk '{print $3}' | sed 's/^go//' | sed 's/[^0-9.].*$//'
}

install_runtime_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    run_root apt-get update
    run_root env DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates tar gzip openssl
  elif command -v dnf >/dev/null 2>&1; then
    run_root dnf install -y curl ca-certificates tar gzip openssl
  elif command -v yum >/dev/null 2>&1; then
    run_root yum install -y curl ca-certificates tar gzip openssl
  elif command -v pacman >/dev/null 2>&1; then
    run_root pacman -Sy --needed --noconfirm curl ca-certificates tar gzip openssl
  elif command -v zypper >/dev/null 2>&1; then
    run_root zypper --non-interactive install curl ca-certificates tar gzip openssl
  elif command -v apk >/dev/null 2>&1; then
    run_root apk add --no-cache bash curl ca-certificates tar gzip openssl openrc
  else
    die "未识别包管理器；请先安装 curl、ca-certificates、tar、gzip、openssl。"
  fi
}

install_build_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    run_root apt-get update
    run_root env DEBIAN_FRONTEND=noninteractive apt-get install -y git curl ca-certificates make gcc g++ pkg-config python3 tar gzip openssl
  elif command -v dnf >/dev/null 2>&1; then
    run_root dnf install -y git curl ca-certificates make gcc gcc-c++ pkgconfig python3 tar gzip openssl
  elif command -v yum >/dev/null 2>&1; then
    run_root yum install -y git curl ca-certificates make gcc gcc-c++ pkgconfig python3 tar gzip openssl
  elif command -v pacman >/dev/null 2>&1; then
    run_root pacman -Sy --needed --noconfirm git curl ca-certificates make gcc pkgconf python tar gzip openssl
  elif command -v zypper >/dev/null 2>&1; then
    run_root zypper --non-interactive install git curl ca-certificates make gcc gcc-c++ pkg-config python3 tar gzip openssl
  elif command -v apk >/dev/null 2>&1; then
    run_root apk add --no-cache bash curl ca-certificates git go build-base pkgconf python3 tar gzip openssl openrc
  else
    die "未识别包管理器；请先安装 git、curl、ca-certificates、make、gcc、python3、tar、gzip、openssl。"
  fi
}

install_go_official() {
  local version="$1"
  local machine go_arch url tmp_dir
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) go_arch="amd64" ;;
    aarch64|arm64) go_arch="arm64" ;;
    *) die "暂不支持自动安装 Go 的架构：$machine" ;;
  esac
  url="https://go.dev/dl/go${version}.linux-${go_arch}.tar.gz"
  tmp_dir="$(mktemp -d)"
  log "下载 Go $version: $url"
  curl -fL "$url" -o "$tmp_dir/go.tgz"
  run_root rm -rf /usr/local/go
  run_root tar -C /usr/local -xzf "$tmp_dir/go.tgz"
  rm -rf "$tmp_dir"
  export PATH="/usr/local/go/bin:$PATH"
  if [[ -d /etc/profile.d ]]; then
    # 需要将字面量 $PATH 写入 profile 脚本。
    # shellcheck disable=SC2016
    printf '%s\n' 'export PATH=/usr/local/go/bin:$PATH' | run_root tee /etc/profile.d/agentdock-go.sh >/dev/null
  fi
}

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  elif command -v python3 >/dev/null 2>&1; then
    python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
  else
    LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 64
    printf '\n'
  fi
}

generate_oauth_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 12
  elif command -v python3 >/dev/null 2>&1; then
    python3 - <<'PYGEN'
import secrets
print(secrets.token_hex(12))
PYGEN
  else
    LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24
    printf '\n'
  fi
}

service_user_exists() {
  id "$1" >/dev/null 2>&1
}

ensure_service_user() {
  local user="$1"
  local home_dir="$2"
  if service_user_exists "$user"; then
    return
  fi
  log "创建运行用户：$user"
  if command -v useradd >/dev/null 2>&1; then
    run_root useradd --system --home-dir "$home_dir" --create-home --shell /usr/sbin/nologin "$user"
  elif command -v adduser >/dev/null 2>&1; then
    run_root addgroup -S "$user" 2>/dev/null || true
    run_root adduser -S -D -H -h "$home_dir" -s /sbin/nologin -G "$user" "$user"
    run_root mkdir -p "$home_dir"
  else
    die "未找到 useradd/adduser，无法创建运行用户：$user"
  fi
}


release_repo_slug() {
  local repo_url="$1"
  local slug
  slug="$repo_url"
  slug="${slug#https://github.com/}"
  slug="${slug#git@github.com:}"
  slug="${slug%.git}"
  [[ "$slug" == */* ]] || die "无法从仓库 URL 推导 GitHub repo slug：$repo_url"
  printf '%s' "$slug"
}

release_arch() {
  local machine
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) die "暂不支持预编译二进制架构：$machine" ;;
  esac
}

release_download_url() {
  local repo_url="$1"
  local version="$2"
  local arch repo_slug file_name base
  arch="$(release_arch)"
  file_name="agentdock_linux_${arch}.tar.gz"
  if [[ -n "${AGENTDOCK_RELEASE_BASE_URL:-}" ]]; then
    base="${AGENTDOCK_RELEASE_BASE_URL%/}"
  else
    repo_slug="$(release_repo_slug "$repo_url")"
    if [[ "$version" == "latest" ]]; then
      base="https://github.com/${repo_slug}/releases/latest/download"
    else
      base="https://github.com/${repo_slug}/releases/download/${version}"
    fi
  fi
  printf '%s/%s' "$base" "$file_name"
}

install_prebuilt_binary() {
  local repo_url="$1"
  local version="$2"
  local source_dir="$3"
  local url tmp_dir file_name tmp_tgz tmp_checksum
  url="$(release_download_url "$repo_url" "$version")"
  tmp_dir="$(mktemp -d)"
  file_name="${url##*/}"
  tmp_tgz="$tmp_dir/$file_name"
  tmp_checksum="$tmp_tgz.sha256"
  log "下载预编译 AgentDock：$url"
  if ! curl -fL "$url" -o "$tmp_tgz"; then
    rm -rf "$tmp_dir"
    return 1
  fi
  if ! curl -fL "$url.sha256" -o "$tmp_checksum"; then
    rm -rf "$tmp_dir"
    die "无法下载预编译包校验文件：$url.sha256"
  fi
  log "校验预编译包 SHA-256"
  if ! (cd "$tmp_dir" && sha256sum -c "$file_name.sha256"); then
    rm -rf "$tmp_dir"
    die "预编译包 SHA-256 校验失败：$url"
  fi
  run_root mkdir -p "$source_dir/bin"
  tar -xzf "$tmp_tgz" -C "$tmp_dir"
  local bundle_dir="$tmp_dir/share/agentdock/core-skills"
  if [[ ! -d "$bundle_dir" || -L "$bundle_dir" || ! -f "$bundle_dir/manifest.json" || -L "$bundle_dir/manifest.json" ]]; then
    rm -rf "$tmp_dir"
    die "预编译包内缺少有效的核心 Skill Bundle：$url"
  fi
  if [[ -x "$tmp_dir/bin/agentdock" ]]; then
    run_root install -m 755 "$tmp_dir/bin/agentdock" "$source_dir/bin/agentdock"
  elif [[ -x "$tmp_dir/agentdock" ]]; then
    run_root install -m 755 "$tmp_dir/agentdock" "$source_dir/bin/agentdock"
  else
    rm -rf "$tmp_dir"
    die "预编译包内未找到 agentdock 可执行文件：$url"
  fi
  CORE_SKILL_TEMP_DIR="$tmp_dir"
  CORE_SKILL_BUNDLE="$bundle_dir"
}

validate_install_mode() {
  local mode="$1"
  case "$mode" in
    binary|source|auto) ;;
    *) die "安装方式必须是 binary/source/auto：$mode" ;;
  esac
}

clone_or_update_source() {
  local repo_url="$1"
  local branch="$2"
  local source_dir="$3"
  local update_existing="$4"
  local installer_user installer_group parent
  installer_user="${SUDO_USER:-$(id -un)}"
  installer_group="$(id -gn "$installer_user" 2>/dev/null || printf '%s' "$installer_user")"
  parent="$(dirname "$source_dir")"

  if [[ -d "$source_dir" && "${EUID:-$(id -u)}" -ne 0 && ! -w "$source_dir" ]]; then
    log "调整安装目录所有者，确保当前安装用户可构建：$source_dir"
    run_root chown -R "$installer_user:$installer_group" "$source_dir"
  fi

  if [[ -d "$source_dir/.git" ]]; then
    log "使用已有 Git 安装目录：$source_dir"
    if [[ "$update_existing" == "yes" ]]; then
      if git -C "$source_dir" diff --quiet && git -C "$source_dir" diff --cached --quiet; then
        git -C "$source_dir" fetch --tags origin "$branch"
        git -C "$source_dir" checkout "$branch"
        git -C "$source_dir" pull --ff-only origin "$branch"
      else
        warn "安装目录存在未提交改动，跳过 git pull：$source_dir"
      fi
    fi
    return
  fi

  if [[ -d "$source_dir/cmd/agentdock" ]]; then
    log "使用已有非 Git 安装目录：$source_dir"
    return
  fi

  log "克隆 AgentDock：$repo_url -> $source_dir"
  run_root mkdir -p "$parent"
  if [[ -w "$parent" ]]; then
    git clone --branch "$branch" "$repo_url" "$source_dir"
  else
    run_root git clone --branch "$branch" "$repo_url" "$source_dir"
    if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
      run_root chown -R "$installer_user:$installer_group" "$source_dir"
    fi
  fi
}

write_env_file() {
  local env_file="$1"
  local host="$2"
  local port="$3"
  local token="$4"
  local log_level="$5"
  local server_url="$6"
  local configure_oauth="$7"
  local oauth_enabled="$8"
  local oauth_password="$9"
  local oauth_token_secret="${10}"

  local env_dir tmp_file managed_keys
  env_dir="$(dirname "$env_file")"
  tmp_file="$(mktemp)"
  managed_keys='AGENTDOCK_HOST|AGENTDOCK_PORT|AGENTDOCK_AUTH_TOKEN|AGENTDOCK_LOG_LEVEL|AGENTDOCK_NEXUS_ENDPOINT|AGENTDOCK_NEXUS_TOKEN|AGENTDOCK_SERVER_URL'
  if [[ "$configure_oauth" == "yes" ]]; then
    managed_keys+='|AGENTDOCK_OAUTH_ENABLED|AGENTDOCK_OAUTH_PASSWORD|AGENTDOCK_OAUTH_TOKEN_SECRET'
  fi

  # 重跑安装器时保留浏览器、代理和其他高级配置，只替换本次安装器负责的键。
  # 同时兼容用户手工写入的 `export KEY=...` 形式，避免重复定义。
  if [[ -f "$env_file" ]]; then
    # shellcheck disable=SC2016
    run_root awk -v keys="$managed_keys"       '$0 !~ "^[[:space:]]*(export[[:space:]]+)?(" keys ")[[:space:]]*="'       "$env_file" >"$tmp_file"
  fi

  {
    cat <<ENV
AGENTDOCK_HOST=$host
AGENTDOCK_PORT=$port
AGENTDOCK_AUTH_TOKEN=$token
AGENTDOCK_LOG_LEVEL=$log_level
ENV
    if [[ -n "$server_url" ]]; then
      printf 'AGENTDOCK_SERVER_URL=%s\n' "$server_url"
    fi
    if [[ "$configure_oauth" == "yes" ]]; then
      printf 'AGENTDOCK_OAUTH_ENABLED=%s\n' "$oauth_enabled"
      printf 'AGENTDOCK_OAUTH_PASSWORD=%s\n' "$oauth_password"
      printf 'AGENTDOCK_OAUTH_TOKEN_SECRET=%s\n' "$oauth_token_secret"
    fi
  } >>"$tmp_file"

  run_root mkdir -p "$env_dir"
  run_root install -m 600 -o root -g root "$tmp_file" "$env_file"
  rm -f "$tmp_file"
}

write_systemd_unit() {
  local service_name="$1"
  local service_user="$2"
  local service_group="$3"
  local source_dir="$4"
  local env_file="$5"
  local runtime_root
  runtime_root="$(dirname "$env_file")"
  local unit_file="/etc/systemd/system/${service_name}.service"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<UNIT
[Unit]
Description=AgentDock MCP server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$service_user
Group=$service_group
WorkingDirectory=$source_dir
EnvironmentFile=$env_file
ExecStart=$source_dir/bin/agentdock service launch-core --runtime-root $runtime_root
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
  run_root install -m 644 -o root -g root "$tmp_file" "$unit_file"
  rm -f "$tmp_file"
}

write_openrc_service() {
  local service_name="$1"
  local service_user="$2"
  local service_group="$3"
  local source_dir="$4"
  local env_file="$5"
  local runtime_root
  runtime_root="$(dirname "$env_file")"
  local init_file="/etc/init.d/${service_name}"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<OPENRC
#!/sbin/openrc-run
name="AgentDock MCP server"
description="AgentDock MCP server"
command="$source_dir/bin/agentdock"
command_args="service launch-core --runtime-root $runtime_root"
command_user="$service_user:$service_group"
directory="$source_dir"
pidfile="/run/${service_name}.pid"
command_background="yes"
log_dir="/var/log/${service_name}"
output_log="/dev/null"
error_log="/dev/null"

agentdock_env_file="$env_file"

start_pre() {
  checkpath -d -m 0750 -o "$service_user:$service_group" "\$log_dir"
  if [ -r "\$agentdock_env_file" ]; then
    set -a
    . "\$agentdock_env_file"
    set +a
  else
    eerror "env file not readable: \$agentdock_env_file"
    return 1
  fi
}

depend() {
  need net
  after firewall
}
OPENRC
  run_root install -m 755 -o root -g root "$tmp_file" "$init_file"
  rm -f "$tmp_file"
}

write_runtime_manifest() {
  local service_manager="$1"
  local service_name="$2"
  local tunnel_service_name="$3"
  local source_dir="$4"
  local env_file="$5"
  local cloudflared_binary="$6"
  local cloudflared_env_file="$7"
  local runtime_root tmp_file
  runtime_root="$(dirname "$env_file")"
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<JSON
{
  "schema_version": 1,
  "service_manager": "$service_manager",
  "service_name": "$service_name",
  "tunnel_service_name": "$tunnel_service_name",
  "agentdock_binary": "$source_dir/bin/agentdock",
  "cloudflared_binary": "$cloudflared_binary",
  "environment_file": "$env_file",
  "tunnel_environment": "$cloudflared_env_file"
}
JSON
  run_root mkdir -p "$runtime_root"
  run_root install -m 0644 -o root -g root "$tmp_file" "$runtime_root/desktop-runtime.json"
  rm -f "$tmp_file"
}

resolve_cloudflared_binary() {
  local candidate="$1"
  local link_target resolved_dir
  local link_count=0

  [[ -n "$candidate" ]] || return 1
  if [[ "$candidate" != /* ]]; then
    if [[ "$candidate" == */* ]]; then
      resolved_dir="$(cd -P "$(dirname "$candidate")" 2>/dev/null && pwd)" || return 1
      candidate="$resolved_dir/$(basename "$candidate")"
    else
      candidate="$(command -v "$candidate" 2>/dev/null || true)"
    fi
  fi
  [[ -n "$candidate" ]] || return 1

  # 包管理器通常通过软链接暴露命令。安装服务前复制真实文件到固定路径，
  # 避免服务依赖登录 shell 的 PATH，也避免覆盖软链接指向的外部文件。
  while [[ -L "$candidate" ]]; do
    (( link_count += 1 ))
    (( link_count <= 40 )) || return 1
    link_target="$(readlink "$candidate")" || return 1
    if [[ "$link_target" == /* ]]; then
      candidate="$link_target"
    else
      candidate="$(dirname "$candidate")/$link_target"
    fi
  done

  resolved_dir="$(cd -P "$(dirname "$candidate")" 2>/dev/null && pwd)" || return 1
  candidate="$resolved_dir/$(basename "$candidate")"
  [[ -f "$candidate" && ! -L "$candidate" && -x "$candidate" ]] || return 1
  "$candidate" --version >/dev/null 2>&1 || return 1
  printf '%s\n' "$candidate"
}

install_cloudflared() {
  local target_binary="$1"
  local source_binary="$CLOUDFLARED_SOURCE_BINARY"
  local discovered_binary resolved_binary
  local machine arch download_url tmp_file staged_target

  if [[ -z "$source_binary" && -f "$target_binary" && ! -L "$target_binary" && \
        -x "$target_binary" ]] && "$target_binary" --version >/dev/null 2>&1; then
    return
  fi

  if [[ -n "$source_binary" ]]; then
    resolved_binary="$(resolve_cloudflared_binary "$source_binary")" || \
      die "AGENTDOCK_CLOUDFLARED_BINARY 指向的 cloudflared 无效：$source_binary"
    source_binary="$resolved_binary"
  else
    if [[ -L "$target_binary" ]] && \
       resolved_binary="$(resolve_cloudflared_binary "$target_binary")"; then
      source_binary="$resolved_binary"
      log "复用现有 cloudflared：$target_binary"
    fi
    if [[ -z "$source_binary" ]]; then
      discovered_binary="$(command -v cloudflared 2>/dev/null || true)"
      if [[ -n "$discovered_binary" ]]; then
        if resolved_binary="$(resolve_cloudflared_binary "$discovered_binary")"; then
          source_binary="$resolved_binary"
          log "复用系统 cloudflared：$discovered_binary"
        else
          log "警告：忽略 PATH 中无效的 cloudflared：$discovered_binary"
        fi
      fi
    fi
  fi

  if [[ -z "$source_binary" ]]; then
    machine="$(uname -m)"
    case "$machine" in
      x86_64|amd64) arch="amd64" ;;
      aarch64|arm64) arch="arm64" ;;
      *) die "暂不支持自动安装 cloudflared 的架构：$machine" ;;
    esac
    download_url="${CLOUDFLARED_RELEASE_BASE_URL%/}/cloudflared-linux-$arch"
    tmp_file="$(mktemp)"
    log "下载 Cloudflare cloudflared：$download_url"
    curl -fL --retry 3 --retry-delay 1 "$download_url" -o "$tmp_file"
    chmod 0755 "$tmp_file"
    if ! resolved_binary="$(resolve_cloudflared_binary "$tmp_file")"; then
      rm -f "$tmp_file"
      die "下载的 cloudflared 无效：$download_url"
    fi
    source_binary="$resolved_binary"
  fi

  if [[ "$source_binary" != "$target_binary" ]]; then
    run_root mkdir -p "$(dirname "$target_binary")"
    staged_target="$target_binary.tmp.$$"
    run_root rm -f "$staged_target"
    if ! run_root install -m 0755 -o root -g root "$source_binary" "$staged_target"; then
      run_root rm -f "$staged_target"
      [[ -z "${tmp_file:-}" ]] || rm -f "$tmp_file"
      die "安装 cloudflared 失败：$target_binary"
    fi
    if ! run_root mv -f "$staged_target" "$target_binary"; then
      run_root rm -f "$staged_target"
      [[ -z "${tmp_file:-}" ]] || rm -f "$tmp_file"
      die "替换 cloudflared 失败：$target_binary"
    fi
  fi
  [[ -z "${tmp_file:-}" ]] || rm -f "$tmp_file"
  run_root "$target_binary" --version >/dev/null
}

write_cloudflared_env() {
  local env_file="$1"
  local mode="$2"
  local target_url="$3"
  local token="$4"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<ENV
# 仅供 cloudflared 服务使用；AgentDock 服务不会读取此文件。
AGENTDOCK_TUNNEL_MODE=$mode
AGENTDOCK_TUNNEL_TARGET=$target_url
TUNNEL_TOKEN=$token
ENV
  run_root mkdir -p "$(dirname "$env_file")"
  run_root install -m 0600 -o root -g root "$tmp_file" "$env_file"
  rm -f "$tmp_file"
}

write_cloudflared_systemd_unit() {
  local tunnel_service_name="$1"
  local service_user="$2"
  local service_group="$3"
  local data_dir="$4"
  local cloudflared_binary="$5"
  local cloudflared_env_file="$6"
  local mode="$7"
  local target_url="$8"
  local agentdock_binary="$9"
  local runtime_root="${10}"
  local unit_file="/etc/systemd/system/${tunnel_service_name}.service"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<UNIT
[Unit]
Description=AgentDock Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$service_user
Group=$service_group
WorkingDirectory=$data_dir
EnvironmentFile=$cloudflared_env_file
ExecStart=$agentdock_binary tunnel launch --runtime-root $runtime_root
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
  run_root install -m 0644 -o root -g root "$tmp_file" "$unit_file"
  rm -f "$tmp_file"
}

write_cloudflared_openrc_service() {
  local tunnel_service_name="$1"
  local service_user="$2"
  local service_group="$3"
  local data_dir="$4"
  local cloudflared_binary="$5"
  local cloudflared_env_file="$6"
  local mode="$7"
  local target_url="$8"
  local agentdock_binary="$9"
  local runtime_root="${10}"
  local init_file="/etc/init.d/${tunnel_service_name}"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<OPENRC
#!/sbin/openrc-run
name="AgentDock Cloudflare Tunnel"
description="AgentDock Cloudflare Tunnel"
command="$agentdock_binary"
command_args="tunnel launch --runtime-root $runtime_root"
command_user="$service_user:$service_group"
directory="$data_dir"
pidfile="/run/${tunnel_service_name}.pid"
command_background="yes"
log_dir="/var/log/${tunnel_service_name}"
output_log="/dev/null"
error_log="/dev/null"
cloudflared_env_file="$cloudflared_env_file"

start_pre() {
  checkpath -d -m 0750 -o "$service_user:$service_group" "\$log_dir"
  if [ -r "\$cloudflared_env_file" ]; then
    set -a
    . "\$cloudflared_env_file"
    set +a
  else
    eerror "env file not readable: \$cloudflared_env_file"
    return 1
  fi
}

depend() {
  need net
  after firewall
}
OPENRC
  run_root install -m 0755 -o root -g root "$tmp_file" "$init_file"
  rm -f "$tmp_file"
}

cloudflared_service_active() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  case "$service_manager" in
    systemd) run_root systemctl is-active --quiet "$tunnel_service_name" ;;
    openrc) run_root rc-service "$tunnel_service_name" status >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

cloudflared_quick_url() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  local started_at="$3"
  local output
  case "$service_manager" in
    systemd) output="$(run_root journalctl -u "$tunnel_service_name" --since "$started_at" --no-pager 2>/dev/null || true)" ;;
    openrc) output="$(run_root tail -n 200 "/var/log/${tunnel_service_name}/cloudflared.out.log" "/var/log/${tunnel_service_name}/cloudflared.err.log" 2>/dev/null || true)" ;;
    *) return 1 ;;
  esac
  # provisioning 失败日志也会出现 trycloudflare.com API 地址，必须先确认 cloudflared 已报告创建成功。
  printf '%s\n' "$output" | awk '
    /Your quick Tunnel has been created! Visit it at/ { created = 1 }
    created && match($0, /https:\/\/[[:alnum:]-]+\.trycloudflare\.com/) {
      print substr($0, RSTART, RLENGTH)
      exit
    }
  '
}

wait_for_cloudflared() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  local mode="$3"
  local started_at="$4"
  local attempts=60
  local stable_checks=0

  while (( attempts-- > 0 )); do
    if cloudflared_service_active "$service_manager" "$tunnel_service_name"; then
      if [[ "$mode" == quick ]]; then
        local public_url
        public_url="$(cloudflared_quick_url "$service_manager" "$tunnel_service_name" "$started_at" || true)"
        if [[ -n "$public_url" ]]; then
          printf '%s' "$public_url"
          return 0
        fi
      else
        (( stable_checks += 1 ))
        if (( stable_checks >= 10 )); then
          printf 'active'
          return 0
        fi
      fi
    else
      stable_checks=0
    fi
    sleep 0.5
  done
  return 1
}

start_cloudflared_service() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  local mode="$3"
  local server_url="$4"
  local started_at
  started_at="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"

  case "$service_manager" in
    systemd)
      run_root systemctl daemon-reload
      run_root systemctl enable --now "$tunnel_service_name"
      run_root systemctl restart "$tunnel_service_name"
      ;;
    openrc)
      # 兼容升级：删除旧版 OpenRC 直接追加的平铺日志，再由 AgentDock 创建受限轮转目录。
      run_root rm -f "/var/log/${tunnel_service_name}.log" "/var/log/${tunnel_service_name}.err"
      run_root rm -rf "/var/log/${tunnel_service_name}"
      run_root rc-update add "$tunnel_service_name" default
      run_root rc-service "$tunnel_service_name" restart
      ;;
    *) die "Cloudflare Tunnel 需要 systemd 或 OpenRC" ;;
  esac

  local result
  if ! result="$(wait_for_cloudflared "$service_manager" "$tunnel_service_name" "$mode" "$started_at")"; then
    die "AgentDock 已安装，但 Cloudflare Tunnel 启动失败，请检查服务日志"
  fi
  if [[ "$mode" == quick ]]; then
    TUNNEL_PUBLIC_URL="$result"
    log "临时公网地址已连接：$result/mcp"
  else
    log "Named Tunnel 已启动：$server_url/mcp"
  fi
}

remove_cloudflared_service() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  local cloudflared_env_file="$3"
  case "$service_manager" in
    systemd)
      if [[ -f "/etc/systemd/system/${tunnel_service_name}.service" ]]; then
        run_root systemctl disable --now "$tunnel_service_name" >/dev/null 2>&1 || true
        run_root rm -f "/etc/systemd/system/${tunnel_service_name}.service"
        run_root systemctl daemon-reload
      fi
      ;;
    openrc)
      if [[ -f "/etc/init.d/${tunnel_service_name}" ]]; then
        run_root rc-service "$tunnel_service_name" stop >/dev/null 2>&1 || true
        run_root rc-update del "$tunnel_service_name" default >/dev/null 2>&1 || true
        run_root rm -f "/etc/init.d/${tunnel_service_name}"
      fi
      run_root rm -rf "/var/log/${tunnel_service_name}"
      ;;
  esac
  run_root rm -f "$cloudflared_env_file"
}

configure_cloudflared() {
  local service_manager="$1"
  local tunnel_service_name="$2"
  local service_user="$3"
  local service_group="$4"
  local data_dir="$5"
  local cloudflared_binary="$6"
  local cloudflared_env_file="$7"
  local mode="$8"
  local target_url="$9"
  local token="${10}"
  local server_url="${11}"
  local agentdock_binary="${12}"
  local runtime_root="${13}"

  install_cloudflared "$cloudflared_binary"
  write_cloudflared_env "$cloudflared_env_file" "$mode" "$target_url" "$token"
  run_root chown "$service_user:$service_group" "$cloudflared_env_file"
  case "$service_manager" in
    systemd)
      write_cloudflared_systemd_unit "$tunnel_service_name" "$service_user" "$service_group" \
        "$data_dir" "$cloudflared_binary" "$cloudflared_env_file" "$mode" "$target_url" \
        "$agentdock_binary" "$runtime_root"
      ;;
    openrc)
      write_cloudflared_openrc_service "$tunnel_service_name" "$service_user" "$service_group" \
        "$data_dir" "$cloudflared_binary" "$cloudflared_env_file" "$mode" "$target_url" \
        "$agentdock_binary" "$runtime_root"
      ;;
    *) die "Cloudflare Tunnel 需要 systemd 或 OpenRC" ;;
  esac
  start_cloudflared_service "$service_manager" "$tunnel_service_name" "$mode" "$server_url"
}

start_service() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd)
      log "启动 systemd 服务：$service_name"
      run_root systemctl daemon-reload
      run_root systemctl enable --now "$service_name"
      run_root systemctl restart "$service_name"
      sleep 2
      run_root systemctl --no-pager --full status "$service_name" || true
      run_root systemctl is-active --quiet "$service_name"
      ;;
    openrc)
      log "启动 OpenRC 服务：$service_name"
      # 升级旧版本时清理曾由 OpenRC 直接追加的平铺日志，后续改由 AgentDock 自己轮转。
      run_root rm -f "/var/log/${service_name}.log" "/var/log/${service_name}.err"
      run_root rc-update add "$service_name" default
      run_root rc-service "$service_name" restart
      sleep 2
      run_root rc-service "$service_name" status
      ;;
    none)
      warn "未配置系统服务；仅完成构建和 env 写入。可手动运行：source 环境变量后执行 bin/agentdock。"
      ;;
    *) die "未知服务管理器：$service_manager" ;;
  esac
}

service_status_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo systemctl status %s --no-pager' "$service_name" ;;
    openrc) printf 'sudo rc-service %s status' "$service_name" ;;
    none) printf '# 未安装系统服务' ;;
  esac
}

service_log_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo journalctl -u %s -n 100 --no-pager' "$service_name" ;;
    openrc) printf 'sudo tail -n 100 /var/log/%s/agentdock.err.log' "$service_name" ;;
    none) printf '# 未安装系统服务' ;;
  esac
}

service_restart_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo systemctl restart %s' "$service_name" ;;
    openrc) printf 'sudo rc-service %s restart' "$service_name" ;;
    none) printf '# 未安装系统服务' ;;
  esac
}

cloudflared_status_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo systemctl status %s --no-pager' "$service_name" ;;
    openrc) printf 'sudo rc-service %s status' "$service_name" ;;
    none) printf '# 未安装 Tunnel 服务' ;;
  esac
}

cloudflared_log_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo journalctl -u %s -n 100 --no-pager' "$service_name" ;;
    openrc) printf 'sudo tail -n 100 /var/log/%s/cloudflared.out.log /var/log/%s/cloudflared.err.log' "$service_name" "$service_name" ;;
    none) printf '# 未安装 Tunnel 服务' ;;
  esac
}

cloudflared_restart_command() {
  local service_manager="$1"
  local service_name="$2"
  case "$service_manager" in
    systemd) printf 'sudo systemctl restart %s' "$service_name" ;;
    openrc) printf 'sudo rc-service %s restart' "$service_name" ;;
    none) printf '# 未安装 Tunnel 服务' ;;
  esac
}

local_health_host() {
  local host="$1"
  case "$host" in
    0.0.0.0|::) printf '127.0.0.1' ;;
    *:*) printf '[%s]' "$host" ;;
    *) printf '%s' "$host" ;;
  esac
}

repo_root_from_script() {
  local script_dir root
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
  root="$(cd "$script_dir/../.." >/dev/null 2>&1 && pwd)"
  if [[ -d "$root/cmd/agentdock" && -f "$root/go.mod" ]]; then
    printf '%s' "$root"
  fi
}

main() {
  require_linux

  local detected_root source_default repo_url branch source_dir data_dir env_file
  local service_name service_user service_group service_manager service_manager_prompt host port token log_level
  local install_mode release_version update_existing run_full_check install_deps
  local oauth_password oauth_token_secret oauth_enabled configure_oauth
  local go_version public_domain smoke_url health_host build_from_source
  local tunnel_mode tunnel_default tunnel_token server_url existing_server_url existing_tunnel_token
  local existing_host existing_port existing_log_level existing_token existing_oauth_password existing_oauth_secret
  local cloudflared_binary cloudflared_env_file tunnel_service_name tunnel_target_url

  detected_root="$(repo_root_from_script || true)"
  if [[ -n "$detected_root" ]]; then
    source_default="${AGENTDOCK_SOURCE_DIR:-$detected_root}"
  else
    source_default="$DEFAULT_SOURCE_DIR"
  fi

  cat >"$TTY_OUT" <<'INTRO'

AgentDock Linux 一键部署将执行：
1. 默认下载预编译二进制，避免安装 Go/gcc 编译链。
2. 仅在选择 source 或 binary 下载失败且选择 fallback 时源码构建。
3. 生成 /etc/agentdock/agentdock.env 和 systemd/OpenRC 服务配置。AgentDock 固定使用运行用户 home 下的 .agentdock 与 AgentDock。
4. 启动 systemd/OpenRC 服务并验证 healthz。

生产建议：监听 127.0.0.1，通过 Caddy/Nginx 做 HTTPS 反代；不要把 AgentDock 直接裸露到公网。

INTRO

  install_mode="$(prompt '安装方式（普通用户选 binary；source/auto 仅开发调试）' "$DEFAULT_INSTALL_MODE")"
  validate_install_mode "$install_mode"
  release_version="$(prompt 'Release 版本：latest 或 vX.Y.Z' "$DEFAULT_RELEASE_VERSION")"
  if [[ "$install_mode" == "binary" ]]; then
    repo_url="$DEFAULT_REPO_URL"
    branch="$DEFAULT_BRANCH"
  else
    repo_url="$(prompt 'Git 仓库 URL' "$DEFAULT_REPO_URL")"
    branch="$(prompt 'Git 分支' "$DEFAULT_BRANCH")"
  fi
  source_dir="$(prompt '安装目录' "$source_default")"
  data_dir="$(prompt '运行数据根目录' "$DEFAULT_DATA_DIR")"
  env_file="$(prompt '环境变量文件' "$DEFAULT_ENV_FILE")"
  service_manager_prompt="$(prompt '服务管理器：auto/systemd/openrc/none' "$DEFAULT_SERVICE_MANAGER")"
  service_manager="$(detect_service_manager "$service_manager_prompt")"
  if [[ "$service_manager" == "none" ]]; then
    warn "未检测到 systemd 或 OpenRC；脚本仍可构建和写入 env，但不会安装系统服务。"
  else
    log "服务管理器：$service_manager"
  fi
  service_name="$(prompt '服务名' "$DEFAULT_SERVICE_NAME")"
  service_user="$(prompt '运行用户' "$DEFAULT_SERVICE_USER")"
  existing_host="$(read_env_assignment "$env_file" AGENTDOCK_HOST)"
  existing_port="$(read_env_assignment "$env_file" AGENTDOCK_PORT)"
  existing_log_level="$(read_env_assignment "$env_file" AGENTDOCK_LOG_LEVEL)"
  host="$(prompt '监听地址' "${existing_host:-$DEFAULT_HOST}")"
  port="$(prompt '监听端口' "${existing_port:-$DEFAULT_PORT}")"
  log_level="$(prompt '日志级别' "${existing_log_level:-$DEFAULT_LOG_LEVEL}")"
  validate_abs_path '安装目录' "$source_dir"
  validate_abs_path '运行数据根目录' "$data_dir"
  validate_abs_path '环境变量文件' "$env_file"
  validate_no_space '服务名' "$service_name"
  validate_no_space '运行用户' "$service_user"
  validate_host "$host"
  validate_port "$port"

  cloudflared_binary="$DEFAULT_CLOUDFLARED_BINARY"
  validate_abs_path 'cloudflared 安装路径' "$cloudflared_binary"
  cloudflared_env_file="$(dirname "$env_file")/cloudflared.env"
  tunnel_service_name="${service_name}-cloudflared"
  tunnel_default="$DEFAULT_TUNNEL_MODE"
  if [[ -z "$tunnel_default" ]]; then
    tunnel_default="$(read_env_assignment "$cloudflared_env_file" AGENTDOCK_TUNNEL_MODE)"
  fi
  if [[ -n "$tunnel_default" ]]; then
    tunnel_mode="$tunnel_default"
    log "沿用公网访问模式：$tunnel_mode"
  elif noninteractive_enabled; then
    # 非交互安装不能替用户决定是否暴露公网；需要 Tunnel 时显式设置环境变量。
    tunnel_mode="none"
  else
    tunnel_mode="$(choose_tunnel_mode)"
  fi
  validate_tunnel_mode "$tunnel_mode"
  if [[ "$tunnel_mode" != none && "$service_manager" == none ]]; then
    die "Quick/Named Tunnel 需要 systemd 或 OpenRC 服务管理器"
  fi

  server_url=""
  tunnel_token=""
  configure_oauth="no"
  oauth_enabled="false"
  case "$tunnel_mode" in
    quick)
      configure_oauth="yes"
      existing_server_url="$DEFAULT_SERVER_URL"
      if [[ -z "$existing_server_url" ]]; then
        existing_server_url="$(read_env_assignment "$env_file" AGENTDOCK_SERVER_URL)"
      fi
      server_url="$existing_server_url"
      if [[ -n "$server_url" ]]; then
        oauth_enabled="true"
      fi
      warn "临时地址在 Tunnel 重启后可能变化；重新运行同一安装脚本即可刷新。"
      ;;
    named)
      configure_oauth="yes"
      oauth_enabled="true"
      existing_server_url="$DEFAULT_SERVER_URL"
      if [[ -z "$existing_server_url" ]]; then
        existing_server_url="$(read_env_assignment "$env_file" AGENTDOCK_SERVER_URL)"
      fi
      server_url="$(prompt 'Named Tunnel HTTPS 公网 Origin' "$existing_server_url")"
      [[ -n "$server_url" ]] || die "Named Tunnel 必须配置 HTTPS 公网 Origin"
      server_url="$(normalize_server_url "$server_url")"

      tunnel_token="${AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN:-}"
      if [[ -z "$tunnel_token" ]]; then
        existing_tunnel_token="$(read_env_assignment "$cloudflared_env_file" TUNNEL_TOKEN)"
        tunnel_token="$existing_tunnel_token"
      fi
      if [[ -z "$tunnel_token" ]]; then
        tunnel_token="$(prompt_required_secret 'Cloudflare Tunnel Token')"
      fi
      [[ -n "$tunnel_token" ]] || die "Named Tunnel 必须提供 Cloudflare Tunnel Token"
      validate_cloudflare_token "$tunnel_token"
      ;;
    none)
      server_url="$DEFAULT_SERVER_URL"
      if [[ -z "$server_url" ]]; then
        server_url="$(read_env_assignment "$env_file" AGENTDOCK_SERVER_URL)"
      fi
      ;;
  esac

  if confirm '是否安装/更新系统基础依赖？binary 只装运行依赖，source 才装 Go/gcc' y; then install_deps="yes"; else install_deps="no"; fi
  update_existing="no"
  run_full_check="no"
  if [[ "$install_mode" != "binary" ]]; then
    if confirm '安装目录已存在时是否尝试 git pull --ff-only？' y; then update_existing="yes"; else update_existing="no"; fi
    if confirm '是否运行 go test ./... 和 go vet ./...？首次部署可跳过以加快安装' n; then run_full_check="yes"; else run_full_check="no"; fi
  fi
  existing_token="$(read_env_assignment "$env_file" AGENTDOCK_AUTH_TOKEN)"
  token="${existing_token:-${AGENTDOCK_AUTH_TOKEN:-}}"
  if [[ -z "$token" ]]; then
    token="$(generate_token)"
    log "已自动生成 Bearer Token。"
  fi
  validate_no_space 'Bearer Token' "$token"

  existing_oauth_password="$(read_env_assignment "$env_file" AGENTDOCK_OAUTH_PASSWORD)"
  oauth_password="${existing_oauth_password:-${AGENTDOCK_OAUTH_PASSWORD:-}}"
  if [[ "$configure_oauth" == "yes" && -z "$oauth_password" ]]; then
    oauth_password="$(generate_oauth_password)"
    log "已自动生成 OAuth 登录密码。"
  fi
  existing_oauth_secret="$(read_env_assignment "$env_file" AGENTDOCK_OAUTH_TOKEN_SECRET)"
  oauth_token_secret="${existing_oauth_secret:-${AGENTDOCK_OAUTH_TOKEN_SECRET:-}}"
  if [[ "$configure_oauth" == "yes" && -z "$oauth_token_secret" ]]; then
    oauth_token_secret="$(generate_token)"
  fi
  if [[ "$configure_oauth" == "yes" ]]; then
    validate_no_space 'OAuth 登录密码' "$oauth_password"
    validate_no_space 'OAuth 签名密钥' "$oauth_token_secret"
    (( ${#oauth_password} >= 12 )) || die "OAuth 登录密码至少需要 12 个字符"
    (( ${#oauth_token_secret} >= 32 )) || die "OAuth 签名密钥至少需要 32 个字节"
  fi

  public_domain=""
  if [[ "$tunnel_mode" == none ]]; then
    public_domain="$(prompt '公网域名，可留空；脚本只输出反代提示，不直接改 Caddy/Nginx' '')"
  fi

  cat >"$TTY_OUT" <<SUMMARY

即将部署：
- 安装目录：$source_dir
- 运行用户 home：$data_dir
- 内部状态目录：$data_dir/.agentdock
- 默认工作目录：$data_dir/AgentDock
- env 文件：$env_file
- 安装方式：$install_mode
- Release 版本：$release_version
- 服务管理器：$service_manager
- 服务名：$service_name
- 运行用户：$service_user
- 本机监听：http://$host:$port
- 公网访问：$tunnel_mode
- 认证方式：Bearer Token、OAuth（公网模式自动同时配置）
- 密钥：安装完成后在终端显示，并写入 root-only env 文件

SUMMARY
  confirm '确认开始执行部署？' y || die '用户取消。'

  build_from_source="no"

  if [[ "$install_deps" == "yes" ]]; then
    if [[ "$install_mode" == "source" ]]; then
      install_build_packages
    else
      install_runtime_packages
    fi
  fi

  if [[ "$install_mode" == "binary" || "$install_mode" == "auto" ]]; then
    if install_prebuilt_binary "$repo_url" "$release_version" "$source_dir"; then
      log "预编译二进制安装完成：$source_dir/bin/agentdock"
    elif [[ "$install_mode" == "auto" ]]; then
      warn "预编译二进制下载失败，将 fallback 到源码构建。"
      build_from_source="yes"
      if [[ "$install_deps" == "yes" ]]; then
        install_build_packages
      fi
    else
      die "预编译二进制下载失败。可改用安装方式 source，或设置 AGENTDOCK_RELEASE_VERSION 指定已存在的 release。"
    fi
  else
    build_from_source="yes"
  fi

  if [[ "$build_from_source" == "yes" ]]; then
    go_version="$(current_go_version || true)"
    if [[ -z "$go_version" ]] || ! semver_ge "$go_version" "$MIN_GO_VERSION"; then
      warn "当前 Go 版本不足：${go_version:-未安装}，需要 >= ${MIN_GO_VERSION}。"
      if confirm "是否安装官方 Go $DEFAULT_GO_VERSION 到 /usr/local/go？" y; then
        install_go_official "$DEFAULT_GO_VERSION"
      else
        die "Go 版本不足，无法构建 AgentDock。"
      fi
    else
      log "Go 版本满足要求：$go_version"
    fi

    clone_or_update_source "$repo_url" "$branch" "$source_dir" "$update_existing"

    log "构建 AgentDock"
    mkdir -p "$source_dir/bin"
    if [[ "$run_full_check" == "yes" ]]; then
      (cd "$source_dir" && go test ./... && go vet ./...)
    fi
    local build_commit build_date
    build_commit="$(cd "$source_dir" && git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)"
    build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    (cd "$source_dir" && go build -trimpath \
      -ldflags "-X github.com/uvwt/agentdock/internal/buildinfo.Commit=$build_commit -X github.com/uvwt/agentdock/internal/buildinfo.BuildDate=$build_date" \
      -o ./bin/agentdock ./cmd/agentdock)
    chmod +x "$source_dir/bin/agentdock"

    command -v python3 >/dev/null 2>&1 || die "源码安装需要 python3 构建核心 Skill Bundle"
    CORE_SKILL_TEMP_DIR="$(mktemp -d)"
    CORE_SKILL_BUNDLE="$CORE_SKILL_TEMP_DIR/core-skills"
    python3 "$source_dir/packaging/build-core-skill-bundle.py" \
      --repo-root "$source_dir" \
      --output "$CORE_SKILL_BUNDLE"
  fi

  # 服务用户必须能够穿过安装目录并执行二进制；mkdir 会受调用者 umask 影响，
  # 因此不能依赖目录碰巧是 0755。
  run_root chmod 0755 "$source_dir" "$source_dir/bin" "$source_dir/bin/agentdock"

  ensure_service_user "$service_user" "$data_dir"
  service_group="$(id -gn "$service_user")"
  run_root mkdir -p "$data_dir/.agentdock" "$data_dir/AgentDock"
  run_root chown -R "$service_user:$service_group" "$data_dir"
  write_env_file "$env_file" "$host" "$port" "$token" "$log_level" \
    "$server_url" "$configure_oauth" "$oauth_enabled" "$oauth_password" "$oauth_token_secret"
  # 原生运行时需要在服务用户身份下读取并原子更新公网地址；目录仅允许该服务用户访问。
  run_root chown "$service_user:$service_group" "$(dirname "$env_file")" "$env_file"
  run_root chmod 0700 "$(dirname "$env_file")"
  write_runtime_manifest "$service_manager" "$service_name" "$tunnel_service_name" \
    "$source_dir" "$env_file" "$cloudflared_binary" "$cloudflared_env_file"
  case "$service_manager" in
    systemd) write_systemd_unit "$service_name" "$service_user" "$service_group" "$source_dir" "$env_file" ;;
    openrc) write_openrc_service "$service_name" "$service_user" "$service_group" "$source_dir" "$env_file" ;;
    none) warn "跳过系统服务写入。" ;;
  esac

  start_service "$service_manager" "$service_name"

  health_host="$(local_health_host "$host")"
  smoke_url="http://$health_host:$port"
  if [[ "$service_manager" != "none" ]]; then
    log "验证 healthz"
    curl -fsS "$smoke_url/healthz"
    printf '\n'

    if [[ -x "$source_dir/packaging/docker/smoke-docker.sh" ]]; then
      log "验证 MCP smoke"
      AGENTDOCK_SMOKE_URL="$smoke_url" AGENTDOCK_AUTH_TOKEN="$token" "$source_dir/packaging/docker/smoke-docker.sh"
    else
      warn "未找到 smoke 脚本，跳过 MCP smoke：$source_dir/packaging/docker/smoke-docker.sh"
    fi
  else
    log "未配置系统服务，跳过运行时健康检查。"
  fi

  [[ -n "$CORE_SKILL_BUNDLE" ]] || die "未准备核心 Skill Bundle"
  make_core_skill_bundle_readable
  log "安装官方核心 Skill"
  run_as_service_user "$service_user" "$data_dir" \
    "$source_dir/bin/agentdock" skill bootstrap --bundle "$CORE_SKILL_BUNDLE"

  if [[ "$tunnel_mode" == none ]]; then
    remove_cloudflared_service "$service_manager" "$tunnel_service_name" "$cloudflared_env_file"
  else
    tunnel_target_url="http://$health_host:$port"
    configure_cloudflared "$service_manager" "$tunnel_service_name" "$service_user" "$service_group" \
      "$data_dir" "$cloudflared_binary" "$cloudflared_env_file" "$tunnel_mode" \
      "$tunnel_target_url" "$tunnel_token" "$server_url" "$source_dir/bin/agentdock" "$(dirname "$env_file")"
    if [[ "$tunnel_mode" == quick ]]; then
      server_url="$TUNNEL_PUBLIC_URL"
      oauth_enabled="true"
      write_env_file "$env_file" "$host" "$port" "$token" "$log_level" \
        "$server_url" yes true "$oauth_password" "$oauth_token_secret"
      run_root chown "$service_user:$service_group" "$env_file"
      log "已将临时公网地址写入 AgentDock OAuth 配置并重启服务"
      start_service "$service_manager" "$service_name"
      curl -fsS "$smoke_url/healthz" >/dev/null
    fi
  fi

  cat >"$TTY_OUT" <<DONE

AgentDock Linux 部署完成。

本机入口：
  $smoke_url/mcp
  $smoke_url/healthz

服务操作：
  $(service_status_command "$service_manager" "$service_name")
  $(service_log_command "$service_manager" "$service_name")
  $(service_restart_command "$service_manager" "$service_name")

认证配置：
  Bearer Token：$token
  OAuth 登录密码：${oauth_password:-未配置}
  OAuth 签名密钥：已安全保存，不显示
  配置文件：$env_file

DONE

  if [[ "$tunnel_mode" != none ]]; then
    cat >"$TTY_OUT" <<TUNNEL_DONE
╭─ AgentDock 公网安装完成 ─────────────────────
│ 公网模式：$tunnel_mode
│ 公网地址：$server_url
│ MCP 地址：${server_url%/}/mcp
│ Bearer Token：$token
│ OAuth 登录密码：$oauth_password
│ 认证方式：Bearer Token、OAuth 均已启用
╰──────────────────────────────────────────────

Cloudflare Tunnel：
  模式：$tunnel_mode
  状态：$(cloudflared_status_command "$service_manager" "$tunnel_service_name")
  日志：$(cloudflared_log_command "$service_manager" "$tunnel_service_name")
  重启：$(cloudflared_restart_command "$service_manager" "$tunnel_service_name")
  独立配置：$cloudflared_env_file
TUNNEL_DONE
    if [[ "$tunnel_mode" == named ]]; then
      cat >"$TTY_OUT" <<NAMED_DONE
  公网 MCP URL：$server_url/mcp
  Cloudflare Public Hostname 的 Service 目标应为：$tunnel_target_url

NAMED_DONE
    else
      cat >"$TTY_OUT" <<QUICK_DONE
  临时地址在 Tunnel 重启后可能变化。
  地址变化后，重新运行同一安装脚本即可获取新地址；Bearer Token、OAuth 密码和签名密钥保持不变。
  然后在客户端替换 MCP URL，并重新完成 OAuth 授权。

QUICK_DONE
    fi
  fi

  if [[ -n "$public_domain" ]]; then
    cat >"$TTY_OUT" <<PROXY
反代参考：

Caddyfile：
$public_domain {
  reverse_proxy 127.0.0.1:$port
}

客户端 MCP URL：
  https://$public_domain/mcp

PROXY
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
