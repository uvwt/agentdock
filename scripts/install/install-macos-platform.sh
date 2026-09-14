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
AUTH_TOKEN_REPLACE=false
PUBLIC_AUTH_CONFIGURE=false
OAUTH_ENABLED_VALUE="false"
OAUTH_PASSWORD_VALUE=""
OAUTH_TOKEN_SECRET_VALUE=""
HOST_EXPLICIT=false
PORT_EXPLICIT=false
TUNNEL_MODE="${AGENTDOCK_TUNNEL_MODE:-none}"
TUNNEL_MODE_EXPLICIT=false
[[ -z "${AGENTDOCK_TUNNEL_MODE+x}" ]] || TUNNEL_MODE_EXPLICIT=true
SERVER_URL="${AGENTDOCK_SERVER_URL:-}"
SERVER_URL_EXPLICIT=false
[[ -z "${AGENTDOCK_SERVER_URL+x}" ]] || SERVER_URL_EXPLICIT=true
TUNNEL_TOKEN="${AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN:-}"
TUNNEL_TOKEN_FILE=""
NON_INTERACTIVE=false
RESULT_FILE=""
OFFLINE_INSTALL=false
AGENTDOCK_ARCHIVE=""
AGENTDOCK_CHECKSUM_FILE=""
CLOUDFLARED_RELEASE_BASE_URL="${AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL:-https://github.com/cloudflare/cloudflared/releases/latest/download}"
CLOUDFLARED_SOURCE_BINARY="${AGENTDOCK_CLOUDFLARED_BINARY:-}"
CLOUDFLARED_SOURCE_EXPLICIT=false
CLOUDFLARED_CHECKSUM_FILE=""

LABEL="com.uvwt.agentdock"
TUNNEL_LABEL="com.uvwt.agentdock.cloudflared"
APP_SUPPORT_DIR="$HOME/Library/Application Support/AgentDock"
AGENTDOCK_ENV="$APP_SUPPORT_DIR/agentdock.env"
START_SCRIPT="$APP_SUPPORT_DIR/start-agentdock.sh"
LAUNCH_AGENTS_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$LAUNCH_AGENTS_DIR/$LABEL.plist"
TUNNEL_ENV="$APP_SUPPORT_DIR/cloudflared.env"
TUNNEL_START_SCRIPT="$APP_SUPPORT_DIR/start-cloudflared.sh"
TUNNEL_PLIST_PATH="$LAUNCH_AGENTS_DIR/$TUNNEL_LABEL.plist"
QUICK_TUNNEL_URL_FILE="$APP_SUPPORT_DIR/quick-tunnel-url.txt"
LOG_DIR="$HOME/Library/Logs/AgentDock"
STDOUT_LOG="$LOG_DIR/agentdock.out.log"
STDERR_LOG="$LOG_DIR/agentdock.err.log"
TUNNEL_STDOUT_LOG="$LOG_DIR/cloudflared.out.log"
TUNNEL_STDERR_LOG="$LOG_DIR/cloudflared.err.log"
QUICK_TUNNEL_RUNTIME_LOG="$LOG_DIR/cloudflared-quick-current.log"
WORK_DIR="$HOME/AgentDock"
STATE_DIR="$HOME/.agentdock"
TARGET="$INSTALL_DIR/agentdock"
CLOUDFLARED_TARGET="$INSTALL_DIR/cloudflared"
SERVICE_WAS_LOADED=false
SERVICE_BACKUP_DIR=""

usage() {
  cat <<'USAGE'
AgentDock macOS 预编译版本安装脚本。

用法：
  curl -fsSL https://github.com/uvwt/agentdock/releases/latest/download/install.sh -o /tmp/agentdock-install.sh
  sh /tmp/agentdock-install.sh [选项]

选项：
  --version latest|vX.Y.Z  Release 版本，默认 latest
  --install-dir PATH       二进制安装目录，默认 ~/.local/bin
  --register-service       生成、注册并启动用户级 LaunchAgent
  --host HOST              服务监听地址，默认 127.0.0.1
  --port PORT              服务监听端口，默认 8765
  --auth-token TOKEN       首次创建 agentdock.env 时写入 Token；已有 Token 永不覆盖
  --tunnel MODE            公网方式：none、quick 或 named；默认 none
  --server-url URL         固定域名模式的 HTTPS 公网 Origin，例如 https://agent.example.com
  --tunnel-token-file PATH 从仅当前用户可读的文件读取 Named Tunnel Token，读取后删除
  --non-interactive        禁止终端询问；缺少必需参数时直接失败
  --result-file PATH       将安装结果以 JSON 写入指定文件，供图形界面读取
  --offline                强制仅使用本地载荷，禁止回退到公网下载
  --agentdock-archive PATH 使用本地 AgentDock Release 压缩包
  --agentdock-checksum-file PATH
                          使用本地 AgentDock SHA-256 校验文件
  --cloudflared-binary PATH
                          使用本地 cloudflared 二进制
  --cloudflared-checksum-file PATH
                          使用本地 cloudflared SHA-256 校验文件
  --no-start               只生成服务文件和 plist，不加载或启动 LaunchAgent
  -h, --help               显示帮助

环境变量：
  AGENTDOCK_RELEASE_VERSION   Release 版本，默认 latest
  AGENTDOCK_INSTALL_DIR       二进制安装目录，默认 ~/.local/bin
  AGENTDOCK_BACKUP_DIR        旧二进制备份目录，默认 ~/.agentdock/backups/bin
  AGENTDOCK_RELEASE_BASE_URL              自定义 AgentDock Release 下载根地址
  AGENTDOCK_TUNNEL_MODE                  高级覆盖：none、quick 或 named
  AGENTDOCK_SERVER_URL                   固定域名模式的 HTTPS 公网 Origin
  AGENTDOCK_OAUTH_PASSWORD               首次安装时可指定 OAuth 登录密码
  AGENTDOCK_OAUTH_TOKEN_SECRET           首次安装时可指定 OAuth 签名密钥
  AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN      Named Tunnel Token；不写入 agentdock.env
  AGENTDOCK_CLOUDFLARED_BINARY           使用指定的本地 cloudflared 二进制
  AGENTDOCK_CLOUDFLARED_RELEASE_BASE_URL 自定义 cloudflared Release 下载根地址

服务文件：
  ~/Library/Application Support/AgentDock/agentdock.env
  ~/Library/Application Support/AgentDock/start-agentdock.sh
  ~/Library/Application Support/AgentDock/cloudflared.env
  ~/Library/Application Support/AgentDock/start-cloudflared.sh
  ~/Library/LaunchAgents/com.uvwt.agentdock.plist
  ~/Library/LaunchAgents/com.uvwt.agentdock.cloudflared.plist
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

verify_payload_checksum() {
  local payload_path="$1"
  local checksum_path="$2"
  local payload_name="$3"
  local expected_hash actual_hash

  [[ -f "$payload_path" && ! -L "$payload_path" ]] || die "$payload_name 必须是普通文件：$payload_path"
  [[ -f "$checksum_path" && ! -L "$checksum_path" ]] || die "$payload_name 校验文件必须是普通文件：$checksum_path"

  expected_hash="$(sed -n '1{s/[[:space:]].*$//;p;}' "$checksum_path")"
  [[ "$expected_hash" =~ '^[[:xdigit:]]{64}$' ]] || die "$payload_name 校验文件格式无效：$checksum_path"
  actual_hash="$(shasum -a 256 "$payload_path" | sed 's/[[:space:]].*$//')"
  [[ "${actual_hash:l}" == "${expected_hash:l}" ]] || die "$payload_name SHA-256 校验失败"
}

cleanup_sensitive_input() {
  if [[ -n "$TUNNEL_TOKEN_FILE" && -e "$TUNNEL_TOKEN_FILE" ]]; then
    rm -f -- "$TUNNEL_TOKEN_FILE"
  fi
}

read_tunnel_token_file() {
  local file_path="$1"
  [[ -f "$file_path" && ! -L "$file_path" ]] || die "Tunnel Token 文件必须是普通文件：$file_path"

  local file_mode file_owner
  file_mode="$(stat -f '%Lp' "$file_path")" || die "无法读取 Tunnel Token 文件权限：$file_path"
  file_owner="$(stat -f '%u' "$file_path")" || die "无法读取 Tunnel Token 文件所有者：$file_path"
  [[ "$file_owner" == "$(id -u)" ]] || die "Tunnel Token 文件必须属于当前用户：$file_path"
  (( 8#$file_mode & 8#077 == 0 )) || die "Tunnel Token 文件不能允许组或其他用户访问：$file_path"

  TUNNEL_TOKEN="$(cat -- "$file_path")"
  rm -f -- "$file_path"
  TUNNEL_TOKEN_FILE=""
  [[ -n "$TUNNEL_TOKEN" ]] || die "Cloudflare Tunnel Token 不能为空"
  [[ "$TUNNEL_TOKEN" != *$'\n'* && "$TUNNEL_TOKEN" != *$'\r'* ]] || die "Cloudflare Tunnel Token 必须是单行文本"
}

write_result_file() {
  [[ -n "$RESULT_FILE" ]] || return 0

  local result_dir result_tmp installed_version service_address final_host final_port
  local local_mcp_url final_tunnel_mode public_url auth_token oauth_password
  result_dir="${RESULT_FILE:h}"
  [[ -n "$result_dir" ]] || result_dir="."
  mkdir -p "$result_dir"
  [[ ! -e "$RESULT_FILE" || ( -f "$RESULT_FILE" && ! -L "$RESULT_FILE" ) ]] || \
    die "安装结果路径必须是普通文件：$RESULT_FILE"

  installed_version="$("$TARGET" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
  [[ -n "$installed_version" ]] || die "无法读取已安装版本"
  service_address="$(read_service_address "$AGENTDOCK_ENV")" || die "无法读取最终服务地址"
  final_host="${service_address%%$'\t'*}"
  final_port="${service_address#*$'\t'}"
  local_mcp_url="http://$(health_host "$final_host"):$final_port/mcp"
  final_tunnel_mode="$TUNNEL_MODE"
  public_url=""
  if [[ "$final_tunnel_mode" != none ]]; then
    public_url="$(read_agentdock_env_key AGENTDOCK_SERVER_URL)"
  fi
  auth_token="$(read_agentdock_env_key AGENTDOCK_AUTH_TOKEN)"
  oauth_password="$(read_agentdock_env_key AGENTDOCK_OAUTH_PASSWORD)"

  result_tmp="$result_dir/.${RESULT_FILE:t}.tmp.$$"
  rm -f -- "$result_tmp"
  plutil -create xml1 "$result_tmp"
  plutil -insert schema_version -integer 1 "$result_tmp"
  plutil -insert ok -bool true "$result_tmp"
  plutil -insert version -string "$installed_version" "$result_tmp"
  plutil -insert healthy -bool "$([[ "$REGISTER_SERVICE" == true && "$NO_START" == false ]] && print true || print false)" "$result_tmp"
  plutil -insert local_mcp_url -string "$local_mcp_url" "$result_tmp"
  plutil -insert tunnel_mode -string "$final_tunnel_mode" "$result_tmp"
  plutil -insert public_url -string "$public_url" "$result_tmp"
  plutil -insert public_mcp_url -string "$([[ -n "$public_url" ]] && print "${public_url%/}/mcp" || print '')" "$result_tmp"
  plutil -insert auth_token -string "$auth_token" "$result_tmp"
  plutil -insert oauth_password -string "$oauth_password" "$result_tmp"
  plutil -convert json "$result_tmp"
  chmod 0600 "$result_tmp"
  mv -f "$result_tmp" "$RESULT_FILE"
}

validate_port() {
  [[ "$1" == <1-65535> ]] || die "端口必须是 1-65535：$1"
}

validate_tunnel_mode() {
  case "$1" in
    none|quick|named) ;;
    *) die "Tunnel 模式必须是 none、quick 或 named：$1" ;;
  esac
}

normalize_server_url() {
  local value="${1%/}"
  [[ "$value" == https://* ]] || die "Named Tunnel 公网地址必须使用 HTTPS：$1"
  local authority="${value#https://}"
  [[ -n "$authority" && "$authority" != */* && "$authority" =~ ^[A-Za-z0-9._:-]+$ ]] || \
    die "--server-url 只能填写 HTTPS Origin，不能包含路径或特殊字符：$1"
  print -r -- "$value"
}

generate_auth_token() {
  require_command openssl
  openssl rand -hex 32
}

generate_oauth_password() {
  require_command openssl
  openssl rand -hex 12
}

generate_oauth_token_secret() {
  require_command openssl
  openssl rand -hex 32
}

read_agentdock_env_key() {
  local key="$1"
  [[ -f "$AGENTDOCK_ENV" && ! -L "$AGENTDOCK_ENV" ]] || return 0
  /bin/zsh -c '
    source "$1" >/dev/null
    case "$2" in
      AGENTDOCK_AUTH_TOKEN) print -r -- "${AGENTDOCK_AUTH_TOKEN:-}" ;;
      AGENTDOCK_SERVER_URL) print -r -- "${AGENTDOCK_SERVER_URL:-}" ;;
      AGENTDOCK_OAUTH_ENABLED) print -r -- "${AGENTDOCK_OAUTH_ENABLED:-}" ;;
      AGENTDOCK_OAUTH_PASSWORD) print -r -- "${AGENTDOCK_OAUTH_PASSWORD:-}" ;;
      AGENTDOCK_OAUTH_TOKEN_SECRET) print -r -- "${AGENTDOCK_OAUTH_TOKEN_SECRET:-}" ;;
      *) exit 2 ;;
    esac
  ' _ "$AGENTDOCK_ENV" "$key"
}

read_existing_tunnel_mode() {
  [[ -f "$TUNNEL_ENV" && ! -L "$TUNNEL_ENV" ]] || return 0
  /bin/zsh -c '
    unset AGENTDOCK_TUNNEL_MODE
    source "$1" >/dev/null
    print -r -- "${AGENTDOCK_TUNNEL_MODE:-}"
  ' _ "$TUNNEL_ENV"
}

read_existing_tunnel_token() {
  [[ -f "$TUNNEL_ENV" && ! -L "$TUNNEL_ENV" ]] || return 0
  /bin/zsh -c '
    unset TUNNEL_TOKEN
    source "$1" >/dev/null
    print -r -- "${TUNNEL_TOKEN:-}"
  ' _ "$TUNNEL_ENV"
}


ensure_public_auth() {
  local current_token="$(read_agentdock_env_key AGENTDOCK_AUTH_TOKEN)"
  local current_password="$(read_agentdock_env_key AGENTDOCK_OAUTH_PASSWORD)"
  local current_secret="$(read_agentdock_env_key AGENTDOCK_OAUTH_TOKEN_SECRET)"

  if [[ -n "$current_token" ]]; then
    AUTH_TOKEN_ARG="$current_token"
  elif [[ -n "$AUTH_TOKEN_ARG" ]]; then
    AUTH_TOKEN_REPLACE=true
  else
    AUTH_TOKEN_ARG="$(generate_auth_token)"
    AUTH_TOKEN_REPLACE=true
    print -- "==> 已自动生成 Bearer Token"
  fi

  if [[ -n "$current_password" ]]; then
    OAUTH_PASSWORD_VALUE="$current_password"
  elif [[ -n "${AGENTDOCK_OAUTH_PASSWORD:-}" ]]; then
    OAUTH_PASSWORD_VALUE="$AGENTDOCK_OAUTH_PASSWORD"
  else
    OAUTH_PASSWORD_VALUE="$(generate_oauth_password)"
    print -- "==> 已自动生成 OAuth 登录密码"
  fi

  if [[ -n "$current_secret" ]]; then
    OAUTH_TOKEN_SECRET_VALUE="$current_secret"
  elif [[ -n "${AGENTDOCK_OAUTH_TOKEN_SECRET:-}" ]]; then
    OAUTH_TOKEN_SECRET_VALUE="$AGENTDOCK_OAUTH_TOKEN_SECRET"
  else
    OAUTH_TOKEN_SECRET_VALUE="$(generate_oauth_token_secret)"
  fi

  (( ${#OAUTH_PASSWORD_VALUE} >= 12 )) || die "OAuth 登录密码至少需要 12 个字符"
  (( ${#OAUTH_TOKEN_SECRET_VALUE} >= 32 )) || die "OAuth 签名密钥至少需要 32 个字节"
}

prompt_named_server_url() {
  [[ "$NON_INTERACTIVE" == false && -t 0 ]] || die "固定域名模式需要设置 AGENTDOCK_SERVER_URL 或 --server-url"
  print -n -- "固定 HTTPS 公网地址（例如 https://agent.example.com）: "
  read -r SERVER_URL
  [[ -n "$SERVER_URL" ]] || die "固定公网地址不能为空"
}

prompt_named_tunnel_token() {
  [[ "$NON_INTERACTIVE" == false && -t 0 ]] || die "Named Tunnel 需要通过环境变量或 --tunnel-token-file 提供 Tunnel Token"
  print -n -- "Cloudflare Tunnel Token（输入不回显）: "
  read -rs TUNNEL_TOKEN
  print
  [[ -n "$TUNNEL_TOKEN" ]] || die "Cloudflare Tunnel Token 不能为空"
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

  # Homebrew 等包管理器通常通过软链接暴露命令。只解析链接并复制真实文件，
  # 不直接把外部路径写入 LaunchAgent，避免后续 shell PATH 变化影响服务启动。
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
  print -r -- "$candidate"
}

install_cloudflared() {
  local source_binary="$CLOUDFLARED_SOURCE_BINARY"
  local discovered_binary resolved_binary
  local archive asset staged_target

  if [[ -z "$source_binary" && -f "$CLOUDFLARED_TARGET" && ! -L "$CLOUDFLARED_TARGET" && \
        -x "$CLOUDFLARED_TARGET" ]] && "$CLOUDFLARED_TARGET" --version >/dev/null 2>&1; then
    return
  fi

  if [[ -n "$source_binary" ]]; then
    if [[ -n "$CLOUDFLARED_CHECKSUM_FILE" ]]; then
      # 离线载荷在执行 --version 之前先校验，避免损坏或被替换的二进制先获得执行机会。
      verify_payload_checksum "$source_binary" "$CLOUDFLARED_CHECKSUM_FILE" "cloudflared 离线载荷"
    elif [[ "$OFFLINE_INSTALL" == true ]]; then
      die "离线安装必须提供 --cloudflared-checksum-file"
    fi
    resolved_binary="$(resolve_cloudflared_binary "$source_binary")" || \
      die "AGENTDOCK_CLOUDFLARED_BINARY 指向的 cloudflared 无效：$source_binary"
    source_binary="$resolved_binary"
  else
    if [[ -L "$CLOUDFLARED_TARGET" ]] && \
       resolved_binary="$(resolve_cloudflared_binary "$CLOUDFLARED_TARGET")"; then
      source_binary="$resolved_binary"
      print -- "==> 复用现有 cloudflared：$CLOUDFLARED_TARGET"
    fi
    if [[ -z "$source_binary" ]]; then
      discovered_binary="$(command -v cloudflared 2>/dev/null || true)"
      if [[ -n "$discovered_binary" ]]; then
        if resolved_binary="$(resolve_cloudflared_binary "$discovered_binary")"; then
          source_binary="$resolved_binary"
          print -- "==> 复用系统 cloudflared：$discovered_binary"
        else
          print -u2 -- "警告：忽略 PATH 中无效的 cloudflared：$discovered_binary"
        fi
      fi
    fi
  fi

  if [[ -z "$source_binary" ]]; then
    [[ "$OFFLINE_INSTALL" == false ]] || die "离线安装包缺少可用的 cloudflared 载荷"
    asset="cloudflared-darwin-${release_arch}.tgz"
    archive="$tmp_dir/$asset"
    print -- "==> 下载 Cloudflare cloudflared：$asset"
    curl -fL --retry 3 --retry-delay 1 "${CLOUDFLARED_RELEASE_BASE_URL%/}/$asset" -o "$archive"
    mkdir -p "$tmp_dir/cloudflared"
    tar -xzf "$archive" -C "$tmp_dir/cloudflared"
    source_binary="$tmp_dir/cloudflared/cloudflared"
    resolved_binary="$(resolve_cloudflared_binary "$source_binary")" || \
      die "下载的 cloudflared 无效：$source_binary"
    source_binary="$resolved_binary"
  fi

  if [[ "$source_binary" != "$CLOUDFLARED_TARGET" ]]; then
    mkdir -p "$(dirname "$CLOUDFLARED_TARGET")"
    staged_target="$CLOUDFLARED_TARGET.tmp.$$"
    rm -f "$staged_target"
    if ! install -m 0755 "$source_binary" "$staged_target"; then
      rm -f "$staged_target"
      die "安装 cloudflared 失败：$CLOUDFLARED_TARGET"
    fi
    if ! mv -f "$staged_target" "$CLOUDFLARED_TARGET"; then
      rm -f "$staged_target"
      die "替换 cloudflared 失败：$CLOUDFLARED_TARGET"
    fi
  fi
  "$CLOUDFLARED_TARGET" --version >/dev/null
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

main() {
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
      CLOUDFLARED_TARGET="$INSTALL_DIR/cloudflared"
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
    --tunnel)
      (( $# >= 2 )) || die "--tunnel 需要值"
      TUNNEL_MODE="$2"
      TUNNEL_MODE_EXPLICIT=true
      shift 2
      ;;
    --server-url)
      (( $# >= 2 )) || die "--server-url 需要值"
      SERVER_URL="$2"
      SERVER_URL_EXPLICIT=true
      shift 2
      ;;
    --tunnel-token-file)
      (( $# >= 2 )) || die "--tunnel-token-file 需要值"
      [[ -z "$TUNNEL_TOKEN_FILE" ]] || die "--tunnel-token-file 只能指定一次"
      TUNNEL_TOKEN_FILE="$2"
      shift 2
      ;;
    --non-interactive)
      NON_INTERACTIVE=true
      shift
      ;;
    --result-file)
      (( $# >= 2 )) || die "--result-file 需要值"
      RESULT_FILE="$2"
      shift 2
      ;;
    --offline)
      OFFLINE_INSTALL=true
      shift
      ;;
    --agentdock-archive)
      (( $# >= 2 )) || die "--agentdock-archive 需要值"
      [[ -z "$AGENTDOCK_ARCHIVE" ]] || die "--agentdock-archive 只能指定一次"
      AGENTDOCK_ARCHIVE="$2"
      shift 2
      ;;
    --agentdock-checksum-file)
      (( $# >= 2 )) || die "--agentdock-checksum-file 需要值"
      [[ -z "$AGENTDOCK_CHECKSUM_FILE" ]] || die "--agentdock-checksum-file 只能指定一次"
      AGENTDOCK_CHECKSUM_FILE="$2"
      shift 2
      ;;
    --cloudflared-binary)
      (( $# >= 2 )) || die "--cloudflared-binary 需要值"
      [[ "$CLOUDFLARED_SOURCE_EXPLICIT" == false ]] || die "--cloudflared-binary 只能指定一次"
      CLOUDFLARED_SOURCE_BINARY="$2"
      CLOUDFLARED_SOURCE_EXPLICIT=true
      shift 2
      ;;
    --cloudflared-checksum-file)
      (( $# >= 2 )) || die "--cloudflared-checksum-file 需要值"
      [[ -z "$CLOUDFLARED_CHECKSUM_FILE" ]] || die "--cloudflared-checksum-file 只能指定一次"
      CLOUDFLARED_CHECKSUM_FILE="$2"
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
if [[ -n "$AGENTDOCK_ARCHIVE" || -n "$AGENTDOCK_CHECKSUM_FILE" ]]; then
  [[ -n "$AGENTDOCK_ARCHIVE" && -n "$AGENTDOCK_CHECKSUM_FILE" ]] || \
    die "--agentdock-archive 与 --agentdock-checksum-file 必须同时提供"
fi
if [[ -n "$CLOUDFLARED_CHECKSUM_FILE" && -z "$CLOUDFLARED_SOURCE_BINARY" ]]; then
  die "--cloudflared-checksum-file 必须与 --cloudflared-binary 同时提供"
fi
if [[ "$OFFLINE_INSTALL" == true ]]; then
  [[ -n "$AGENTDOCK_ARCHIVE" && -n "$AGENTDOCK_CHECKSUM_FILE" ]] || \
    die "离线安装包缺少 AgentDock 核心载荷"
fi
trap 'cleanup_sensitive_input' EXIT
if [[ -n "$TUNNEL_TOKEN_FILE" ]]; then
  [[ -z "$TUNNEL_TOKEN" ]] || die "不能同时使用环境变量和 --tunnel-token-file 提供 Tunnel Token"
  read_tunnel_token_file "$TUNNEL_TOKEN_FILE"
fi
if [[ "$REGISTER_SERVICE" == true && "$TUNNEL_MODE_EXPLICIT" == false ]]; then
  existing_tunnel_mode="$(read_existing_tunnel_mode)"
  case "$existing_tunnel_mode" in
    quick|named)
      TUNNEL_MODE="$existing_tunnel_mode"
      TUNNEL_MODE_EXPLICIT=true
      print -- "==> 沿用现有公网访问方式：$TUNNEL_MODE"
      ;;
    *)
      # 登录自启与公网访问是两个独立选择。首次注册服务默认仅本机使用，
      # 只有调用方显式传入 --tunnel quick|named 才创建公网入口。
      TUNNEL_MODE=none
      ;;
  esac
fi
validate_port "$SERVICE_PORT"
validate_tunnel_mode "$TUNNEL_MODE"
if [[ "$OFFLINE_INSTALL" == true && "$TUNNEL_MODE" != none ]]; then
  [[ -n "$CLOUDFLARED_SOURCE_BINARY" && -n "$CLOUDFLARED_CHECKSUM_FILE" ]] || \
    die "离线公网安装缺少 cloudflared 载荷或校验文件"
fi
if [[ "$TUNNEL_MODE" != none ]]; then
  REGISTER_SERVICE=true
  PUBLIC_AUTH_CONFIGURE=true
  ensure_public_auth
  if [[ "$TUNNEL_MODE" == named ]]; then
    if [[ -z "$SERVER_URL" ]]; then
      SERVER_URL="$(read_agentdock_env_key AGENTDOCK_SERVER_URL)"
    fi
    [[ -n "$SERVER_URL" ]] || prompt_named_server_url
    SERVER_URL="$(normalize_server_url "$SERVER_URL")"
    SERVER_URL_EXPLICIT=true
    OAUTH_ENABLED_VALUE="true"
    if [[ -z "$TUNNEL_TOKEN" ]]; then
      TUNNEL_TOKEN="$(read_existing_tunnel_token)"
    fi
    [[ -n "$TUNNEL_TOKEN" ]] || prompt_named_tunnel_token
  else
    # 重跑安装器刷新临时地址时，先保留旧 Origin 让现有 OAuth 服务继续可用；
    # 新 Tunnel 成功后再原子更新地址并重启 AgentDock。--no-start 没有新地址，
    # 因此必须清空旧 Origin，避免把固定域名或已失效临时地址继续交付给用户。
    if [[ "$NO_START" == true ]]; then
      SERVER_URL=""
      OAUTH_ENABLED_VALUE="false"
    else
      if [[ -z "$SERVER_URL" ]]; then
        SERVER_URL="$(read_agentdock_env_key AGENTDOCK_SERVER_URL)"
      fi
      if [[ -n "$SERVER_URL" ]]; then
        OAUTH_ENABLED_VALUE="true"
      else
        OAUTH_ENABLED_VALUE="false"
      fi
    fi
    SERVER_URL_EXPLICIT=true
  fi
fi
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
  cleanup_sensitive_input
}
trap cleanup EXIT

if [[ "$REGISTER_SERVICE" == true ]]; then
  snapshot_service_files
fi

if [[ -n "$AGENTDOCK_ARCHIVE" ]]; then
  print -- "==> 使用内置 AgentDock 离线载荷：$asset"
  verify_payload_checksum "$AGENTDOCK_ARCHIVE" "$AGENTDOCK_CHECKSUM_FILE" "AgentDock 离线载荷"
  cp -p "$AGENTDOCK_ARCHIVE" "$tmp_dir/$asset"
else
  [[ "$OFFLINE_INSTALL" == false ]] || die "离线安装禁止回退到公网下载"
  print -- "==> 下载 $asset"
  curl -fL --retry 3 --retry-delay 1 "$base_url/$asset" -o "$tmp_dir/$asset"
  curl -fL --retry 3 --retry-delay 1 "$base_url/$asset.sha256" -o "$tmp_dir/$asset.sha256"

  print -- "==> 校验 SHA-256"
  (
    cd "$tmp_dir"
    shasum -a 256 -c "$asset.sha256"
  )
fi

mkdir -p "$tmp_dir/extract"
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir/extract"
source_binary="$tmp_dir/extract/bin/agentdock"
core_skill_bundle="$tmp_dir/extract/share/agentdock/core-skills"
[[ -f "$source_binary" && ! -L "$source_binary" ]] || die "Release 压缩包中的 bin/agentdock 必须是普通文件"
[[ -d "$core_skill_bundle" && ! -L "$core_skill_bundle" ]] || die "Release 压缩包缺少核心 Skill Bundle"
[[ -f "$core_skill_bundle/manifest.json" && ! -L "$core_skill_bundle/manifest.json" ]] || die "Release 压缩包中的核心 Skill manifest 必须是普通文件"

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

"$source_binary" --help >/dev/null 2>&1
staged_target="$INSTALL_DIR/.agentdock.install.$$"

restore_install_files() {
  if ! restore_service_files; then
    die "生成服务文件失败，且旧服务文件恢复失败"
  fi
  if [[ -n "$backup" && -f "$backup" ]]; then
    cp -p "$backup" "$staged_target" || die "生成服务文件失败，且旧二进制复制失败；备份保留在 $backup"
    mv -f "$staged_target" "$TARGET" || die "生成服务文件失败，且旧二进制恢复失败；备份保留在 $backup"
  else
    rm -f "$TARGET"
  fi
}

install_live_binary_from_payload() {
  rm -f "$staged_target"
  install -m 0755 "$source_binary" "$staged_target"
  mv -f "$staged_target" "$TARGET"
}

if [[ "$REGISTER_SERVICE" == true ]]; then
  # 安装脚本与 payload 来自同一 release：binary 必然支持 Engine。旧版本请使用
  # 对应旧 release 的安装脚本，本脚本不再保留 legacy 安装状态机（env/plist/
  # LaunchAgent/tunnel/rollback 全部由 Go Installer Engine 拥有，重复实现会腐烂）。
  if [[ "$("$source_binary" install --engine-ready 2>/dev/null || true)" != *agentdock-installer-engine* ]]; then
    die "payload 不支持 Go Installer Engine；安装脚本与 payload 必须来自同一 release"
  fi
  if [[ "$TUNNEL_MODE" != none ]]; then
      install_cloudflared
    fi
    install_args=(
      install
      --install-root "$INSTALL_DIR"
      --runtime-root "$APP_SUPPORT_DIR"
      --binary "$source_binary"
      --live-binary "$TARGET"
      --skill-bundle "$core_skill_bundle"
      --log-level "$SERVICE_LOG_LEVEL"
      --tunnel-mode "$TUNNEL_MODE"
      --launch-agents-dir "$LAUNCH_AGENTS_DIR"
      --data-dir "$WORK_DIR"
      --agentdock-home "$STATE_DIR"
      --register-service
    )
    if [[ "$NO_START" == true ]]; then
      install_args+=(--no-start --skip-health)
    fi
    if [[ "$HOST_EXPLICIT" == true ]]; then
      install_args+=(--host "$SERVICE_HOST")
    fi
    if [[ "$PORT_EXPLICIT" == true ]]; then
      install_args+=(--port "$SERVICE_PORT")
    fi
    if [[ -n "$SERVER_URL" ]]; then
      install_args+=(--server-url "$SERVER_URL")
    fi
    if [[ -n "$TUNNEL_TOKEN" ]]; then
      install_args+=(--tunnel-token "$TUNNEL_TOKEN")
    fi
    if [[ -x "$CLOUDFLARED_TARGET" ]]; then
      install_args+=(--cloudflared "$CLOUDFLARED_TARGET")
    fi
    if [[ -z "$(read_agentdock_env_key AGENTDOCK_AUTH_TOKEN 2>/dev/null || true)" && -n "$AUTH_TOKEN_ARG" ]]; then
      install_args+=(--auth-token "$AUTH_TOKEN_ARG")
    fi
    if [[ -n "$OAUTH_PASSWORD_VALUE" ]]; then
      install_args+=(--oauth-password "$OAUTH_PASSWORD_VALUE")
    fi
    if [[ -n "$OAUTH_TOKEN_SECRET_VALUE" ]]; then
      install_args+=(--oauth-token-secret "$OAUTH_TOKEN_SECRET_VALUE")
    fi
    if ! "$source_binary" "${install_args[@]}" >"$tmp_dir/installer-engine.json"; then
      restore_install_files
      die "Go Installer Engine 失败，已禁止回退 legacy 实现"
    fi

  if [[ "$NO_START" == true ]]; then
    print -- "==> 已生成服务文件和 plist，按 --no-start 要求未加载 LaunchAgent"
  else
    print -- "==> 已由 Go Installer Engine 完成 LaunchAgent 启动与健康检查"
  fi
else
  install_live_binary_from_payload
fi

write_result_file
print -- "installed: $TARGET"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  print -- "PATH 尚未包含 ${INSTALL_DIR}，可执行："
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
  if [[ "$TUNNEL_MODE_EXPLICIT" == true && "$TUNNEL_MODE" != none ]]; then
    # 公网地址的最终投影属于 Engine（quick 地址由 tunnel 运行时回写 env）。
    # 终端摘要必须消费最终 env 值，不能沿用调用 Engine 之前的旧 shell 变量。
    final_server_url="$(read_agentdock_env_key AGENTDOCK_SERVER_URL 2>/dev/null || true)"
    if [[ -z "$final_server_url" ]]; then
      final_server_url="$SERVER_URL"
    fi
    local_public_url="$final_server_url"
    local_mcp_url="${final_server_url%/}/mcp"
    if [[ "$TUNNEL_MODE" == quick && "$NO_START" == true ]]; then
      local_public_url="启动 Tunnel 后生成"
      local_mcp_url="启动 Tunnel 后生成"
    fi
    final_auth_token="$(read_agentdock_env_key AGENTDOCK_AUTH_TOKEN)"
    final_oauth_password="$(read_agentdock_env_key AGENTDOCK_OAUTH_PASSWORD)"
    cat <<STATUS

╭─ AgentDock 安装完成 ─────────────────────────
│ 公网模式：$TUNNEL_MODE
│ 公网地址：$local_public_url
│ MCP 地址：$local_mcp_url
│ Bearer Token：$final_auth_token
│ OAuth 登录密码：$final_oauth_password
│ 认证方式：Bearer Token、OAuth 均已配置
│ 配置文件：$AGENTDOCK_ENV
│ Tunnel 日志：$TUNNEL_STDERR_LOG
╰──────────────────────────────────────────────
STATUS
    if [[ "$TUNNEL_MODE" == quick ]]; then
      if [[ "$NO_START" == true ]]; then
        print -- "临时模式尚未启动；实际启动后安装器会写入公网地址并启用 OAuth。"
      else
        print -- "临时地址在 Tunnel 重启后可能变化；重新运行同一安装命令即可刷新，认证凭据保持不变。"
        print -- "地址变化后，请在客户端替换 MCP URL 并重新完成 OAuth 授权。"
      fi
    fi
  fi
else
  cat <<STATUS

启动示例：
  $TARGET --host 127.0.0.1 --port 8765

注册后台服务：
  sh install.sh --register-service
STATUS
fi
}

if [[ "${ZSH_EVAL_CONTEXT:-toplevel}" != *:file ]]; then
  main "$@"
fi
