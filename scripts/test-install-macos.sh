#!/bin/zsh
set -euo pipefail

ROOT_DIR="${0:A:h:h}"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-macos-installer-test.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

case "$(uname -m)" in
  arm64|aarch64) release_arch="arm64" ;;
  x86_64|amd64) release_arch="amd64" ;;
  *) print -u2 -- "unsupported test architecture: $(uname -m)"; exit 1 ;;
esac

release_dir="$TMP_ROOT/release"
build_dir="$TMP_ROOT/build"
install_dir="$TMP_ROOT/install/bin"
home_dir="$TMP_ROOT/home"
asset="agentdock_darwin_${release_arch}.tar.gz"
mkdir -p "$release_dir" "$build_dir/bin" "$home_dir"

(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$release_arch" \
    go build -trimpath -o "$build_dir/bin/agentdock" ./cmd/agentdock
)

tar -C "$build_dir" -czf "$release_dir/$asset" bin/agentdock
(
  cd "$release_dir"
  shasum -a 256 "$asset" > "$asset.sha256"
)

run_installer() {
  HOME="$home_dir" \
  AGENTDOCK_RELEASE_BASE_URL="file://$release_dir" \
  AGENTDOCK_INSTALL_DIR="$install_dir" \
  AGENTDOCK_BACKUP_DIR="$home_dir/.agentdock/backups/bin" \
    zsh "$ROOT_DIR/scripts/install-macos.sh" --version latest
}

run_installer
test -x "$install_dir/agentdock"
"$install_dir/agentdock" --help >/dev/null 2>&1

run_installer
backup_count="$(find "$home_dir/.agentdock/backups/bin" -type f -name 'agentdock.*' | wc -l | tr -d ' ')"
[[ "$backup_count" == "1" ]]

print -- "macOS release installer test passed"
