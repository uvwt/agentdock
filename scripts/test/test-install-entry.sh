#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-install-entry-test.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

ENTRY="$ROOT_DIR/scripts/install/install.sh"
RELEASE_DIR="$TMP_ROOT/release"
FAKE_BIN="$TMP_ROOT/bin"
mkdir -p "$RELEASE_DIR" "$FAKE_BIN"

export AGENTDOCK_TTY_IN=/dev/stdin
export AGENTDOCK_TTY_OUT=/dev/stderr

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_arg() {
  expected="$1"
  file="$2"
  grep -Fqx -- "arg=$expected" "$file" || fail "missing engine arg: $expected"
}
assert_no_arg() {
  unexpected="$1"
  file="$2"
  if grep -Fqx -- "arg=$unexpected" "$file"; then
    fail "unexpected engine arg: $unexpected"
  fi
}
checksum_asset() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$RELEASE_DIR" && sha256sum "$file" >"$file.sha256")
  else
    (cd "$RELEASE_DIR" && shasum -a 256 "$file" >"$file.sha256")
  fi
}

cat >"$FAKE_BIN/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
  -s) printf '%s\n' "${TEST_UNAME_S:?}" ;;
  -m) printf '%s\n' "${TEST_UNAME_M:-x86_64}" ;;
  *) printf '%s\n' "${TEST_UNAME_S:?}" ;;
esac
EOF
chmod +x "$FAKE_BIN/uname"

make_release() {
  platform="$1"
  arch="$2"
  payload="$TMP_ROOT/payload-$platform-$arch"
  rm -rf "$payload"
  mkdir -p "$payload/bin" "$payload/share/agentdock/core-skills/packages"
  cat >"$payload/bin/agentdock" <<'EOF'
#!/bin/sh
set -eu
if [ "${1:-}" = install ] && [ "${2:-}" = --engine-ready ]; then
  printf '%s\n' 'agentdock-installer-engine schema=1'
  exit 0
fi
if [ -n "${TEST_ENGINE_LOG:-}" ]; then
  : >>"$TEST_ENGINE_LOG"
  for arg in "$@"; do printf 'arg=%s\n' "$arg" >>"$TEST_ENGINE_LOG"; done
fi

if [ "${1:-}" = nexus ] && [ "${2:-}" = status ]; then
  if [ -f "${AGENTDOCK_HOME:-}/nexus/endpoint" ]; then
    endpoint="$(cat "${AGENTDOCK_HOME}/nexus/endpoint")"
    printf '{"paired":true,"endpoint":"%s","device_token_stored":true}\n' "$endpoint"
  else
    printf '%s\n' '{"paired":false,"device_token_stored":false}'
  fi
  exit 0
fi
if [ "${1:-}" = nexus ] && [ "${2:-}" = pair ]; then
  endpoint=""
  shift 2
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --endpoint) endpoint="$2"; shift 2 ;;
      --code) shift 2 ;;
      *) shift ;;
    esac
  done
  mkdir -p "${AGENTDOCK_HOME:?}/nexus"
  printf '%s\n' "$endpoint" >"${AGENTDOCK_HOME}/nexus/endpoint"
  printf '%s\n' 'legacy-pair-success-output'
  exit 0
fi
if [ "${1:-}" = service ] && [ "${2:-}" = restart ]; then
  exit 0
fi

install_root=""
runtime_root=""
tunnel_mode=""
server_url=""
rotate_oauth=false
previous=""
for arg in "$@"; do
  case "$previous" in
    install-root) install_root="$arg"; previous="" ;;
    runtime-root) runtime_root="$arg"; previous="" ;;
    tunnel-mode) tunnel_mode="$arg"; previous="" ;;
    server-url) server_url="$arg"; previous="" ;;
    *)
      case "$arg" in
        --install-root) previous=install-root ;;
        --runtime-root) previous=runtime-root ;;
        --tunnel-mode) previous=tunnel-mode ;;
        --server-url) previous=server-url ;;
        --rotate-oauth) rotate_oauth=true ;;
      esac
      ;;
  esac
done
if [ "${1:-}" = install ] && [ -n "$install_root" ] && [ -n "$runtime_root" ]; then
  mkdir -p "$install_root/bin" "$runtime_root"
  stable="$install_root/bin/agentdock"
  if [ "$0" != "$stable" ]; then cp "$0" "$stable"; fi
  chmod +x "$stable"
  cat >"$runtime_root/agentdock.env" <<ENV
AGENTDOCK_HOST=127.0.0.1
AGENTDOCK_PORT=8765
AGENTDOCK_AUTH_TOKEN=test-access-token
ENV
  if [ -n "$tunnel_mode" ] && [ "$tunnel_mode" != none ]; then
    printf 'AGENTDOCK_TUNNEL_MODE=%s\n' "$tunnel_mode" >"$runtime_root/cloudflared.env"
    if [ "$rotate_oauth" = true ]; then
      printf "%s\n" "AGENTDOCK_OAUTH_PASSWORD='test-oauth-password'" >>"$runtime_root/agentdock.env"
    fi
    if [ -n "$server_url" ]; then
      printf 'AGENTDOCK_SERVER_URL=%s\n' "$server_url" >>"$runtime_root/agentdock.env"
    fi
  else
    rm -f "$runtime_root/cloudflared.env"
  fi
fi
printf '%s\n' '{"schema_version":1,"state":"committed"}'
EOF
  chmod +x "$payload/bin/agentdock"
  printf '%s\n' '{"schema_version":1,"bundle_version":"test","skills":[]}' >"$payload/share/agentdock/core-skills/manifest.json"
  asset="agentdock_${platform}_${arch}.tar.gz"
  tar -czf "$RELEASE_DIR/$asset" -C "$payload" .
  checksum_asset "$asset"
}

make_release linux amd64
make_release darwin amd64

# The unified public entry must work with only the release tarball. No platform
# installer/uninstaller asset is created in RELEASE_DIR, so any hidden dependency fails.
LINUX_ROOT="$TMP_ROOT/linux"
mkdir -p "$LINUX_ROOT"
PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_ROOT/engine.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_ROOT/install" \
  AGENTDOCK_ENV_FILE="$LINUX_ROOT/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_ROOT/data" \
  AGENTDOCK_CLI_LINK_PATH="$LINUX_ROOT/bin/agentdock" \
  AGENTDOCK_CLOUDFLARED_INSTALL_PATH="$LINUX_ROOT/bin/cloudflared" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_TUNNEL_MODE=none \
  sh "$ENTRY"
assert_arg install "$LINUX_ROOT/engine.log"
assert_arg --install-root "$LINUX_ROOT/engine.log"
assert_arg "$LINUX_ROOT/install" "$LINUX_ROOT/engine.log"
assert_arg --runtime-root "$LINUX_ROOT/engine.log"
assert_arg "$LINUX_ROOT/runtime" "$LINUX_ROOT/engine.log"
assert_arg --agentdock-home "$LINUX_ROOT/engine.log"
assert_arg "$LINUX_ROOT/data/.agentdock" "$LINUX_ROOT/engine.log"
assert_arg --agentdock-default-dir "$LINUX_ROOT/engine.log"
assert_arg "$LINUX_ROOT/data/AgentDock" "$LINUX_ROOT/engine.log"
[ -L "$LINUX_ROOT/bin/agentdock" ] || fail "Linux install did not expose agentdock through the CLI link"
[ "$(readlink "$LINUX_ROOT/bin/agentdock")" = "$LINUX_ROOT/install/bin/agentdock" ] || fail "Linux CLI link points to the wrong binary"
[ ! -e "$LINUX_ROOT/bin/cloudflared" ] || fail "local-only install unexpectedly installed cloudflared"

# Fresh installs pair Nexus only after Core is installed. Official mode owns the
# endpoint and deliberately omits --name so AgentDock can use its hostname default.
LINUX_NEXUS="$TMP_ROOT/linux-nexus"
mkdir -p "$LINUX_NEXUS"
PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_NEXUS/engine.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_NEXUS/install" \
  AGENTDOCK_ENV_FILE="$LINUX_NEXUS/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_NEXUS/data" \
  AGENTDOCK_CLI_LINK_PATH="$LINUX_NEXUS/bin/agentdock" \
  AGENTDOCK_CLOUDFLARED_INSTALL_PATH="$LINUX_NEXUS/bin/cloudflared" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_TUNNEL_MODE=none \
  AGENTDOCK_NEXUS_MODE=official AGENTDOCK_NEXUS_PAIR_CODE=test-pair-code \
  sh "$ENTRY" >"$LINUX_NEXUS/output.log" 2>&1
assert_arg https://mcp.nexusdock.co "$LINUX_NEXUS/engine.log"
assert_no_arg --name "$LINUX_NEXUS/engine.log"
core_line="$(grep -n '^arg=install$' "$LINUX_NEXUS/engine.log" | head -1 | cut -d: -f1)"
pair_line="$(grep -n '^arg=nexus$' "$LINUX_NEXUS/engine.log" | head -1 | cut -d: -f1)"
if [ -z "$core_line" ] || [ -z "$pair_line" ] || [ "$core_line" -ge "$pair_line" ]; then
  fail "Nexus pairing ran before Core install"
fi
grep -Fq 'NexusDock：https://mcp.nexusdock.co' "$LINUX_NEXUS/output.log" || fail "final summary missing official Nexus endpoint"
if grep -Fq 'legacy-pair-success-output' "$LINUX_NEXUS/output.log"; then
  fail "installer leaked nexus pair success output"
fi
[ ! -e "$LINUX_NEXUS/bin/cloudflared" ] || fail "Nexus-only install unexpectedly installed cloudflared"
[ ! -e "$LINUX_NEXUS/runtime/.installer-onboarding" ] || fail "completed install left onboarding recovery state behind"

# Interrupted onboarding restarts the full first-install flow even though the stable
# binary already exists. First run commits Core, then intentionally fails because
# non-interactive official Nexus pairing has no code. The second run must install
# Core again, then continue through Nexus and Tunnel from the beginning.
LINUX_RESUME="$TMP_ROOT/linux-resume"
mkdir -p "$LINUX_RESUME"
if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_RESUME/first-engine.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_RESUME/install" \
  AGENTDOCK_ENV_FILE="$LINUX_RESUME/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_RESUME/data" \
  AGENTDOCK_CLI_LINK_PATH="$LINUX_RESUME/bin/agentdock" \
  AGENTDOCK_CLOUDFLARED_INSTALL_PATH="$LINUX_RESUME/bin/cloudflared" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_TUNNEL_MODE=none \
  AGENTDOCK_NEXUS_MODE=official \
  sh "$ENTRY" >"$LINUX_RESUME/first-output.log" 2>&1; then
  fail "interrupted onboarding fixture unexpectedly completed"
fi
[ -x "$LINUX_RESUME/install/bin/agentdock" ] || fail "interrupted onboarding did not commit Core"
[ "$(cat "$LINUX_RESUME/runtime/.installer-onboarding")" = nexus ] || fail "interrupted onboarding did not persist nexus stage"

PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_RESUME/resume-engine.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_RESUME/install" \
  AGENTDOCK_ENV_FILE="$LINUX_RESUME/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_RESUME/data" \
  AGENTDOCK_CLI_LINK_PATH="$LINUX_RESUME/bin/agentdock" \
  AGENTDOCK_CLOUDFLARED_INSTALL_PATH="$LINUX_RESUME/bin/cloudflared" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_TUNNEL_MODE=none \
  AGENTDOCK_NEXUS_MODE=official AGENTDOCK_NEXUS_PAIR_CODE=resume-pair-code \
  sh "$ENTRY" >"$LINUX_RESUME/resume-output.log" 2>&1
assert_arg nexus "$LINUX_RESUME/resume-engine.log"
assert_arg install "$LINUX_RESUME/resume-engine.log"
grep -Fq '检测到上次安装未完成，重新开始安装流程。' "$LINUX_RESUME/resume-output.log" || fail "restart path did not report full onboarding restart"
grep -Fq 'NexusDock：https://mcp.nexusdock.co' "$LINUX_RESUME/resume-output.log" || fail "restarted install did not finish Nexus pairing"
[ ! -e "$LINUX_RESUME/runtime/.installer-onboarding" ] || fail "restarted install did not clear onboarding state"
[ ! -e "$LINUX_RESUME/bin/cloudflared" ] || fail "restarted no-tunnel install unexpectedly installed cloudflared"

# Cloudflare is a post-Core phase. Selecting Named Tunnel installs cloudflared,
# repairs the runtime, pre-generates OAuth credentials, and prints public details.
cat >"$FAKE_BIN/cloudflared" <<'EOF'
#!/bin/sh
if [ "${1:-}" = --version ]; then
  printf '%s\n' 'cloudflared version test'
  exit 0
fi
exit 0
EOF
chmod +x "$FAKE_BIN/cloudflared"

LINUX_CF="$TMP_ROOT/linux-cloudflare"
mkdir -p "$LINUX_CF"
PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_CF/engine.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_CF/install" \
  AGENTDOCK_ENV_FILE="$LINUX_CF/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_CF/data" \
  AGENTDOCK_CLI_LINK_PATH="$LINUX_CF/bin/agentdock" \
  AGENTDOCK_CLOUDFLARED_INSTALL_PATH="$LINUX_CF/bin/cloudflared" \
  AGENTDOCK_CLOUDFLARED_BINARY="$FAKE_BIN/cloudflared" \
  AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN=test-tunnel-token \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_NEXUS_MODE=none \
  AGENTDOCK_TUNNEL_MODE=named AGENTDOCK_SERVER_URL=https://agent.example.test \
  sh "$ENTRY" >"$LINUX_CF/output.log" 2>&1
[ -x "$LINUX_CF/bin/cloudflared" ] || fail "Named Tunnel did not install cloudflared"
assert_arg --repair "$LINUX_CF/engine.log"
assert_arg --rotate-oauth "$LINUX_CF/engine.log"
assert_arg named "$LINUX_CF/engine.log"
grep -Fq '公网 MCP：https://agent.example.test/mcp' "$LINUX_CF/output.log" || fail "Named Tunnel summary missing public MCP URL"
grep -Fq 'OAuth 密码：test-oauth-password' "$LINUX_CF/output.log" || fail "Named Tunnel summary missing OAuth password"

# A checksum mismatch must fail before the Engine executes.
cp "$RELEASE_DIR/agentdock_linux_amd64.tar.gz.sha256" "$TMP_ROOT/linux.sha.good"
printf '%064d  agentdock_linux_amd64.tar.gz\n' 0 >"$RELEASE_DIR/agentdock_linux_amd64.tar.gz.sha256"
if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_ROOT/should-not-run.log" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_ROOT/bad-install" \
  AGENTDOCK_ENV_FILE="$LINUX_ROOT/bad-runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_ROOT/bad-data" \
  AGENTDOCK_INSTALLER_BASE_URL="file://$RELEASE_DIR" \
  AGENTDOCK_NONINTERACTIVE=true AGENTDOCK_TUNNEL_MODE=none \
  sh "$ENTRY" >/dev/null 2>"$LINUX_ROOT/checksum.err"; then
  fail "checksum mismatch was accepted"
fi
grep -Fq 'SHA-256 校验失败' "$LINUX_ROOT/checksum.err" || fail "checksum failure was not explicit"
[ ! -e "$LINUX_ROOT/should-not-run.log" ] || fail "Engine ran after checksum failure"
mv "$TMP_ROOT/linux.sha.good" "$RELEASE_DIR/agentdock_linux_amd64.tar.gz.sha256"

# Linux purge-data forwards deterministic user-data paths to Engine. The shell no
# longer recursively removes state based on ambient AGENTDOCK_HOME.
LINUX_UNINSTALL="$TMP_ROOT/linux-uninstall"
mkdir -p "$LINUX_UNINSTALL/install/bin" "$LINUX_UNINSTALL/runtime" "$LINUX_UNINSTALL/data/.agentdock" "$LINUX_UNINSTALL/data/AgentDock"
cp "$TMP_ROOT/payload-linux-amd64/bin/agentdock" "$LINUX_UNINSTALL/install/bin/agentdock"
PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Linux TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$LINUX_UNINSTALL/engine.log" \
  AGENTDOCK_HOME="$TMP_ROOT/host-production-home" \
  AGENTDOCK_NO_SUDO=true AGENTDOCK_SERVICE_MANAGER=none \
  AGENTDOCK_SOURCE_DIR="$LINUX_UNINSTALL/install" \
  AGENTDOCK_ENV_FILE="$LINUX_UNINSTALL/runtime/agentdock.env" \
  AGENTDOCK_DATA_DIR="$LINUX_UNINSTALL/data" \
  sh "$ENTRY" --uninstall --purge-data
assert_arg uninstall "$LINUX_UNINSTALL/engine.log"
assert_arg --purge-data "$LINUX_UNINSTALL/engine.log"
assert_arg "$LINUX_UNINSTALL/data/.agentdock" "$LINUX_UNINSTALL/engine.log"
assert_arg "$LINUX_UNINSTALL/data/AgentDock" "$LINUX_UNINSTALL/engine.log"
assert_no_arg "$TMP_ROOT/host-production-home" "$LINUX_UNINSTALL/engine.log"

# macOS custom/isolation layouts must not turn inherited runtime env into delete
# targets. Without explicit installer cleanup paths, purge-data is rejected.
MAC_ROOT="$TMP_ROOT/macos"
HOST_STATE="$TMP_ROOT/host-production/.agentdock"
HOST_WORK="$TMP_ROOT/host-production/AgentDock"
mkdir -p "$MAC_ROOT/bin" "$MAC_ROOT/runtime" "$HOST_STATE" "$HOST_WORK"
printf 'keep\n' >"$HOST_STATE/must-survive"
cp "$TMP_ROOT/payload-darwin-amd64/bin/agentdock" "$MAC_ROOT/bin/agentdock"

# Ordinary uninstall also recursively removes runtime/log/app paths. A custom
# runtime accidentally set to the whole HOME must fail before the Engine runs.
if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Darwin TEST_UNAME_M=x86_64 \
  AGENTDOCK_HOME="$HOST_STATE" AGENTDOCK_DEFAULT_DIR="$HOST_WORK" \
  AGENTDOCK_INSTALL_DIR="$MAC_ROOT/bin" AGENTDOCK_RUNTIME_ROOT="$HOME" \
  sh "$ENTRY" --uninstall >/dev/null 2>"$MAC_ROOT/runtime-guard.err"; then
  fail "macOS uninstall accepted the whole user HOME as runtime root"
fi
grep -Fq '不能是整个用户主目录' "$MAC_ROOT/runtime-guard.err" || fail "macOS runtime-root guard error missing"
grep -Fqx 'keep' "$HOST_STATE/must-survive" || fail "macOS runtime-root guard touched ambient production state"

# An explicitly managed App path must still identify an .app bundle.
mkdir -p "$MAC_ROOT/not-an-app"
if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Darwin TEST_UNAME_M=x86_64 \
  AGENTDOCK_HOME="$HOST_STATE" AGENTDOCK_DEFAULT_DIR="$HOST_WORK" \
  AGENTDOCK_INSTALL_DIR="$MAC_ROOT/bin" AGENTDOCK_RUNTIME_ROOT="$MAC_ROOT/runtime" \
  AGENTDOCK_APP_PATH="$MAC_ROOT/not-an-app" \
  sh "$ENTRY" --uninstall >/dev/null 2>"$MAC_ROOT/app-guard.err"; then
  fail "macOS uninstall accepted a non-.app recursive delete target"
fi
grep -Fq '必须指向 .app 应用目录' "$MAC_ROOT/app-guard.err" || fail "macOS app-path guard error missing"

if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Darwin TEST_UNAME_M=x86_64 \
  AGENTDOCK_HOME="$HOST_STATE" AGENTDOCK_DEFAULT_DIR="$HOST_WORK" \
  AGENTDOCK_INSTALL_DIR="$MAC_ROOT/bin" AGENTDOCK_RUNTIME_ROOT="$MAC_ROOT/runtime" \
  AGENTDOCK_APP_PATH="$MAC_ROOT/AgentDock.app" \
  sh "$ENTRY" --uninstall --purge-data >/dev/null 2>"$MAC_ROOT/guard.err"; then
  fail "macOS purge-data accepted inherited runtime cleanup targets"
fi
grep -Fq '必须显式提供 --agentdock-home' "$MAC_ROOT/guard.err" || fail "macOS purge guard error missing"
grep -Fqx 'keep' "$HOST_STATE/must-survive" || fail "macOS purge guard touched ambient production state"

# Even explicit cleanup flags may not name the whole user HOME. This check runs
# before the Engine or any rm -rf, so a typo cannot turn into account-wide deletion.
SAFE_WORK="$MAC_ROOT/safe-work"
mkdir -p "$SAFE_WORK"
if PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Darwin TEST_UNAME_M=x86_64 \
  AGENTDOCK_HOME="$HOST_STATE" AGENTDOCK_DEFAULT_DIR="$HOST_WORK" \
  AGENTDOCK_INSTALL_DIR="$MAC_ROOT/bin" AGENTDOCK_RUNTIME_ROOT="$MAC_ROOT/runtime" \
  AGENTDOCK_APP_PATH="$MAC_ROOT/AgentDock.app" \
  sh "$ENTRY" --uninstall --purge-data \
    --agentdock-home "$HOME" --agentdock-default-dir "$SAFE_WORK" >/dev/null 2>"$MAC_ROOT/home-guard.err"; then
  fail "macOS purge-data accepted the whole user HOME as a cleanup target"
fi
grep -Fq '不能是整个用户主目录' "$MAC_ROOT/home-guard.err" || fail "macOS whole-home purge guard error missing"
grep -Fqx 'keep' "$HOST_STATE/must-survive" || fail "macOS whole-home guard touched ambient production state"

# Explicit isolated cleanup targets are accepted and ambient production state survives.
ISO_STATE="$MAC_ROOT/isolated/.agentdock"
ISO_WORK="$MAC_ROOT/isolated/AgentDock"
mkdir -p "$ISO_STATE" "$ISO_WORK"
printf 'state\n' >"$ISO_STATE/test"
printf 'work\n' >"$ISO_WORK/test"
PATH="$FAKE_BIN:$PATH" \
  TEST_UNAME_S=Darwin TEST_UNAME_M=x86_64 \
  TEST_ENGINE_LOG="$MAC_ROOT/engine.log" \
  AGENTDOCK_HOME="$HOST_STATE" AGENTDOCK_DEFAULT_DIR="$HOST_WORK" \
  AGENTDOCK_INSTALL_DIR="$MAC_ROOT/bin" AGENTDOCK_RUNTIME_ROOT="$MAC_ROOT/runtime" \
  AGENTDOCK_APP_PATH="$MAC_ROOT/AgentDock.app" \
  sh "$ENTRY" --uninstall --purge-data \
    --agentdock-home "$ISO_STATE" --agentdock-default-dir "$ISO_WORK"
[ ! -e "$ISO_STATE" ] || fail "explicit isolated state was not removed"
[ ! -e "$ISO_WORK" ] || fail "explicit isolated workspace was not removed"
grep -Fqx 'keep' "$HOST_STATE/must-survive" || fail "explicit macOS purge touched ambient production state"
assert_arg --purge-config "$MAC_ROOT/engine.log"
assert_no_arg --purge-data "$MAC_ROOT/engine.log"
assert_arg "$ISO_STATE" "$MAC_ROOT/engine.log"
assert_arg "$ISO_WORK" "$MAC_ROOT/engine.log"

printf '%s\n' 'unified installer entry tests passed'
