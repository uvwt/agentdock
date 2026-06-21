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
DEFAULT_TOOL_PROFILE="${AGENTDOCK_TOOL_PROFILE:-unified}"
DEFAULT_MODE="${AGENTDOCK_MODE:-sandboxed}"
DEFAULT_PATH_POLICY="${AGENTDOCK_PATH_POLICY:-workspace}"
DEFAULT_SANDBOX_MODE="${AGENTDOCK_SANDBOX_MODE:-landlock}"

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

环境变量可覆盖默认值：
  AGENTDOCK_REPO_URL、AGENTDOCK_BRANCH、AGENTDOCK_SOURCE_DIR、AGENTDOCK_DATA_DIR
  AGENTDOCK_ENV_FILE、AGENTDOCK_SERVICE_NAME、AGENTDOCK_SERVICE_USER
  AGENTDOCK_HOST、AGENTDOCK_PORT、AGENTDOCK_AUTH_TOKEN、AGENTDOCK_GO_VERSION

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
  command -v systemctl >/dev/null 2>&1 || die "未找到 systemctl；当前脚本用于 systemd Linux。"
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

install_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    run_root apt-get update
    run_root env DEBIAN_FRONTEND=noninteractive apt-get install -y git curl ca-certificates make gcc g++ pkg-config python3 tar gzip
  elif command -v dnf >/dev/null 2>&1; then
    run_root dnf install -y git curl ca-certificates make gcc gcc-c++ pkgconfig python3 tar gzip
  elif command -v yum >/dev/null 2>&1; then
    run_root yum install -y git curl ca-certificates make gcc gcc-c++ pkgconfig python3 tar gzip
  elif command -v pacman >/dev/null 2>&1; then
    run_root pacman -Sy --needed --noconfirm git curl ca-certificates make gcc pkgconf python tar gzip
  elif command -v zypper >/dev/null 2>&1; then
    run_root zypper --non-interactive install git curl ca-certificates make gcc gcc-c++ pkg-config python3 tar gzip
  else
    die "未识别包管理器；请先安装 git、curl、ca-certificates、make、gcc、python3、tar、gzip。"
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
  log "创建 systemd 运行用户：$user"
  run_root useradd --system --home-dir "$home_dir" --create-home --shell /usr/sbin/nologin "$user"
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
    log "调整源码目录所有者，确保当前安装用户可构建：$source_dir"
    run_root chown -R "$installer_user:$installer_group" "$source_dir"
  fi

  if [[ -d "$source_dir/.git" ]]; then
    log "使用已有 Git 源码目录：$source_dir"
    if [[ "$update_existing" == "yes" ]]; then
      if git -C "$source_dir" diff --quiet && git -C "$source_dir" diff --cached --quiet; then
        git -C "$source_dir" fetch --tags origin "$branch"
        git -C "$source_dir" checkout "$branch"
        git -C "$source_dir" pull --ff-only origin "$branch"
      else
        warn "源码目录存在未提交改动，跳过 git pull：$source_dir"
      fi
    fi
    return
  fi

  if [[ -d "$source_dir/cmd/agentdock" ]]; then
    log "使用已有非 Git 源码目录：$source_dir"
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
  local workspace_dir="$2"
  local control_dir="$3"
  local host="$4"
  local port="$5"
  local token="$6"
  local tool_profile="$7"
  local log_level="$8"
  local mode="$9"
  local path_policy="${10}"
  local sandbox_mode="${11}"
  local skip_prompts="${12}"
  local recall_endpoint="${13}"
  local recall_token="${14}"
  local nexus_endpoint="${15}"
  local nexus_device_name="${16}"
  local nexus_state_dir="${17}"

  local env_dir tmp_file
  env_dir="$(dirname "$env_file")"
  tmp_file="$(mktemp)"
  cat >"$tmp_file" <<ENV
AGENTDOCK_WORKSPACE=$workspace_dir
AGENTDOCK_DIR=$control_dir
AGENTDOCK_HOST=$host
AGENTDOCK_PORT=$port
AGENTDOCK_TOOL_PROFILE=$tool_profile
AGENTDOCK_AUTH_TOKEN=$token
AGENTDOCK_LOG_LEVEL=$log_level
AGENTDOCK_MODE=$mode
AGENTDOCK_PATH_POLICY=$path_policy
AGENTDOCK_SANDBOX_MODE=$sandbox_mode
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
  if [[ -n "$nexus_state_dir" ]]; then
    printf 'AGENTDOCK_NEXUS_STATE_DIR=%s\n' "$nexus_state_dir" >>"$tmp_file"
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
  --workspace \${AGENTDOCK_WORKSPACE} \\
  --agentdock-dir \${AGENTDOCK_DIR} \\
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

  local detected_root source_default repo_url branch source_dir data_dir workspace_dir control_dir env_file
  local service_name service_user service_group host port token tool_profile log_level mode path_policy sandbox_mode skip_prompts
  local recall_endpoint recall_token nexus_endpoint nexus_device_name nexus_state_dir update_existing run_full_check install_deps install_go
  local go_version public_domain smoke_url health_host

  detected_root="$(repo_root_from_script || true)"
  if [[ -n "$detected_root" ]]; then
    source_default="${AGENTDOCK_SOURCE_DIR:-$detected_root}"
  else
    source_default="$DEFAULT_SOURCE_DIR"
  fi

  cat >"$TTY_OUT" <<'INTRO'

AgentDock Linux 一键部署将执行：
1. 安装/检查基础依赖和 Go 版本。
2. 克隆或更新 AgentDock 源码。
3. 构建 bin/agentdock。
4. 生成 /etc/agentdock/agentdock.env 和 systemd unit。
5. 启动服务并验证 healthz / MCP smoke。

生产建议：监听 127.0.0.1，通过 Caddy/Nginx 做 HTTPS 反代；不要把 AgentDock 直接裸露到公网。

INTRO

  repo_url="$(prompt 'Git 仓库 URL' "$DEFAULT_REPO_URL")"
  branch="$(prompt 'Git 分支' "$DEFAULT_BRANCH")"
  source_dir="$(prompt '源码安装目录' "$source_default")"
  data_dir="$(prompt '运行数据根目录' "$DEFAULT_DATA_DIR")"
  env_file="$(prompt '环境变量文件' "$DEFAULT_ENV_FILE")"
  service_name="$(prompt 'systemd 服务名' "$DEFAULT_SERVICE_NAME")"
  service_user="$(prompt '运行用户' "$DEFAULT_SERVICE_USER")"
  host="$(prompt '监听地址' "$DEFAULT_HOST")"
  port="$(prompt '监听端口' "$DEFAULT_PORT")"
  tool_profile="$(prompt '工具配置 profile：unified/read-only/compat-readonly-all' "$DEFAULT_TOOL_PROFILE")"
  log_level="$(prompt '日志级别' "$DEFAULT_LOG_LEVEL")"
  mode="$(prompt '运行模式：sandboxed/host' "$DEFAULT_MODE")"
  path_policy="$(prompt '路径策略：workspace/host' "$DEFAULT_PATH_POLICY")"
  sandbox_mode="$(prompt '命令沙箱：landlock/none' "$DEFAULT_SANDBOX_MODE")"

  validate_abs_path '源码安装目录' "$source_dir"
  validate_abs_path '运行数据根目录' "$data_dir"
  validate_abs_path '环境变量文件' "$env_file"
  validate_no_space 'systemd 服务名' "$service_name"
  validate_no_space '运行用户' "$service_user"
  validate_no_space '监听地址' "$host"
  validate_port "$port"

  workspace_dir="$data_dir/workspace"
  control_dir="$data_dir/AgentDock"
  nexus_state_dir="$data_dir/nexus"

  if confirm '是否安装/更新系统基础依赖？' y; then install_deps="yes"; else install_deps="no"; fi
  if confirm '源码目录已存在时是否尝试 git pull --ff-only？' y; then update_existing="yes"; else update_existing="no"; fi
  if confirm '是否运行 go test ./... 和 go vet ./...？首次部署可跳过以加快安装' n; then run_full_check="yes"; else run_full_check="no"; fi
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
- 源码目录：$source_dir
- 运行数据：$data_dir
- workspace：$workspace_dir
- AgentDock 控制目录：$control_dir
- env 文件：$env_file
- systemd 服务：$service_name
- 运行用户：$service_user
- 本机监听：http://$host:$port
- token：已隐藏，将写入 root-only env 文件

SUMMARY
  confirm '确认开始执行部署？' y || die '用户取消。'

  if [[ "$install_deps" == "yes" ]]; then
    install_packages
  fi

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

  ensure_service_user "$service_user" "$data_dir"
  service_group="$(id -gn "$service_user")"
  run_root mkdir -p "$workspace_dir" "$control_dir" "$nexus_state_dir"
  run_root chown -R "$service_user:$service_group" "$data_dir"

  log "构建 AgentDock"
  mkdir -p "$source_dir/bin"
  if [[ "$run_full_check" == "yes" ]]; then
    (cd "$source_dir" && go test ./... && go vet ./...)
  fi
  (cd "$source_dir" && go build -trimpath -o ./bin/agentdock ./cmd/agentdock)
  chmod +x "$source_dir/bin/agentdock"

  write_env_file "$env_file" "$workspace_dir" "$control_dir" "$host" "$port" "$token" "$tool_profile" "$log_level" "$mode" "$path_policy" "$sandbox_mode" "$skip_prompts" "$recall_endpoint" "$recall_token" "$nexus_endpoint" "$nexus_device_name" "$nexus_state_dir"
  write_systemd_unit "$service_name" "$service_user" "$service_group" "$source_dir" "$env_file"

  log "启动 systemd 服务：$service_name"
  run_root systemctl daemon-reload
  run_root systemctl enable --now "$service_name"
  run_root systemctl restart "$service_name"
  sleep 2
  run_root systemctl --no-pager --full status "$service_name" || true
  run_root systemctl is-active --quiet "$service_name"

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
  sudo systemctl status $service_name --no-pager
  sudo journalctl -u $service_name -n 100 --no-pager
  sudo systemctl restart $service_name

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
