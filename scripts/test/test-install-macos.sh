#!/bin/zsh
set -euo pipefail

ROOT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
[[ "$(uname -s)" == Darwin ]] || { print -u2 -- "macOS only"; exit 0; }

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-unified-macos-test.XXXXXX")"
cleanup() { rm -rf "$TMP_ROOT"; }
trap cleanup EXIT HUP INT TERM

arch="$(go env GOARCH)"
release="$TMP_ROOT/release"
payload="$TMP_ROOT/payload"
home="$TMP_ROOT/home"
install_dir="$TMP_ROOT/bin"
runtime_root="$TMP_ROOT/runtime"
state_dir="$home/.agentdock"
work_dir="$home/AgentDock"
launch_agents="$home/Library/LaunchAgents"
mkdir -p "$release" "$payload/bin" "$payload/share/agentdock" "$home" "$launch_agents"

CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -o "$payload/bin/agentdock" ./cmd/agentdock
python3 packaging/build-core-skill-bundle.py --output "$payload/share/agentdock/core-skills"
asset="agentdock_darwin_${arch}.tar.gz"
tar -C "$payload" -czf "$release/$asset" bin/agentdock share/agentdock/core-skills
shasum -a 256 "$release/$asset" | awk -v a="$asset" '{print $1 "  " a}' > "$release/$asset.sha256"

run_install() {
  HOME="$home" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$release" \
  AGENTDOCK_INSTALL_DIR="$install_dir" \
  AGENTDOCK_RUNTIME_ROOT="$runtime_root" \
  AGENTDOCK_LAUNCH_AGENTS_DIR="$launch_agents" \
  AGENTDOCK_INSTALLER_AGENTDOCK_HOME="$state_dir" \
  AGENTDOCK_INSTALLER_DEFAULT_DIR="$work_dir" \
  AGENTDOCK_NONINTERACTIVE=true \
  AGENTDOCK_TUNNEL_MODE=none \
    sh "$ROOT_DIR/scripts/install/install.sh" "$@"
}

assert_committed() {
  "$install_dir/agentdock" install inspect --state-root "$runtime_root" --require-committed >/dev/null
}

print -- '[macos] fresh'
run_install
assert_committed
[[ -x "$install_dir/agentdock" ]]
for skill in agentdock-user-guide skill-authoring skill-installation plugin-import; do
  [[ -f "$state_dir/skills/$skill/SKILL.md" ]]
done

print -- '[macos] repair'
run_install
assert_committed

mkdir -p "$state_dir" "$work_dir"
printf keep > "$state_dir/preserve-marker"
printf keep > "$work_dir/preserve-marker"
print -- '[macos] default uninstall preserves user state'
run_install --uninstall
[[ ! -e "$install_dir/agentdock" ]]
[[ -f "$state_dir/preserve-marker" ]]
[[ -f "$work_dir/preserve-marker" ]]

print -- '[macos] reinstall'
run_install
assert_committed
printf keep > "$state_dir/purge-marker"
printf keep > "$work_dir/purge-marker"
print -- '[macos] purge'
run_install --uninstall --purge-data --agentdock-home "$state_dir" --agentdock-default-dir "$work_dir"
[[ ! -e "$install_dir/agentdock" ]]
[[ ! -e "$runtime_root" ]]
[[ ! -e "$state_dir" ]]
[[ ! -e "$work_dir" ]]

print -- 'UNIFIED_MACOS_INSTALLER_TEST_PASS'
