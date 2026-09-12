#!/bin/sh
set -eu

# 公开 Unix bootstrap。下载并校验受信任安装器后交给平台适配层。
# 产品事务、manifest、rollback 的权威实现是 `agentdock install`（Installer Engine）。
# 不得改名删除本入口；平台脚本只是兼容适配，不再新增第二套状态机。

umask 077

DEFAULT_BASE_URL="https://github.com/uvwt/agentdock/releases/latest/download"
BASE_URL="${AGENTDOCK_INSTALLER_BASE_URL:-$DEFAULT_BASE_URL}"
TMP_ROOT=""
CLEAR_PUBLIC_URL=false
ENV_BACKUP=""
ENV_BACKUP_ACTIVE=false
ENV_BACKUP_EXISTS=false
TUNNEL_ENV_BACKUP=""
TUNNEL_ENV_BACKUP_EXISTS=false
PREVIOUS_TUNNEL_MODE="none"
TTY_IN="${AGENTDOCK_TTY_IN:-/dev/tty}"
TTY_OUT="${AGENTDOCK_TTY_OUT:-/dev/tty}"

if ! ( : <"$TTY_IN" ) 2>/dev/null; then
  TTY_IN="/dev/stdin"
fi
if ! ( : >"$TTY_OUT" ) 2>/dev/null; then
  TTY_OUT="/dev/stderr"
fi

log() { printf '==> %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# 由 trap 间接调用。
# shellcheck disable=SC2317,SC2329
cleanup() {
  if [ "${ENV_BACKUP_ACTIVE:-false}" = true ]; then
    restore_public_config >/dev/null 2>&1 || true
  fi
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

run_root() {
  if is_true "${AGENTDOCK_NO_SUDO:-false}" || [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    die "需要 root 权限，请安装 sudo 或使用 root 运行。"
  fi
}

usage() {
  cat <<'USAGE'
AgentDock 安装与维护入口。

用法：
  sh install.sh                 简洁安装；已安装时显示维护菜单
  sh install.sh --advanced      使用完整高级安装流程
  sh install.sh --uninstall     Linux：移除服务，保留程序、配置和数据
  sh install.sh --uninstall --purge-config
                                同时清除配置，便于重新安装
  sh install.sh --uninstall --purge-data
                                完全清除 AgentDock 程序、配置和数据
USAGE
}

download_file() {
  url="$1"
  destination="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$destination"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$destination" "$url"
    return
  fi
  die "缺少 curl 或 wget，无法下载安装器。"
}

sha256_file() {
  file="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
    return
  fi
  die "缺少 SHA-256 校验工具（sha256sum、shasum 或 openssl）。"
}

verify_checksum() {
  file="$1"
  checksum_file="$2"
  expected="$(awk 'NF { print $1; exit }' "$checksum_file" | tr '[:upper:]' '[:lower:]')"
  actual="$(sha256_file "$file" | tr '[:upper:]' '[:lower:]')"

  [ -n "$expected" ] || die "校验文件为空：$checksum_file"
  [ "$actual" = "$expected" ] || die "安装器 SHA-256 校验失败：$file"
}

script_dir() {
  case "$0" in
    */*) CDPATH='' cd -- "$(dirname -- "$0")" && pwd ;;
    *) die "本地安装器模式要求从文件路径执行 install.sh。" ;;
  esac
}

prepare_release_asset() {
  asset="$1"
  if [ "${AGENTDOCK_USE_LOCAL_PLATFORM_INSTALLER:-false}" = "true" ]; then
    asset_path="$(script_dir)/$asset"
    [ -f "$asset_path" ] || die "找不到本地安装器资源：$asset_path"
    printf '%s' "$asset_path"
    return
  fi

  asset_path="$TMP_ROOT/$asset"
  checksum_path="$asset_path.sha256"
  log "下载 AgentDock 安装器资源：$asset"
  download_file "$BASE_URL/$asset" "$asset_path"
  download_file "$BASE_URL/$asset.sha256" "$checksum_path"
  verify_checksum "$asset_path" "$checksum_path"
  chmod 700 "$asset_path"
  printf '%s' "$asset_path"
}

run_linux_uninstaller() {
  uninstaller_path="$(prepare_release_asset uninstall-linux.sh)"
  sh "$uninstaller_path" "$@"
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

prompt_secret_required() {
  label="$1"
  answer=""
  printf '%s（输入不回显）: ' "$label" >>"$TTY_OUT"
  if [ -t 0 ] || [ "$TTY_IN" = "/dev/tty" ]; then
    stty -echo <"$TTY_IN" 2>/dev/null || true
    IFS= read -r answer <"$TTY_IN" || true
    stty echo <"$TTY_IN" 2>/dev/null || true
    printf '\n' >>"$TTY_OUT"
  else
    IFS= read -r answer <"$TTY_IN" || true
  fi
  [ -n "$answer" ] || die "$label 不能为空。"
  printf '%s' "$answer"
}

root_file_exists() {
  run_root test -f "$1"
}

read_env_assignment() {
  file="$1"
  key="$2"
  root_file_exists "$file" || return 0
  # awk 程序使用单引号避免 Shell 展开，其中的 $1 属于 awk 字段。
  # shellcheck disable=SC2016
  run_root awk -F= -v key="$key" '$1 == key {value=substr($0, index($0, "=") + 1)} END {print value}' "$file"
}

linux_env_file() {
  printf '%s' "${AGENTDOCK_ENV_FILE:-/etc/agentdock/agentdock.env}"
}

linux_cloudflared_env_file() {
  env_file="$(linux_env_file)"
  printf '%s/cloudflared.env' "$(dirname "$env_file")"
}

linux_tunnel_service_name() {
  printf '%s-cloudflared' "${AGENTDOCK_SERVICE_NAME:-agentdock}"
}

current_tunnel_mode() {
  mode="${AGENTDOCK_TUNNEL_MODE:-}"
  if [ -z "$mode" ]; then
    mode="$(read_env_assignment "$(linux_cloudflared_env_file)" AGENTDOCK_TUNNEL_MODE)"
  fi
  printf '%s' "${mode:-none}"
}

persisted_tunnel_mode() {
  mode="$(read_env_assignment "$(linux_cloudflared_env_file)" AGENTDOCK_TUNNEL_MODE)"
  printf '%s' "${mode:-none}"
}

choose_public_access() {
  cat >>"$TTY_OUT" <<'CHOICE'

公网访问：
1) 仅本机
2) 临时公网地址
3) Cloudflare 固定地址
CHOICE
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1|none)
      AGENTDOCK_TUNNEL_MODE="none"
      CLEAR_PUBLIC_URL=true
      unset AGENTDOCK_SERVER_URL AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN 2>/dev/null || true
      ;;
    2|quick)
      AGENTDOCK_TUNNEL_MODE="quick"
      CLEAR_PUBLIC_URL=false
      unset AGENTDOCK_SERVER_URL AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN 2>/dev/null || true
      ;;
    3|named)
      AGENTDOCK_TUNNEL_MODE="named"
      CLEAR_PUBLIC_URL=false
      existing_url="$(read_env_assignment "$(linux_env_file)" AGENTDOCK_SERVER_URL)"
      AGENTDOCK_SERVER_URL="$(prompt_value 'HTTPS 公网地址' "$existing_url")"
      [ -n "$AGENTDOCK_SERVER_URL" ] || die "HTTPS 公网地址不能为空。"
      AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN="$(prompt_secret_required 'Cloudflare Tunnel Token')"
      ;;
    *) die "无效选择：$choice" ;;
  esac
  export AGENTDOCK_TUNNEL_MODE
  if [ -n "${AGENTDOCK_SERVER_URL:-}" ]; then
    export AGENTDOCK_SERVER_URL
  fi
  if [ -n "${AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN:-}" ]; then
    export AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN
  fi
}

backup_public_config() {
  [ "$ENV_BACKUP_ACTIVE" = false ] || return 0

  env_file="$(linux_env_file)"
  tunnel_env_file="$(linux_cloudflared_env_file)"
  ENV_BACKUP="$TMP_ROOT/agentdock.env.before-public-change"
  TUNNEL_ENV_BACKUP="$TMP_ROOT/cloudflared.env.before-public-change"
  PREVIOUS_TUNNEL_MODE="$(persisted_tunnel_mode)"

  if root_file_exists "$env_file"; then
    run_root cp -p "$env_file" "$ENV_BACKUP"
    ENV_BACKUP_EXISTS=true
  fi
  if root_file_exists "$tunnel_env_file"; then
    run_root cp -p "$tunnel_env_file" "$TUNNEL_ENV_BACKUP"
    TUNNEL_ENV_BACKUP_EXISTS=true
  fi
  ENV_BACKUP_ACTIVE=true
}

backup_and_clear_public_url() {
  env_file="$(linux_env_file)"
  root_file_exists "$env_file" || return 0
  backup_public_config

  filtered_file="$TMP_ROOT/agentdock.env.without-public-url"
  # shellcheck disable=SC2016
  run_root awk '
    $0 !~ /^[[:space:]]*(export[[:space:]]+)?AGENTDOCK_SERVER_URL[[:space:]]*=/ { print }
  ' "$env_file" >"$filtered_file"
  run_root cp "$filtered_file" "$env_file"
}

restore_public_config() {
  [ "$ENV_BACKUP_ACTIVE" = true ] || return 0
  env_file="$(linux_env_file)"
  tunnel_env_file="$(linux_cloudflared_env_file)"
  run_root mkdir -p "$(dirname "$env_file")" "$(dirname "$tunnel_env_file")"

  if [ "$ENV_BACKUP_EXISTS" = true ]; then
    run_root cp -p "$ENV_BACKUP" "$env_file"
  else
    run_root rm -f "$env_file"
  fi
  if [ "$TUNNEL_ENV_BACKUP_EXISTS" = true ]; then
    run_root cp -p "$TUNNEL_ENV_BACKUP" "$tunnel_env_file"
  else
    run_root rm -f "$tunnel_env_file"
  fi
  ENV_BACKUP_ACTIVE=false
  log "安装未完成，已恢复原公网配置（模式：${PREVIOUS_TUNNEL_MODE}）。"
}

commit_public_config() {
  ENV_BACKUP_ACTIVE=false
}

install_quick_tunnel_retry_guard() {
  mode="$1"
  [ "$mode" = "quick" ] || return 0
  [ "$(linux_service_manager)" = "systemd" ] || return 0

  systemd_dir="${AGENTDOCK_SYSTEMD_DIR:-/etc/systemd/system}"
  service_name="$(linux_tunnel_service_name)"
  dropin_dir="$systemd_dir/$service_name.service.d"
  temp_file="$TMP_ROOT/agentdock-retry-limit.conf"

  cat >"$temp_file" <<'UNIT'
[Unit]
StartLimitIntervalSec=300
StartLimitBurst=3

[Service]
RestartSec=30
UNIT
  run_root mkdir -p "$dropin_dir"
  run_root install -m 0644 "$temp_file" "$dropin_dir/retry-limit.conf"
  run_root systemctl daemon-reload >/dev/null 2>&1 || true
  run_root systemctl reset-failed "$service_name" >/dev/null 2>&1 || true
}

remove_quick_tunnel_retry_guard() {
  systemd_dir="${AGENTDOCK_SYSTEMD_DIR:-/etc/systemd/system}"
  service_name="$(linux_tunnel_service_name)"
  dropin_dir="$systemd_dir/$service_name.service.d"
  managed_file="$dropin_dir/retry-limit.conf"
  if run_root test -e "$managed_file"; then
    run_root rm -f "$managed_file"
    run_root rmdir "$dropin_dir" >/dev/null 2>&1 || true
    if [ "$(linux_service_manager)" = "systemd" ]; then
      run_root systemctl daemon-reload >/dev/null 2>&1 || true
    fi
  fi
}

linux_service_manager() {
  requested="${AGENTDOCK_SERVICE_MANAGER:-auto}"
  case "$requested" in
    systemd|openrc|none)
      printf '%s' "$requested"
      return
      ;;
    auto) ;;
    *) die "服务管理器必须是 auto/systemd/openrc/none：$requested" ;;
  esac

  systemd_dir="${AGENTDOCK_SYSTEMD_DIR:-/etc/systemd/system}"
  if command -v systemctl >/dev/null 2>&1 && { [ -d /run/systemd/system ] || [ -d "$systemd_dir" ]; }; then
    printf 'systemd'
  elif [ -f /etc/alpine-release ] || { command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; }; then
    printf 'openrc'
  else
    printf 'none'
  fi
}

quick_tunnel_rate_limited() {
  log_file="$1"
  started_at="${2:-}"
  if grep -Eq '429 Too Many Requests|error code: 1015|status_code=.?429' "$log_file" 2>/dev/null; then
    return 0
  fi

  service_name="$(linux_tunnel_service_name)"
  case "$(linux_service_manager)" in
    systemd)
      command -v journalctl >/dev/null 2>&1 || return 1
      if [ -n "$started_at" ]; then
        run_root journalctl -u "$service_name" --since "$started_at" --no-pager 2>/dev/null
      else
        run_root journalctl -u "$service_name" -n 100 --no-pager 2>/dev/null
      fi | grep -Eq '429 Too Many Requests|error code: 1015|status_code=.?429'
      ;;
    openrc)
      log_dir="${AGENTDOCK_OPENRC_LOG_DIR:-/var/log}"
      grep -Eq '429 Too Many Requests|error code: 1015|status_code=.?429' \
        "$log_dir/$service_name.log" "$log_dir/$service_name.err" 2>/dev/null
      ;;
    *) return 1 ;;
  esac
}

stop_rate_limited_tunnel() {
  service_name="$(linux_tunnel_service_name)"
  case "$(linux_service_manager)" in
    systemd)
      run_root systemctl disable --now "$service_name" >/dev/null 2>&1 || true
      ;;
    openrc)
      run_root rc-service "$service_name" stop >/dev/null 2>&1 || true
      run_root rc-update del "$service_name" default >/dev/null 2>&1 || true
      ;;
  esac
}

restart_restored_services() {
  service_name="${AGENTDOCK_SERVICE_NAME:-agentdock}"
  tunnel_service_name="$service_name-cloudflared"

  case "$(linux_service_manager)" in
    systemd)
      run_root systemctl restart "$service_name" >/dev/null 2>&1 || true
      if [ "$PREVIOUS_TUNNEL_MODE" = "none" ]; then
        run_root systemctl disable --now "$tunnel_service_name" >/dev/null 2>&1 || true
      else
        run_root systemctl enable --now "$tunnel_service_name" >/dev/null 2>&1 || true
      fi
      ;;
    openrc)
      run_root rc-service "$service_name" restart >/dev/null 2>&1 || true
      if [ "$PREVIOUS_TUNNEL_MODE" = "none" ]; then
        run_root rc-service "$tunnel_service_name" stop >/dev/null 2>&1 || true
        run_root rc-update del "$tunnel_service_name" default >/dev/null 2>&1 || true
      else
        run_root rc-update add "$tunnel_service_name" default >/dev/null 2>&1 || true
        run_root rc-service "$tunnel_service_name" restart >/dev/null 2>&1 || true
      fi
      ;;
  esac
}

print_linux_summary() {
  env_file="$(linux_env_file)"
  host="$(read_env_assignment "$env_file" AGENTDOCK_HOST)"
  port="$(read_env_assignment "$env_file" AGENTDOCK_PORT)"
  token="$(read_env_assignment "$env_file" AGENTDOCK_AUTH_TOKEN)"
  oauth_password="$(read_env_assignment "$env_file" AGENTDOCK_OAUTH_PASSWORD)"
  public_url="$(read_env_assignment "$env_file" AGENTDOCK_SERVER_URL)"
  mode="$(current_tunnel_mode)"
  host="${host:-127.0.0.1}"
  port="${port:-8765}"

  cat >>"$TTY_OUT" <<DONE

AgentDock 安装完成
本机地址：http://$host:$port/mcp
DONE
  if [ "$mode" != "none" ] && [ -n "$public_url" ]; then
    printf '公网地址：%s/mcp\n' "${public_url%/}" >>"$TTY_OUT"
  fi
  if [ -n "$token" ]; then
    printf 'Bearer Token：%s\n' "$token" >>"$TTY_OUT"
  fi
  if [ -n "$oauth_password" ]; then
    printf 'OAuth 密码：%s\n' "$oauth_password" >>"$TTY_OUT"
  fi
  printf '重新配置或卸载：再次运行 install.sh\n' >>"$TTY_OUT"
}

run_simple_linux_install() {
  installer_path="$1"
  shift
  mode="$(current_tunnel_mode)"
  started_at="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  install_quick_tunnel_retry_guard "$mode"
  backup_public_config
  if [ "$CLEAR_PUBLIC_URL" = true ]; then
    backup_and_clear_public_url
  fi

  install_log="$TMP_ROOT/linux-install.log"
  log "正在安装 AgentDock（公网模式：${mode}）"
  status=0
  AGENTDOCK_NONINTERACTIVE=true \
    "$PLATFORM_SHELL" "$installer_path" "$@" >"$install_log" 2>&1 || status=$?

  if [ "$status" -ne 0 ]; then
    if quick_tunnel_rate_limited "$install_log" "$started_at"; then
      stop_rate_limited_tunnel
      printf 'ERROR: Cloudflare 临时 Tunnel 请求受限（429/1015），已停止 Tunnel 服务，避免持续重试。\n' >&2
      printf '请稍后重新运行安装器，或改用 Cloudflare 固定地址。\n' >&2
      restore_public_config
      return "$status"
    fi
    restore_public_config
    restart_restored_services
    cat "$install_log" >&2
    return "$status"
  fi

  commit_public_config
  if [ "$mode" != "quick" ]; then
    remove_quick_tunnel_retry_guard
  fi
  print_linux_summary
}

run_direct_linux_install() {
  installer_path="$1"
  guard_mode="$2"
  shift 2
  started_at="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  install_quick_tunnel_retry_guard "$guard_mode"

  install_log="$TMP_ROOT/linux-direct-install.log"
  status_file="$TMP_ROOT/linux-direct-install.status"
  (
    set +e
    "$PLATFORM_SHELL" "$installer_path" "$@" 2>&1
    direct_status=$?
    printf '%s\n' "$direct_status" >"$status_file"
  ) | tee "$install_log"
  status="$(cat "$status_file")"

  if [ "$status" -ne 0 ]; then
    rate_limited=false
    if quick_tunnel_rate_limited "$install_log" "$started_at"; then
      stop_rate_limited_tunnel
      printf 'ERROR: Cloudflare 临时 Tunnel 请求受限（429/1015），已停止 Tunnel 服务，避免持续重试。\n' >&2
      printf '请稍后重新运行安装器，或改用 Cloudflare 固定地址。\n' >&2
      rate_limited=true
    fi
    restore_public_config
    if [ "$rate_limited" = false ]; then
      restart_restored_services
    fi
    return "$status"
  fi

  commit_public_config
  if [ "$(current_tunnel_mode)" != "quick" ]; then
    remove_quick_tunnel_retry_guard
  fi
  return 0
}

linux_existing_menu() {
  cat >>"$TTY_OUT" <<'MENU'

检测到已安装 AgentDock：
1) 更新或修复（保留当前配置）
2) 修改公网访问
3) 卸载
4) 高级安装
MENU
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1) SIMPLE_INSTALL=true ;;
    2) backup_public_config; choose_public_access; SIMPLE_INSTALL=true ;;
    3) run_linux_uninstaller --interactive; exit 0 ;;
    4) backup_public_config; ADVANCED=true ;;
    *) die "无效选择：$choice" ;;
  esac
}

ADVANCED=false
UNINSTALL=false
PURGE_CONFIG=false
PURGE_DATA=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --advanced)
      ADVANCED=true
      shift
      ;;
    --uninstall)
      UNINSTALL=true
      shift
      ;;
    --purge-config)
      PURGE_CONFIG=true
      shift
      ;;
    --purge-data)
      PURGE_CONFIG=true
      PURGE_DATA=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      break
      ;;
  esac
done

case "$(uname -s 2>/dev/null || true)" in
  Linux)
    PLATFORM_INSTALLER="install-linux-platform.sh"
    PLATFORM_SHELL="bash"
    ;;
  Darwin)
    PLATFORM_INSTALLER="install-macos-platform.sh"
    PLATFORM_SHELL="zsh"
    ;;
  *)
    die "当前系统不受 install.sh 支持；Windows 请使用 install.ps1。"
    ;;
esac

if [ "$PURGE_CONFIG" = true ] || [ "$PURGE_DATA" = true ]; then
  [ "$UNINSTALL" = true ] || die "--purge-config/--purge-data 必须与 --uninstall 一起使用。"
fi

# 源码开发需要显式开启本地模式；Release 用户始终下载并校验平台安装器。
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-installer.XXXXXX")"

if [ "$UNINSTALL" = true ]; then
  [ "$PLATFORM_INSTALLER" = "install-linux-platform.sh" ] || die "--uninstall 当前仅支持 Linux。"
  if [ "$PURGE_DATA" = true ]; then
    run_linux_uninstaller --purge-data
  elif [ "$PURGE_CONFIG" = true ]; then
    run_linux_uninstaller --purge-config
  else
    run_linux_uninstaller --services-only
  fi
  exit $?
fi

if [ "$PLATFORM_INSTALLER" != "install-linux-platform.sh" ]; then
  command -v "$PLATFORM_SHELL" >/dev/null 2>&1 || die "缺少 ${PLATFORM_SHELL}，无法运行 ${PLATFORM_INSTALLER}。"
  INSTALLER_PATH="$(prepare_release_asset "$PLATFORM_INSTALLER")"
  "$PLATFORM_SHELL" "$INSTALLER_PATH" "$@"
  exit $?
fi

command -v "$PLATFORM_SHELL" >/dev/null 2>&1 || die "缺少 ${PLATFORM_SHELL}，无法运行 ${PLATFORM_INSTALLER}。"

if is_true "${AGENTDOCK_NONINTERACTIVE:-false}"; then
  backup_public_config
  INSTALLER_PATH="$(prepare_release_asset "$PLATFORM_INSTALLER")"
  run_direct_linux_install "$INSTALLER_PATH" "$(current_tunnel_mode)" "$@"
  exit $?
fi

SIMPLE_INSTALL=false
if [ "$ADVANCED" = true ]; then
  # 高级流程可能在脚本内部选择 Quick Tunnel，因此预先安装保护；成功后按实际模式清理。
  backup_public_config
  INSTALLER_PATH="$(prepare_release_asset "$PLATFORM_INSTALLER")"
  run_direct_linux_install "$INSTALLER_PATH" quick "$@"
  exit $?
fi

if root_file_exists "$(linux_env_file)"; then
  linux_existing_menu
else
  if [ -z "${AGENTDOCK_TUNNEL_MODE:-}" ]; then
    choose_public_access
  fi
  SIMPLE_INSTALL=true
fi

if [ "$ADVANCED" = true ]; then
  INSTALLER_PATH="$(prepare_release_asset "$PLATFORM_INSTALLER")"
  run_direct_linux_install "$INSTALLER_PATH" quick "$@"
  exit $?
fi

if [ "$SIMPLE_INSTALL" = true ]; then
  INSTALLER_PATH="$(prepare_release_asset "$PLATFORM_INSTALLER")"
  run_simple_linux_install "$INSTALLER_PATH" "$@"
  exit $?
fi

die "未选择安装操作。"
