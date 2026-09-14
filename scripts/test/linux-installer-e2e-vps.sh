#!/usr/bin/env bash
# AgentDock Linux Installer E2E：systemd + 非 root 用户 + passwordless sudo。
# 只使用 agentdock-e2e 命名空间和隔离目录；结束时校验 production 未变化。
set -Eeuo pipefail

ROOT=/tmp/agentdock-linux-e2e
INSTALLER_USER="$(id -un)"
E2E_USER=agentdock-e2e
INSTALL_ROOT=/opt/agentdock-e2e
RUNTIME_ROOT=/etc/agentdock-e2e
DATA_ROOT=/srv/agentdock-e2e
SERVICE_NAME=agentdock-e2e
PORT=18765
HTTP_PORT=18080

: "${AGENTDOCK_E2E_BUNDLE_URL:?AGENTDOCK_E2E_BUNDLE_URL is required}"
: "${AGENTDOCK_E2E_BUNDLE_SHA256:?AGENTDOCK_E2E_BUNDLE_SHA256 is required}"
: "${AGENTDOCK_E2E_TARBALL_URL:?AGENTDOCK_E2E_TARBALL_URL is required}"
: "${AGENTDOCK_E2E_SHA_URL:?AGENTDOCK_E2E_SHA_URL is required}"

log() { printf '[e2e] %s\n' "$*"; }
pass() { printf '[e2e][PASS] %s\n' "$*"; }
fail() { printf '[e2e][FAIL] %s\n' "$*" >&2; exit 1; }

HTTP_PID=""
cleanup() {
  set +e
  [[ -z "$HTTP_PID" ]] || kill "$HTTP_PID" >/dev/null 2>&1 || true
  sudo systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
  sudo rm -f "/etc/systemd/system/$SERVICE_NAME.service" "/etc/systemd/system/$SERVICE_NAME-cloudflared.service"
  sudo systemctl daemon-reload >/dev/null 2>&1 || true
  sudo rm -rf "$INSTALL_ROOT" "$RUNTIME_ROOT" "$DATA_ROOT"
  if id -u "$E2E_USER" >/dev/null 2>&1; then
    sudo userdel "$E2E_USER" >/dev/null 2>&1 || true
  fi
  sudo rm -rf "$ROOT"
}
trap cleanup EXIT

[[ "$(id -u)" -ne 0 ]] || fail "run this E2E as a non-root sudo user"
[[ "$INSTALLER_USER" != "$E2E_USER" ]] || fail "installer user and service user must be different"
! id -u "$E2E_USER" >/dev/null 2>&1 || fail "stale service user exists: $E2E_USER"
[[ ! -e "$INSTALL_ROOT" && ! -e "$RUNTIME_ROOT" && ! -e "$DATA_ROOT" ]] || fail "stale E2E paths exist; clean them before running"
sudo -n true >/dev/null 2>&1 || fail "passwordless sudo is required"
[[ -d /run/systemd/system ]] || fail "systemd is required"
command -v curl >/dev/null 2>&1 || fail "curl required"
command -v python3 >/dev/null 2>&1 || fail "python3 required"
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum required"
# ROOT 是本测试独占命名空间；清掉上一次异常中止留下的临时载荷。
sudo rm -rf "$ROOT"
mkdir -p "$ROOT/dist"

log "STEP 0: production baseline"
systemctl is-active agentdock >/dev/null || fail "production agentdock not active"
curl -sf -m 5 http://127.0.0.1:8765/healthz | grep -q '"ok":true' || fail "production healthz not ok"
sudo sha256sum /etc/agentdock/agentdock.env /opt/agentdock/bin/agentdock | tee "$ROOT/prod-baseline.txt" >/dev/null

log "STEP 1: fetch isolated installer bundle and release artifact"
curl -fsSL "$AGENTDOCK_E2E_BUNDLE_URL" -o "$ROOT/bundle.zip"
printf '%s  %s\n' "$AGENTDOCK_E2E_BUNDLE_SHA256" "$ROOT/bundle.zip" | sha256sum -c - >/dev/null || fail "bundle sha mismatch"
python3 -m zipfile -e "$ROOT/bundle.zip" "$ROOT"
[[ -f "$ROOT/scripts-install/install.sh" ]] || fail "install.sh missing from bundle"
[[ -f "$ROOT/scripts-install/install-linux-platform.sh" ]] || fail "install-linux-platform.sh missing from bundle"
[[ -f "$ROOT/scripts-install/uninstall-linux.sh" ]] || fail "uninstall-linux.sh missing from bundle"

curl -fsSL "$AGENTDOCK_E2E_TARBALL_URL" -o "$ROOT/dist/agentdock_linux_amd64.tar.gz"
curl -fsSL "$AGENTDOCK_E2E_SHA_URL" -o "$ROOT/dist/agentdock_linux_amd64.tar.gz.sha256"
(
  cd "$ROOT/dist"
  sha256sum -c agentdock_linux_amd64.tar.gz.sha256 >/dev/null
) || fail "release tarball sha mismatch"

log "STEP 2: serve release artifact locally"
python3 -m http.server "$HTTP_PORT" --bind 127.0.0.1 --directory "$ROOT/dist" >"$ROOT/http.log" 2>&1 &
HTTP_PID=$!
sleep 1
curl -sf -m 5 "http://127.0.0.1:$HTTP_PORT/agentdock_linux_amd64.tar.gz.sha256" >/dev/null || fail "local artifact server not serving"

run_e2e_install() {
  env \
    AGENTDOCK_RELEASE_BASE_URL="http://127.0.0.1:$HTTP_PORT" \
    AGENTDOCK_NONINTERACTIVE=true \
    AGENTDOCK_INSTALL_MODE=binary \
    AGENTDOCK_USE_LOCAL_PLATFORM_INSTALLER=true \
    AGENTDOCK_SOURCE_DIR="$INSTALL_ROOT" \
    AGENTDOCK_DATA_DIR="$DATA_ROOT" \
    AGENTDOCK_ENV_FILE="$RUNTIME_ROOT/agentdock.env" \
    AGENTDOCK_SERVICE_NAME="$SERVICE_NAME" \
    AGENTDOCK_SERVICE_USER="$E2E_USER" \
    AGENTDOCK_PORT="$PORT" \
    AGENTDOCK_TUNNEL_MODE=none \
    bash "$ROOT/scripts-install/install.sh"
}

run_e2e_uninstall() {
  env \
    AGENTDOCK_SOURCE_DIR="$INSTALL_ROOT" \
    AGENTDOCK_DATA_DIR="$DATA_ROOT" \
    AGENTDOCK_ENV_FILE="$RUNTIME_ROOT/agentdock.env" \
    AGENTDOCK_SERVICE_NAME="$SERVICE_NAME" \
    AGENTDOCK_SERVICE_USER="$E2E_USER" \
    bash "$ROOT/scripts-install/uninstall-linux.sh" "$@"
}

assert_ownership() {
  local bad owner mode
  bad="$(sudo find "$INSTALL_ROOT" -not -user root -print -quit)"
  [[ -z "$bad" ]] || fail "install tree contains non-root entry: $bad"
  [[ "$(stat -c '%U:%a' "$INSTALL_ROOT/bin/agentdock")" = "root:755" ]] || fail "live binary owner/mode invalid"

  owner="$(stat -c '%U' "$RUNTIME_ROOT")"
  mode="$(stat -c '%a' "$RUNTIME_ROOT")"
  [[ "$owner:$mode" = "$E2E_USER:700" ]] || fail "runtime root owner/mode unexpected: $owner:$mode"
  [[ "$(sudo stat -c '%U' "$RUNTIME_ROOT/agentdock.env")" = "$E2E_USER" ]] || fail "agentdock.env not owned by service user"
  [[ "$(sudo stat -c '%U' "$RUNTIME_ROOT/desktop-runtime.json")" = "$E2E_USER" ]] || fail "runtime manifest not owned by service user"
  [[ "$(sudo stat -c '%U:%a' "$RUNTIME_ROOT/install")" = "root:700" ]] || fail "installer state directory must remain root:700"
  bad="$(sudo find "$RUNTIME_ROOT/install" -not -user root -print -quit)"
  [[ -z "$bad" ]] || fail "installer journal contains non-root entry: $bad"

  bad="$(sudo find "$DATA_ROOT" -not -user "$E2E_USER" -print -quit)"
  [[ -z "$bad" ]] || fail "data tree contains non-service-user entry: $bad"
  sudo test -d "$DATA_ROOT/.agentdock/skill-store" || fail "skill store missing"
}

assert_committed_and_healthy() {
  systemctl is-active "$SERVICE_NAME" >/dev/null || fail "$SERVICE_NAME not active"
  curl -sf -m 5 "http://127.0.0.1:$PORT/healthz" | grep -q '"ok":true' || fail "e2e healthz not ok"
  sudo "$INSTALL_ROOT/bin/agentdock" install inspect --state-root "$RUNTIME_ROOT" --require-committed >/dev/null || fail "inspect not committed"
}

log "STEP 3: fresh install as non-root user with sudo bridge"
run_e2e_install || fail "fresh install failed"
assert_committed_and_healthy
assert_ownership
pass "fresh install committed + healthy + ownership correct"

log "STEP 4: same-version repair"
run_e2e_install || fail "repair failed"
assert_committed_and_healthy
assert_ownership
[[ -z "$(sudo find "$INSTALL_ROOT/versions" -maxdepth 1 \( -name '.repair-*' -o -name '.bootstrap-*' \) -print -quit)" ]] || fail "repair/bootstrap residue remains"
pass "same-version repair committed without staging residue"

log "STEP 5: services-only uninstall then reinstall"
run_e2e_uninstall --services-only || fail "services-only uninstall failed"
systemctl is-active "$SERVICE_NAME" >/dev/null 2>&1 && fail "service still active after uninstall"
run_e2e_install || fail "reinstall failed"
assert_committed_and_healthy
source_version="$(sudo python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_version"])' "$RUNTIME_ROOT/install/transaction.json")"
[[ -n "$source_version" && "$source_version" != unknown ]] || fail "reinstall source_version invalid: $source_version"
pass "reinstall committed, source_version=$source_version"

log "STEP 6: purge uninstall"
run_e2e_uninstall --purge-data || fail "purge uninstall failed"
[[ ! -e "$INSTALL_ROOT" ]] || fail "$INSTALL_ROOT remains after purge"
[[ ! -e "$RUNTIME_ROOT" ]] || fail "$RUNTIME_ROOT remains after purge"
pass "purge uninstall removed isolated install/runtime"

log "STEP 7: production baseline re-check"
systemctl is-active agentdock >/dev/null || fail "production agentdock not active after e2e"
curl -sf -m 5 http://127.0.0.1:8765/healthz | grep -q '"ok":true' || fail "production healthz not ok after e2e"
sudo sha256sum -c "$ROOT/prod-baseline.txt" >/dev/null || fail "production files changed"
pass "production unchanged"

echo "[e2e] ALL_LINUX_E2E_PASS"
