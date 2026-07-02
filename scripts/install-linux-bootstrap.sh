#!/bin/sh
set -eu

REPO_URL="${AGENTDOCK_REPO_URL:-https://github.com/uvwt/agentdock.git}"
BRANCH="${AGENTDOCK_BRANCH:-main}"
INSTALL_URL="${AGENTDOCK_INSTALL_URL:-https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux.sh}"
TMP_SCRIPT="${TMPDIR:-/tmp}/agentdock-install.sh"

log() { printf '==> %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

need_root_cmd() {
  if [ "$(id -u)" = "0" ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    die "需要 root 或 sudo 来安装 bash/curl。"
  fi
}

install_minimal_deps() {
  if command -v bash >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
    return 0
  fi
  log "安装最小依赖：bash curl"
  if command -v apk >/dev/null 2>&1; then
    need_root_cmd apk add --no-cache bash curl ca-certificates
  elif command -v apt-get >/dev/null 2>&1; then
    need_root_cmd apt-get update
    need_root_cmd env DEBIAN_FRONTEND=noninteractive apt-get install -y bash curl ca-certificates
  elif command -v dnf >/dev/null 2>&1; then
    need_root_cmd dnf install -y bash curl ca-certificates
  elif command -v yum >/dev/null 2>&1; then
    need_root_cmd yum install -y bash curl ca-certificates
  elif command -v pacman >/dev/null 2>&1; then
    need_root_cmd pacman -Sy --needed --noconfirm bash curl ca-certificates
  elif command -v zypper >/dev/null 2>&1; then
    need_root_cmd zypper --non-interactive install bash curl ca-certificates
  else
    die "未识别包管理器；请先安装 bash 和 curl。"
  fi
}

download_installer() {
  log "下载 AgentDock Linux installer"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$INSTALL_URL" -o "$TMP_SCRIPT"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$TMP_SCRIPT" "$INSTALL_URL"
  else
    die "缺少 curl/wget，无法下载安装脚本。"
  fi
  chmod +x "$TMP_SCRIPT"
}

install_minimal_deps
download_installer
exec bash "$TMP_SCRIPT" "$@"
