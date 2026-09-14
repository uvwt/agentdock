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
  : >"$TEST_ENGINE_LOG"
  for arg in "$@"; do printf 'arg=%s\n' "$arg" >>"$TEST_ENGINE_LOG"; done
fi
# The public bootstrap only requires a successful structured Engine call here.
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
