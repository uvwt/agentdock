#!/bin/zsh
set -euo pipefail

RELEASE_VERSION="${AGENTDOCK_RELEASE_VERSION:-latest}"
INSTALL_DIR="${AGENTDOCK_INSTALL_DIR:-$HOME/.local/bin}"
BACKUP_DIR="${AGENTDOCK_BACKUP_DIR:-$HOME/.agentdock/backups/bin}"
RELEASE_BASE_URL="${AGENTDOCK_RELEASE_BASE_URL:-}"

usage() {
  cat <<'EOF'
AgentDock macOS 预编译版本安装脚本。

用法：
  zsh install-macos.sh [--version latest|vX.Y.Z] [--install-dir PATH]

环境变量：
  AGENTDOCK_RELEASE_VERSION   Release 版本，默认 latest
  AGENTDOCK_INSTALL_DIR       二进制安装目录，默认 ~/.local/bin
  AGENTDOCK_BACKUP_DIR        旧二进制备份目录，默认 ~/.agentdock/backups/bin
  AGENTDOCK_RELEASE_BASE_URL  自定义 Release 下载根地址，主要用于镜像或测试
EOF
}

die() {
  print -u2 -- "ERROR: $*"
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
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
      shift 2
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

for command_name in curl install mktemp shasum tar uname; do
  require_command "$command_name"
done

case "$(uname -m)" in
  arm64|aarch64) release_arch="arm64" ;;
  x86_64|amd64) release_arch="amd64" ;;
  *) die "不支持的 macOS 架构：$(uname -m)" ;;
esac

asset="agentdock_darwin_${release_arch}.tar.gz"
base_url="$(release_url)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-install.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

print -- "==> 下载 $asset"
curl -fL --retry 3 --retry-delay 1 \
  "$base_url/$asset" \
  -o "$tmp_dir/$asset"
curl -fL --retry 3 --retry-delay 1 \
  "$base_url/$asset.sha256" \
  -o "$tmp_dir/$asset.sha256"

print -- "==> 校验 SHA-256"
(
  cd "$tmp_dir"
  shasum -a 256 -c "$asset.sha256"
)

mkdir -p "$tmp_dir/extract"
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir/extract"
source_binary="$tmp_dir/extract/bin/agentdock"
[[ -f "$source_binary" ]] || die "Release 压缩包缺少 bin/agentdock"

target="$INSTALL_DIR/agentdock"
mkdir -p "$INSTALL_DIR" "$HOME/.agentdock" "$HOME/AgentDock"

if [[ -f "$target" ]]; then
  mkdir -p "$BACKUP_DIR"
  backup="$BACKUP_DIR/agentdock.$(date +%Y%m%d%H%M%S)"
  cp -p "$target" "$backup"
  print -- "==> 已备份旧版本到 $backup"
fi

install -m 0755 "$source_binary" "$target"
"$target" --help >/dev/null 2>&1

print -- "installed: $target"
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  print -- "PATH 尚未包含 $INSTALL_DIR，可执行："
  print -- "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zprofile"
  print -- "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi

cat <<EOF

启动示例：
  $target --host 127.0.0.1 --port 8765

状态目录：
  $HOME/.agentdock
  $HOME/AgentDock
EOF
