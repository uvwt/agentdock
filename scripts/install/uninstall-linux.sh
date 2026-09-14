#!/bin/sh
set -eu

umask 077

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
AgentDock Linux 卸载器。

用法：
  sh uninstall-linux.sh                 交互选择卸载范围
  sh uninstall-linux.sh --services-only 仅移除服务，保留程序、配置和数据
  sh uninstall-linux.sh --purge-config  移除服务并清除 AgentDock 配置文件
  sh uninstall-linux.sh --purge-data    完全清除程序、配置和数据
USAGE
}

prompt_choice() {
  label="$1"
  default_value="$2"
  answer=""
  printf '%s [%s]: ' "$label" "$default_value" >>"$TTY_OUT"
  IFS= read -r answer <"$TTY_IN" || true
  printf '%s' "${answer:-$default_value}"
}

validate_service_name() {
  service_name="$1"
  case "$service_name" in
    ""|.*|*[!A-Za-z0-9_.@-]*)
      die "服务名包含不安全字符：$service_name"
      ;;
  esac
}

trim_trailing_slashes() {
  value="$1"
  while [ "$value" != "/" ] && [ "${value%/}" != "$value" ]; do
    value="${value%/}"
  done
  printf '%s' "$value"
}

validate_absolute_path() {
  label="$1"
  path="$2"
  case "$path" in
    /*) ;;
    *) die "$label 必须是绝对路径：$path" ;;
  esac
  case "$path" in
    *"/../"*|*"/.."|*"/./"*|*"/.")
      die "拒绝使用包含路径跳转的目录（${label}）：$path"
      ;;
  esac
}

canonical_existing_path() {
  path="$1"
  if command -v realpath >/dev/null 2>&1; then
    realpath "$path"
    return
  fi
  if command -v readlink >/dev/null 2>&1; then
    readlink -f "$path"
    return
  fi
  if [ -d "$path" ]; then
    (CDPATH='' cd -P -- "$path" && pwd -P)
    return
  fi
  parent="$(dirname "$path")"
  base="$(basename "$path")"
  canonical_parent="$(CDPATH='' cd -P -- "$parent" && pwd -P)"
  printf '%s/%s' "${canonical_parent%/}" "$base"
}

require_safe_removal_path() {
  label="$1"
  path="$2"
  validate_absolute_path "$label" "$path"
  normalized_path="$(trim_trailing_slashes "$path")"

  case "$normalized_path" in
    /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/var)
      die "拒绝删除危险路径（${label}）：$path"
      ;;
  esac

  case "$normalized_path" in
    /home/*|/Users/*)
      account_relative="${normalized_path#/*/}"
      case "$account_relative" in
        */*) ;;
        *) die "拒绝删除用户主目录（${label}）：$path" ;;
      esac
      ;;
  esac

  if [ -e "$normalized_path" ] || [ -L "$normalized_path" ]; then
    canonical_path="$(canonical_existing_path "$normalized_path")" || \
      die "无法解析待删除路径（${label}）：$path"
    [ "$canonical_path" = "$normalized_path" ] || \
      die "拒绝删除经过软链接或非规范路径指向的目录（${label}）：$path -> $canonical_path"
  fi

  printf '%s' "$normalized_path"
}

paths_overlap() {
  first="${1%/}"
  second="${2%/}"
  [ "$first" = "$second" ] && return 0
  case "$first/" in
    "$second/"*) return 0 ;;
  esac
  case "$second/" in
    "$first/"*) return 0 ;;
  esac
  return 1
}

path_contains() {
  parent="${1%/}"
  child="${2%/}"
  [ "$parent" = "$child" ] && return 0
  case "$child/" in
    "$parent/"*) return 0 ;;
  esac
  return 1
}

validate_managed_source() {
  source_dir="$1"
  [ -e "$source_dir" ] || return 0
  [ -d "$source_dir" ] || die "安装目录不是目录：$source_dir"

  [ -f "$source_dir/bin/agentdock" ] && return 0
  if [ -f "$source_dir/go.mod" ] && [ -d "$source_dir/cmd/agentdock" ]; then
    return 0
  fi
  die "拒绝清除无法识别为 AgentDock 的安装目录：$source_dir"
}

validate_service_directory() {
  label="$1"
  path="$2"
  validate_absolute_path "$label" "$path"
  normalized_path="$(trim_trailing_slashes "$path")"
  if [ -e "$normalized_path" ] || [ -L "$normalized_path" ]; then
    canonical_path="$(canonical_existing_path "$normalized_path")" || \
      die "无法解析服务目录（${label}）：$path"
    [ "$canonical_path" = "$normalized_path" ] || \
      die "拒绝使用经过软链接或非规范路径指向的服务目录（${label}）：$path -> $canonical_path"
  fi
}

remove_systemd_services() {
  service_name="$1"
  tunnel_service_name="$2"
  systemd_dir="$3"

  service_unit="$systemd_dir/$service_name.service"
  tunnel_unit="$systemd_dir/$tunnel_service_name.service"
  tunnel_dropin="$systemd_dir/$tunnel_service_name.service.d"

  if command -v systemctl >/dev/null 2>&1; then
    run_root systemctl disable --now "$tunnel_service_name" >/dev/null 2>&1 || true
    run_root systemctl disable --now "$service_name" >/dev/null 2>&1 || true
  fi

  run_root rm -f "$service_unit" "$tunnel_unit"
  run_root rm -rf "$tunnel_dropin"

  if command -v systemctl >/dev/null 2>&1; then
    run_root systemctl daemon-reload >/dev/null 2>&1 || true
    run_root systemctl reset-failed "$service_name" "$tunnel_service_name" >/dev/null 2>&1 || true
  fi
}

remove_openrc_services() {
  service_name="$1"
  tunnel_service_name="$2"
  openrc_dir="$3"

  if command -v rc-service >/dev/null 2>&1; then
    run_root rc-service "$tunnel_service_name" stop >/dev/null 2>&1 || true
    run_root rc-service "$service_name" stop >/dev/null 2>&1 || true
  fi
  if command -v rc-update >/dev/null 2>&1; then
    run_root rc-update del "$tunnel_service_name" default >/dev/null 2>&1 || true
    run_root rc-update del "$service_name" default >/dev/null 2>&1 || true
  fi

  run_root rm -f "$openrc_dir/$tunnel_service_name" "$openrc_dir/$service_name"
}

remove_managed_config() {
  env_file="$1"
  config_dir="$2"

  # 自定义配置目录可能与其他服务共用，只删除 AgentDock 管理的文件。
  run_root rm -f \
    "$env_file" \
    "$config_dir/cloudflared.env" \
    "$config_dir/desktop-runtime.json"

  if run_root rmdir "$config_dir" >/dev/null 2>&1; then
    log "已移除空配置目录：$config_dir"
  else
    log "配置目录仍包含其他文件，已保留：$config_dir"
  fi
}

remove_managed_data() {
  data_dir="$1"

  # 自定义数据目录可能与其他程序共用，只删除 AgentDock 管理的子目录。
  run_root rm -rf "$data_dir/.agentdock" "$data_dir/AgentDock"
  if run_root rmdir "$data_dir" >/dev/null 2>&1; then
    log "已移除空数据目录：$data_dir"
  else
    log "数据目录仍包含其他文件，已保留：$data_dir"
  fi
}

choose_scope() {
  cat >>"$TTY_OUT" <<'CHOICE'

卸载范围：
1) 仅移除服务，保留配置和数据
2) 同时清除 AgentDock 配置文件
3) 完全清除程序、配置和数据
CHOICE
  choice="$(prompt_choice '选择' 1)"
  case "$choice" in
    1) PURGE_CONFIG=false; PURGE_DATA=false ;;
    2) PURGE_CONFIG=true; PURGE_DATA=false ;;
    3) PURGE_CONFIG=true; PURGE_DATA=true ;;
    *) die "无效选择：$choice" ;;
  esac
}

PURGE_CONFIG=false
PURGE_DATA=false
INTERACTIVE=false
EXPLICIT_SCOPE=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --interactive)
      INTERACTIVE=true
      shift
      ;;
    --services-only)
      PURGE_CONFIG=false
      PURGE_DATA=false
      EXPLICIT_SCOPE=true
      shift
      ;;
    --purge-config)
      PURGE_CONFIG=true
      PURGE_DATA=false
      EXPLICIT_SCOPE=true
      shift
      ;;
    --purge-data)
      PURGE_CONFIG=true
      PURGE_DATA=true
      EXPLICIT_SCOPE=true
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

if [ "$INTERACTIVE" = true ]; then
  [ "$EXPLICIT_SCOPE" = false ] || die "--interactive 不能与卸载范围参数同时使用。"
  choose_scope
elif [ "$EXPLICIT_SCOPE" = false ]; then
  choose_scope
fi

service_name="${AGENTDOCK_SERVICE_NAME:-agentdock}"
tunnel_service_name="$service_name-cloudflared"
systemd_dir="${AGENTDOCK_SYSTEMD_DIR:-/etc/systemd/system}"
openrc_dir="${AGENTDOCK_OPENRC_DIR:-/etc/init.d}"
env_file="${AGENTDOCK_ENV_FILE:-/etc/agentdock/agentdock.env}"
config_dir="$(dirname "$env_file")"
source_dir="${AGENTDOCK_SOURCE_DIR:-/opt/agentdock}"
data_dir="${AGENTDOCK_DATA_DIR:-/srv/agentdock}"

validate_service_name "$service_name"
validate_service_directory "systemd 目录" "$systemd_dir"
validate_service_directory "OpenRC 目录" "$openrc_dir"
validate_absolute_path "环境变量文件" "$env_file"

# 先完成全部路径校验，再停止服务或删除任何文件，避免参数错误造成半卸载状态。
if is_true "$PURGE_CONFIG"; then
  require_safe_removal_path "配置目录" "$config_dir" >/dev/null
fi
if is_true "$PURGE_DATA"; then
  source_dir="$(require_safe_removal_path "安装目录" "$source_dir")"
  data_dir="$(require_safe_removal_path "数据目录" "$data_dir")"
  validate_managed_source "$source_dir"
  paths_overlap "$source_dir" "$data_dir" && \
    die "拒绝清除互相重叠的安装目录和数据目录：${source_dir}、$data_dir"
  path_contains "$source_dir" "$config_dir" && \
    die "拒绝清除与配置目录重叠的安装目录：${source_dir}、$config_dir"
  path_contains "$data_dir" "$config_dir" && \
    die "拒绝清除与配置目录重叠的数据目录：${data_dir}、$config_dir"
fi

engine_binary="$source_dir/bin/agentdock"
if [ -x "$engine_binary" ]; then
  engine_ready_out="$("$engine_binary" install --engine-ready 2>/dev/null || true)"
  case "$engine_ready_out" in
    *agentdock-installer-engine*)
      if is_true "$PURGE_DATA"; then
        run_root "$engine_binary" uninstall \
          --install-root "$source_dir" \
          --runtime-root "$config_dir" \
          --service-name "$service_name" \
          --systemd-dir "$systemd_dir" \
          --openrc-dir "$openrc_dir" \
          --purge-data || die "Go Installer Engine 卸载失败"
      elif is_true "$PURGE_CONFIG"; then
        run_root "$engine_binary" uninstall \
          --install-root "$source_dir" \
          --runtime-root "$config_dir" \
          --service-name "$service_name" \
          --systemd-dir "$systemd_dir" \
          --openrc-dir "$openrc_dir" \
          --purge-config || die "Go Installer Engine 卸载失败"
      else
        run_root "$engine_binary" uninstall \
          --install-root "$source_dir" \
          --runtime-root "$config_dir" \
          --service-name "$service_name" \
          --systemd-dir "$systemd_dir" \
          --openrc-dir "$openrc_dir" || die "Go Installer Engine 卸载失败"
      fi
      ;;
  esac
fi

remove_systemd_services "$service_name" "$tunnel_service_name" "$systemd_dir"
remove_openrc_services "$service_name" "$tunnel_service_name" "$openrc_dir"

if is_true "$PURGE_CONFIG"; then
  remove_managed_config "$env_file" "$config_dir"
fi
if is_true "$PURGE_DATA"; then
  run_root rm -rf "$source_dir"
  remove_managed_data "$data_dir"
fi

printf '\nAgentDock 已卸载。\n' >>"$TTY_OUT"
if ! is_true "$PURGE_CONFIG"; then
  printf '已保留配置：%s\n' "$config_dir" >>"$TTY_OUT"
fi
if ! is_true "$PURGE_DATA"; then
  printf '已保留程序和数据：%s、%s\n' "$source_dir" "$data_dir" >>"$TTY_OUT"
fi
printf 'cloudflared 未删除，避免影响其他服务。\n' >>"$TTY_OUT"
