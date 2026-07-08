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

TTY_IN="/dev/tty"
TTY_OUT="/dev/tty"
if [[ ! -r "$TTY_IN" ]]; then
  TTY_IN="/dev/stdin"
fi
if [[ ! -w "$TTY_OUT" ]]; then
  TTY_OUT="/dev/stderr"
fi

usage() {
  cat <<'USAGE'
AgentDock Linux 问答式一键部署脚本。

用法：
  bash scripts/install-linux.sh
  curl -fsSL https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux.sh -o /tmp/agentdock-install.sh
  bash /tmp/agentdock-install.sh

Alpine/极简系统如果没有 curl/bash：
  apk add --no-cache bash curl
  curl -fsSL https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux.sh -o /tmp/agentdock-install.sh
  bash /tmp/agentdock-install.sh

环境变量可覆盖默认值：
  AGENTDOCK_INSTALL_MODE、AGENTDOCK_RELEASE_VERSION、AGENTDOCK_REPO_URL、AGENTDOCK_BRANCH
  AGENTDOCK_SOURCE_DIR、AGENTDOCK_DATA_DIR、AGENTDOCK_ENV_FILE
  AGENTDOCK_SERVICE_NAME、AGENTDOCK_SERVICE_USER、AGENTDOCK_HOST、AGENTDOCK_PORT
  AGENTDOCK_AUTH_TOKEN、AGENTDOCK_GO_VERSION

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
  printf '%s（输入不回显，留空自动生成）: ' "$label" >"$TTY_OUT"
  stty -echo <"$TTY_IN" 2>/dev/null || true
  IFS= read -r answer <"$TTY_IN" || true
  stty echo <"$TTY_IN" 2>/dev/null || true
  printf '\n' >"$TTY_OUT"
  printf '%s' "$answer"
}

confirm() {
  local label="$1"
  local default_value="${2:-y}"
  local answer=""
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
  [[ "$(uname -s)" == "Linux" ]] || die "此脚本只支持 Linux；macOS 请使用 scripts/install-macos.sh。"
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
  repo_slug="$(release_repo_slug "$repo_url")"
  file_name="agentdock_linux_${arch}.tar.gz"
  if [[ "$version" == "latest" ]]; then
    base="https://github.com/${repo_slug}/releases/latest/download"
  else
    base="https://github.com/${repo_slug}/releases/download/${version}"
  fi
  printf '%s/%s' "$base" "$file_name"
}

install_prebuilt_binary() {
  local repo_url="$1"
  local version="$2"
  local source_dir="$3"
  local url tmp_dir tmp_tgz
  url="$(release_download_url "$repo_url" "$version")"
  tmp_dir="$(mktemp -d)"
  tmp_tgz="$tmp_dir/agentdock.tgz"
  log "下载预编译 AgentDock：$url"
  if ! curl -fL "$url" -o "$tmp_tgz"; then
    rm -rf "$tmp_dir"
    return 1
  fi
  run_root mkdir -p "$source_dir/bin"
  tar -xzf "$tmp_tgz" -C "$tmp_dir"
  if [[ -x "$tmp_dir/bin/agentdock" ]]; then
    run_root install -m 755 "$tmp_dir/bin/agentdock" "$source_dir/bin/agentdock"
  elif [[ -x "$tmp_dir/agentdock" ]]; then
    run_root install -m 755 "$tmp_dir/agentdock" "$source_dir/bin/agentdock"
  else
    rm -rf "$tmp_dir"
    die "预编译包内未找到 agentdock 可执行文件：$url"
  fi
  rm -rf "$tmp_dir"
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
  local skip_prompts="$6"
  local recall_endpoint="$7"
  local recall_token="$8"
  local nexus_endpoint="$9"
  local nexus_device_name="${10}"

  local env_dir tmp_file
  env_dir="$(dirname "$env_file")"
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<ENV
AGENTDOCK_HOST=$host
AGENTDOCK_PORT=$port
AGENTDOCK_AUTH_TOKEN=$token
AGENTDOCK_LOG_LEVEL=$log_level
AGENTDOCK_SKIP_PERMISSION_PROMPTS=$skip_prompts
AGENTDOCK_ENABLE_VIEW_IMAGE=true
ENV
  if [[ -n "$recall_endpoint" ]]; then
    printf 'AGENTDOCK_RECALL_ENDPOINT=%s\n' "$recall_endpoint" >>"$tmp_file"
  fi
  if [[ -n "$recall_token" ]]; then
    printf 'AGENTDOCK_RECALL_TOKEN=%s\n' "$recall_token" >>"$tmp_file"
  fi
  if [[ -n "$nexus_endpoint" ]]; then
    printf 'AGENTDOCK_NEXUS_ENDPOINT=%s\n' "$nexus_endpoint" >>"$tmp_file"
  fi
  if [[ -n "$nexus_device_name" ]]; then
    printf 'AGENTDOCK_NEXUS_DEVICE_NAME=%s\n' "$nexus_device_name" >>"$tmp_file"
  fi

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
ExecStart=$source_dir/bin/agentdock \\
  --host \${AGENTDOCK_HOST} \\
  --port \${AGENTDOCK_PORT} \\
  --log-level \${AGENTDOCK_LOG_LEVEL}
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
  local init_file="/etc/init.d/${service_name}"
  local tmp_file
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<OPENRC
#!/sbin/openrc-run
name="AgentDock MCP server"
description="AgentDock MCP server"
command="$source_dir/bin/agentdock"
command_args=""
command_user="$service_user:$service_group"
directory="$source_dir"
pidfile="/run/${service_name}.pid"
command_background="yes"
output_log="/var/log/${service_name}.log"
error_log="/var/log/${service_name}.err"

agentdock_env_file="$env_file"

start_pre() {
  checkpath -f -m 0644 -o "$service_user:$service_group" "\$output_log"
  checkpath -f -m 0644 -o "$service_user:$service_group" "\$error_log"
  if [ -r "\$agentdock_env_file" ]; then
    set -a
    . "\$agentdock_env_file"
    set +a
    command_args="--host \${AGENTDOCK_HOST} --port \${AGENTDOCK_PORT} --log-level \${AGENTDOCK_LOG_LEVEL}"
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
    openrc) printf 'sudo tail -n 100 /var/log/%s.log /var/log/%s.err' "$service_name" "$service_name" ;;
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

local_health_host() {
  local host="$1"
  case "$host" in
    0.0.0.0|::) printf '127.0.0.1' ;;
    *) printf '%s' "$host" ;;
  esac
}

repo_root_from_script() {
  local script_dir root
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
  root="$(cd "$script_dir/.." >/dev/null 2>&1 && pwd)"
  if [[ -d "$root/cmd/agentdock" && -f "$root/go.mod" ]]; then
    printf '%s' "$root"
  fi
}

main() {
  require_linux

  local detected_root source_default repo_url branch source_dir data_dir env_file
  local service_name service_user service_group service_manager service_manager_prompt host port token log_level skip_prompts
  local install_mode release_version recall_endpoint recall_token nexus_endpoint nexus_device_name update_existing run_full_check install_deps
  local go_version public_domain smoke_url health_host build_from_source binary_installed

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

  repo_url="$(prompt 'Git 仓库 URL' "$DEFAULT_REPO_URL")"
  branch="$(prompt 'Git 分支' "$DEFAULT_BRANCH")"
  source_dir="$(prompt '安装目录' "$source_default")"
  data_dir="$(prompt '运行数据根目录' "$DEFAULT_DATA_DIR")"
  env_file="$(prompt '环境变量文件' "$DEFAULT_ENV_FILE")"
  install_mode="$(prompt '安装方式：binary/source/auto' "$DEFAULT_INSTALL_MODE")"
  validate_install_mode "$install_mode"
  release_version="$(prompt 'Release 版本：latest 或 vX.Y.Z' "$DEFAULT_RELEASE_VERSION")"
  service_manager_prompt="$(prompt '服务管理器：auto/systemd/openrc/none' "$DEFAULT_SERVICE_MANAGER")"
  service_manager="$(detect_service_manager "$service_manager_prompt")"
  if [[ "$service_manager" == "none" ]]; then
    warn "未检测到 systemd 或 OpenRC；脚本仍可构建和写入 env，但不会安装系统服务。"
  else
    log "服务管理器：$service_manager"
  fi
  service_name="$(prompt '服务名' "$DEFAULT_SERVICE_NAME")"
  service_user="$(prompt '运行用户' "$DEFAULT_SERVICE_USER")"
  host="$(prompt '监听地址' "$DEFAULT_HOST")"
  port="$(prompt '监听端口' "$DEFAULT_PORT")"
  log_level="$(prompt '日志级别' "$DEFAULT_LOG_LEVEL")"
  validate_abs_path '安装目录' "$source_dir"
  validate_abs_path '运行数据根目录' "$data_dir"
  validate_abs_path '环境变量文件' "$env_file"
  validate_no_space '服务名' "$service_name"
  validate_no_space '运行用户' "$service_user"
  validate_no_space '监听地址' "$host"
  validate_port "$port"

  if confirm '是否安装/更新系统基础依赖？binary 只装运行依赖，source 才装 Go/gcc' y; then install_deps="yes"; else install_deps="no"; fi
  update_existing="no"
  run_full_check="no"
  if [[ "$install_mode" != "binary" ]]; then
    if confirm '安装目录已存在时是否尝试 git pull --ff-only？' y; then update_existing="yes"; else update_existing="no"; fi
    if confirm '是否运行 go test ./... 和 go vet ./...？首次部署可跳过以加快安装' n; then run_full_check="yes"; else run_full_check="no"; fi
  fi
  if confirm '是否跳过工具权限确认？仅 localhost/demo 建议开启' n; then skip_prompts="true"; else skip_prompts="false"; fi

  token="${AGENTDOCK_AUTH_TOKEN:-}"
  if [[ -z "$token" ]]; then
    token="$(prompt_secret 'Bearer token')"
  fi
  if [[ -z "$token" ]]; then
    token="$(generate_token)"
    log "已自动生成 bearer token，并将写入 $env_file。"
  fi
  validate_no_space 'Bearer token' "$token"

  recall_endpoint=""
  recall_token=""
  if confirm '是否配置 RecallDock endpoint？' n; then
    recall_endpoint="$(prompt 'RecallDock endpoint，例如 http://127.0.0.1:18777' '')"
    if [[ -n "$recall_endpoint" ]] && confirm 'RecallDock 是否需要 token？' n; then
      recall_token="$(prompt_secret 'RecallDock token')"
    fi
  fi

  nexus_endpoint=""
  nexus_device_name=""
  if confirm '是否配置 AgentDock Nexus endpoint？' n; then
    nexus_endpoint="$(prompt 'Nexus endpoint，例如 https://nexus.example.com' '')"
    nexus_device_name="$(prompt 'Nexus 设备名' "$(hostname)-linux")"
  fi

  public_domain="$(prompt '公网域名，可留空；脚本只输出反代提示，不直接改 Caddy/Nginx' '')"

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
- token：已隐藏，将写入 root-only env 文件

SUMMARY
  confirm '确认开始执行部署？' y || die '用户取消。'

  build_from_source="no"
  binary_installed="no"

  if [[ "$install_deps" == "yes" ]]; then
    if [[ "$install_mode" == "source" ]]; then
      install_build_packages
    else
      install_runtime_packages
    fi
  fi

  if [[ "$install_mode" == "binary" || "$install_mode" == "auto" ]]; then
    if install_prebuilt_binary "$repo_url" "$release_version" "$source_dir"; then
      binary_installed="yes"
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
      warn "当前 Go 版本不足：${go_version:-未安装}，需要 >= $MIN_GO_VERSION。"
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
    (cd "$source_dir" && go build -trimpath -o ./bin/agentdock ./cmd/agentdock)
    chmod +x "$source_dir/bin/agentdock"
  fi

  ensure_service_user "$service_user" "$data_dir"
  service_group="$(id -gn "$service_user")"
  run_root mkdir -p "$data_dir/.agentdock" "$data_dir/AgentDock"
  run_root chown -R "$service_user:$service_group" "$data_dir"
  write_env_file "$env_file" "$host" "$port" "$token" "$log_level" "$skip_prompts" "$recall_endpoint" "$recall_token" "$nexus_endpoint" "$nexus_device_name"
  case "$service_manager" in
    systemd) write_systemd_unit "$service_name" "$service_user" "$service_group" "$source_dir" "$env_file" ;;
    openrc) write_openrc_service "$service_name" "$service_user" "$service_group" "$source_dir" "$env_file" ;;
    none) warn "跳过系统服务写入。" ;;
  esac

  start_service "$service_manager" "$service_name"

  health_host="$(local_health_host "$host")"
  log "验证 healthz"
  curl -fsS "http://$health_host:$port/healthz"
  printf '\n'

  smoke_url="http://$health_host:$port"
  if [[ -x "$source_dir/scripts/smoke-docker.sh" ]]; then
    log "验证 MCP smoke"
    AGENTDOCK_SMOKE_URL="$smoke_url" AGENTDOCK_AUTH_TOKEN="$token" "$source_dir/scripts/smoke-docker.sh"
  else
    warn "未找到 smoke 脚本，跳过 MCP smoke：$source_dir/scripts/smoke-docker.sh"
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

Bearer token 已写入：
  $env_file
需要复制给客户端时，在服务器上执行：
  sudo awk -F= '/^AGENTDOCK_AUTH_TOKEN=/{print \$2}' $env_file

DONE

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

main "$@"
