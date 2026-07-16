#!/bin/zsh
set -euo pipefail

ROOT_DIR="${0:A:h:h}"
TEST_PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
TMP_ROOT="$(PATH="$TEST_PATH" mktemp -d "${TMPDIR:-/tmp}/agentdock-macos-installer-test.XXXXXX")"
trap 'PATH="$TEST_PATH" rm -rf "$TMP_ROOT"' EXIT

case "$(uname -m)" in
  arm64|aarch64) release_arch="arm64" ;;
  x86_64|amd64) release_arch="amd64" ;;
  *) print -u2 -- "unsupported test architecture: $(uname -m)"; exit 1 ;;
esac

release_dir="$TMP_ROOT/release files"
build_dir="$TMP_ROOT/build files"
home_dir="$TMP_ROOT/home with spaces"
asset="agentdock_darwin_${release_arch}.tar.gz"
mkdir -p "$release_dir" "$build_dir/bin" "$home_dir"
release_url="$(python3 - "$release_dir" <<'PYURI'
from pathlib import Path
import sys
print(Path(sys.argv[1]).resolve().as_uri())
PYURI
)"

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
  env -i \
    HOME="$home_dir" \
    PATH="$TEST_PATH" \
    TMPDIR="$TMP_ROOT" \
    AGENTDOCK_RELEASE_BASE_URL="$release_url" \
    zsh "$ROOT_DIR/scripts/install-macos.sh" "$@"
}

mode_of() {
  stat -f '%Lp' "$1"
}

assert_file_contains() {
  local file_path="$1"
  local text="$2"
  grep -Fq -- "$text" "$file_path" || {
    print -u2 -- "missing text in $file_path: $text"
    exit 1
  }
}

assert_file_not_contains() {
  local file_path="$1"
  local text="$2"
  if grep -Fq -- "$text" "$file_path"; then
    print -u2 -- "unexpected text in $file_path: $text"
    exit 1
  fi
}

count_env_key() {
  local file_path="$1"
  local key="$2"
  grep -Ec "^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*=" "$file_path" || true
}

sha256_of() {
  shasum -a 256 "$1" | awk '{print $1}'
}

binary="$home_dir/.local/bin/agentdock"
state_dir="$home_dir/.agentdock"
backup_dir="$state_dir/backups/bin"
work_dir="$home_dir/AgentDock"
app_support="$home_dir/Library/Application Support/AgentDock"
agentdock_env="$app_support/agentdock.env"
start_script="$app_support/start-agentdock.sh"
plist="$home_dir/Library/LaunchAgents/com.uvwt.agentdock.plist"
log_dir="$home_dir/Library/Logs/AgentDock"

# 全新安装使用 --no-start，只生成标准服务文件，不接触当前用户真实 LaunchAgent。
run_installer \
  --version latest \
  --register-service \
  --no-start \
  --host 127.0.0.1 \
  --port 18766 \
  --auth-token 'initial token with spaces'

test -x "$binary"
"$binary" --help >/dev/null 2>&1
test -d "$state_dir"
test -d "$backup_dir"
test -d "$work_dir"
test -d "$app_support"
test -d "$log_dir"
test "$(mode_of "$app_support")" = "700"
test "$(mode_of "$agentdock_env")" = "600"
test "$(mode_of "$start_script")" = "700"
test "$(mode_of "$log_dir")" = "700"
test "$(mode_of "$log_dir/agentdock.out.log")" = "600"
test "$(mode_of "$log_dir/agentdock.err.log")" = "600"

assert_file_contains "$agentdock_env" 'AGENTDOCK_HOST=127.0.0.1'
assert_file_contains "$agentdock_env" 'AGENTDOCK_PORT=18766'
assert_file_contains "$agentdock_env" 'AGENTDOCK_AUTH_TOKEN=initial\ token\ with\ spaces'
assert_file_contains "$start_script" 'USER_HOME="$HOME"'
assert_file_contains "$start_script" 'export HOME="$USER_HOME"'
assert_file_contains "$start_script" 'exec "$USER_HOME/.local/bin/agentdock"'
assert_file_contains "$start_script" 'source "$AGENTDOCK_ENV"'
plutil -lint "$plist" >/dev/null
test "$(plutil -extract ProgramArguments.0 raw -o - "$plist")" = "$start_script"
test "$(plutil -extract WorkingDirectory raw -o - "$plist")" = "$work_dir"
test "$(plutil -extract StandardOutPath raw -o - "$plist")" = "$log_dir/agentdock.out.log"
test "$(plutil -extract StandardErrorPath raw -o - "$plist")" = "$log_dir/agentdock.err.log"

# 模拟用户维护已有 Nexus 配置，重复安装不得覆盖它和既有 Token。
python3 - "$agentdock_env" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
text = path.read_text()
text = text.replace("AGENTDOCK_NEXUS_ENDPOINT=''", "AGENTDOCK_NEXUS_ENDPOINT=https://nexus.example.test")
path.write_text(text)
PY

run_installer \
  --version latest \
  --register-service \
  --no-start \
  --host 127.0.0.2 \
  --port 18888 \
  --auth-token 'replacement token must be ignored'

assert_file_contains "$agentdock_env" 'AGENTDOCK_HOST=127.0.0.2'
assert_file_contains "$agentdock_env" 'AGENTDOCK_PORT=18888'
assert_file_contains "$agentdock_env" 'AGENTDOCK_AUTH_TOKEN=initial\ token\ with\ spaces'
assert_file_not_contains "$agentdock_env" 'replacement token must be ignored'
assert_file_contains "$agentdock_env" 'AGENTDOCK_NEXUS_ENDPOINT=https://nexus.example.test'
backup_count="$(find "$backup_dir" -type f -name 'agentdock.*' | wc -l | tr -d ' ')"
test "$backup_count" = "1"

# 兼容已有的 `export KEY=...` 写法；未显式传值时不得追加默认值覆盖用户配置。
python3 - "$agentdock_env" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
text = path.read_text()
for key in ("AGENTDOCK_HOST", "AGENTDOCK_PORT", "AGENTDOCK_AUTH_TOKEN"):
    text = text.replace(f"{key}=", f"export {key}=", 1)
path.write_text(text)
PY
run_installer --version latest --register-service --no-start
assert_file_contains "$agentdock_env" 'export AGENTDOCK_HOST=127.0.0.2'
assert_file_contains "$agentdock_env" 'export AGENTDOCK_PORT=18888'
assert_file_contains "$agentdock_env" 'export AGENTDOCK_AUTH_TOKEN=initial\ token\ with\ spaces'
test "$(count_env_key "$agentdock_env" AGENTDOCK_HOST)" = "1"
test "$(count_env_key "$agentdock_env" AGENTDOCK_PORT)" = "1"
test "$(count_env_key "$agentdock_env" AGENTDOCK_AUTH_TOKEN)" = "1"
backup_count="$(find "$backup_dir" -type f -name 'agentdock.*' | wc -l | tr -d ' ')"
test "$backup_count" = "2"

# 清理旧版本可能遗留的重复定义时，保留最后一条定义，维持原有 source 语义。
printf '%s\n' 'AGENTDOCK_HOST=127.0.0.7' >> "$agentdock_env"
run_installer --version latest --register-service --no-start
assert_file_contains "$agentdock_env" 'AGENTDOCK_HOST=127.0.0.7'
test "$(count_env_key "$agentdock_env" AGENTDOCK_HOST)" = "1"
backup_count="$(find "$backup_dir" -type f -name 'agentdock.*' | wc -l | tr -d ' ')"
test "$backup_count" = "3"

# 注册服务必须坚持标准二进制目标，不能把 plist 指向一处、二进制装到另一处。
if run_installer --register-service --no-start --install-dir "$TMP_ROOT/nonstandard" >/dev/null 2>&1; then
  print -u2 -- "installer accepted a non-standard service binary path"
  exit 1
fi

# 目标路径若是目录或符号链接，必须在下载和替换前拒绝。
invalid_home="$TMP_ROOT/invalid target home"
mkdir -p "$invalid_home/.local/bin/agentdock"
if env -i HOME="$invalid_home" PATH="$TEST_PATH" AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" >/dev/null 2>&1; then
  print -u2 -- "installer accepted a non-regular binary target"
  exit 1
fi
test -d "$invalid_home/.local/bin/agentdock"

# agentdock.env 不得通过 HOME/PATH 把启动脚本重定向到其他用户目录或命令路径。
cat >> "$agentdock_env" <<'ENV'
HOME=/tmp/agentdock-evil-home
PATH=/tmp/agentdock-evil-path
ENV
cat > "$binary" <<'SCRIPT'
#!/bin/zsh
printf 'HOME=%s\nPATH=%s\nARGS=%s\n' "$HOME" "$PATH" "$*"
SCRIPT
chmod 0755 "$binary"
startup_output="$(env -i HOME="$home_dir" PATH="$TEST_PATH" /bin/zsh "$start_script")"
assert_file_contains <(print -r -- "$startup_output") "HOME=$home_dir"
assert_file_contains <(print -r -- "$startup_output") "PATH=$home_dir/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
assert_file_contains <(print -r -- "$startup_output") 'ARGS=--host 127.0.0.7 --port 18888 --log-level info'

# 用完全隔离的 launchctl/lsof/ps/curl 替身验证 bootstrap、kickstart 和新 PID 检查。
service_home="$TMP_ROOT/service home with spaces"
fake_bin="$TMP_ROOT/fake bin"
fake_state="$TMP_ROOT/fake launchctl state"
real_curl="$(command -v curl)"
mkdir -p "$service_home" "$fake_bin" "$fake_state"

cat > "$fake_bin/launchctl" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
print -r -- "$*" >> "$TEST_LAUNCHCTL_STATE/calls.log"
case "$1" in
  print)
    [[ -f "$TEST_LAUNCHCTL_STATE/loaded" || -f "$TEST_LAUNCHCTL_STATE/pid" ]] || exit 1
    if [[ -f "$TEST_LAUNCHCTL_STATE/pid" ]]; then
      print -- "  pid = $(cat "$TEST_LAUNCHCTL_STATE/pid")"
    else
      print -- "  state = waiting"
    fi
    ;;
  bootout)
    [[ ! -f "$TEST_LAUNCHCTL_STATE/fail-bootout" ]] || exit 1
    rm -f "$TEST_LAUNCHCTL_STATE/pid" "$TEST_LAUNCHCTL_STATE/loaded"
    ;;
  bootstrap)
    if [[ -f "$TEST_LAUNCHCTL_STATE/fail-bootstrap-once" ]]; then
      rm -f "$TEST_LAUNCHCTL_STATE/fail-bootstrap-once"
      exit 1
    fi
    : > "$TEST_LAUNCHCTL_STATE/loaded"
    ;;
  kickstart)
    if [[ -f "$TEST_LAUNCHCTL_STATE/fail-kickstart-once" ]]; then
      rm -f "$TEST_LAUNCHCTL_STATE/fail-kickstart-once"
      exit 1
    fi
    : > "$TEST_LAUNCHCTL_STATE/loaded"
    current=41000
    [[ ! -f "$TEST_LAUNCHCTL_STATE/last_pid" ]] || current="$(cat "$TEST_LAUNCHCTL_STATE/last_pid")"
    next=$(( current + 1 ))
    print -r -- "$next" > "$TEST_LAUNCHCTL_STATE/last_pid"
    print -r -- "$next" > "$TEST_LAUNCHCTL_STATE/pid"
    ;;
  *) exit 2 ;;
esac
SCRIPT

cat > "$fake_bin/lsof" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
cat "$TEST_LAUNCHCTL_STATE/pid"
SCRIPT

cat > "$fake_bin/ps" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
print -- "$HOME/.local/bin/agentdock --host 127.0.0.1 --port 18767"
SCRIPT

cat > "$fake_bin/curl" <<SCRIPT
#!/bin/zsh
set -euo pipefail
print -r -- "\$*" >> "\$TEST_LAUNCHCTL_STATE/curl.calls"
for arg in "\$@"; do
  if [[ "\$arg" == http://*'/healthz' ]]; then
    version="\$("\$HOME/.local/bin/agentdock" --version | sed -n '1s/^AgentDock[[:space:]][[:space:]]*//p')"
    printf '{"ok":true,"version":"%s"}\n' "\$version"
    exit 0
  fi
done
exec "$real_curl" "\$@"
SCRIPT
chmod 0755 "$fake_bin/launchctl" "$fake_bin/lsof" "$fake_bin/ps" "$fake_bin/curl"

# 首次安装在 bootstrap 后、kickstart 前失败时，必须卸载部分加载的任务并删除新文件。
partial_home="$TMP_ROOT/partial service home"
mkdir -p "$partial_home"
: > "$fake_state/fail-kickstart-once"
if env -i \
  HOME="$partial_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" \
    --register-service \
    --port 18767 >/dev/null 2>&1; then
  print -u2 -- "installer unexpectedly succeeded after simulated first-install kickstart failure"
  exit 1
fi
test ! -e "$fake_state/fail-kickstart-once"
test ! -e "$fake_state/loaded"
test ! -e "$fake_state/pid"
test ! -e "$partial_home/.local/bin/agentdock"
test ! -e "$partial_home/Library/Application Support/AgentDock/agentdock.env"
test ! -e "$partial_home/Library/Application Support/AgentDock/start-agentdock.sh"
test ! -e "$partial_home/Library/LaunchAgents/com.uvwt.agentdock.plist"

env -i \
  HOME="$service_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" \
    --register-service \
    --port 18767 \
    --auth-token test-token

assert_file_contains "$fake_state/calls.log" 'bootout gui/'
assert_file_contains "$fake_state/calls.log" 'bootstrap gui/'
assert_file_contains "$fake_state/calls.log" 'kickstart -k gui/'
test -f "$fake_state/pid"

# 已有自定义端口时，未显式传 host/port 的升级必须按最终 env 验证，不能回退到 8765。
: > "$fake_state/curl.calls"
env -i \
  HOME="$service_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" \
    --register-service >/dev/null
assert_file_contains "$fake_state/curl.calls" 'http://127.0.0.1:18767/healthz'
assert_file_not_contains "$fake_state/curl.calls" 'http://127.0.0.1:8765/healthz'
test -f "$fake_state/pid"

# 已有服务升级时，bootstrap 失败必须恢复旧二进制、env、启动脚本、plist 和旧 LaunchAgent。
rollback_release_dir="$TMP_ROOT/rollback release"
rollback_build_dir="$TMP_ROOT/rollback build"
mkdir -p "$rollback_release_dir" "$rollback_build_dir/bin"
cat > "$rollback_build_dir/bin/agentdock" <<'SCRIPT'
#!/bin/zsh
case "${1:-}" in
  --version)
    print -- "AgentDock v9.9.9"
    print -- "commit: rollback-test"
    ;;
  --help) ;;
  *) ;;
esac
SCRIPT
chmod 0755 "$rollback_build_dir/bin/agentdock"
tar -C "$rollback_build_dir" -czf "$rollback_release_dir/$asset" bin/agentdock
(
  cd "$rollback_release_dir"
  shasum -a 256 "$asset" > "$asset.sha256"
)
rollback_release_url="$(python3 - "$rollback_release_dir" <<'PYURI'
from pathlib import Path
import sys
print(Path(sys.argv[1]).resolve().as_uri())
PYURI
)"
service_binary="$service_home/.local/bin/agentdock"
service_env="$service_home/Library/Application Support/AgentDock/agentdock.env"
service_start="$service_home/Library/Application Support/AgentDock/start-agentdock.sh"
service_plist="$service_home/Library/LaunchAgents/com.uvwt.agentdock.plist"
old_binary_sha="$(sha256_of "$service_binary")"
old_env_sha="$(sha256_of "$service_env")"
old_start_sha="$(sha256_of "$service_start")"
old_plist_sha="$(sha256_of "$service_plist")"
: > "$fake_state/fail-bootstrap-once"
if env -i \
  HOME="$service_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$rollback_release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" \
    --register-service \
    --host 127.0.0.8 \
    --port 18888 >/dev/null 2>&1; then
  print -u2 -- "installer unexpectedly succeeded after simulated bootstrap failure"
  exit 1
fi
test ! -e "$fake_state/fail-bootstrap-once"
test -f "$fake_state/pid"
test "$(sha256_of "$service_binary")" = "$old_binary_sha"
test "$(sha256_of "$service_env")" = "$old_env_sha"
test "$(sha256_of "$service_start")" = "$old_start_sha"
test "$(sha256_of "$service_plist")" = "$old_plist_sha"
assert_file_contains "$fake_state/curl.calls" 'http://127.0.0.1:18767/healthz'
assert_file_not_contains "$fake_state/curl.calls" 'http://127.0.0.8:18888/healthz'

# 升级一开始就无法 bootout 时，旧服务仍在运行；回滚不得再次中断它。
old_pid="$(cat "$fake_state/pid")"
: > "$fake_state/curl.calls"
: > "$fake_state/fail-bootout"
if env -i \
  HOME="$service_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$rollback_release_url" \
  zsh "$ROOT_DIR/scripts/install-macos.sh" \
    --register-service \
    --host 127.0.0.8 \
    --port 18888 >/dev/null 2>&1; then
  print -u2 -- "installer unexpectedly succeeded after simulated bootout failure"
  exit 1
fi
test "$(cat "$fake_state/pid")" = "$old_pid"
test "$(sha256_of "$service_binary")" = "$old_binary_sha"
test "$(sha256_of "$service_env")" = "$old_env_sha"
test "$(sha256_of "$service_start")" = "$old_start_sha"
test "$(sha256_of "$service_plist")" = "$old_plist_sha"
assert_file_contains "$fake_state/curl.calls" 'http://127.0.0.1:18767/healthz'
assert_file_not_contains "$fake_state/curl.calls" 'http://127.0.0.8:18888/healthz'

# 无法停止已加载服务时，卸载器必须保留全部运行文件。
if env -i HOME="$service_home" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/uninstall-macos.sh" >/dev/null 2>&1; then
  print -u2 -- "uninstaller ignored a launchctl bootout failure"
  exit 1
fi
test -f "$fake_state/pid"
test -x "$service_binary"
test -f "$service_env"
test -x "$service_start"
test -f "$service_plist"
rm -f "$fake_state/fail-bootout"

# 默认卸载只删服务，保留二进制、状态和工作目录；launchctl 仍使用替身。
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/uninstall-macos.sh"
test -x "$binary"
test -d "$state_dir"
test -d "$work_dir"
test ! -e "$app_support"
test ! -e "$plist"
test ! -e "$log_dir"

# 显式删除二进制仍保留数据。
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/uninstall-macos.sh" --remove-binary
test ! -e "$binary"
test -d "$state_dir"
test -d "$work_dir"

# 彻底删除必须使用显式参数。
mkdir -p "$home_dir/.local/bin" "$state_dir" "$work_dir"
: > "$binary"
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/uninstall-macos.sh" --purge-data
test ! -e "$binary"
test ! -e "$state_dir"
test ! -e "$work_dir"

# 签名脚本必须先解锁显式钥匙串，并原样传递包含空格的密码。
sign_fake_bin="$TMP_ROOT/sign fake bin"
sign_state="$TMP_ROOT/sign state"
mkdir -p "$sign_fake_bin" "$sign_state"
cat > "$sign_fake_bin/security" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
print -r -- "${(j:|:)@}" >> "$SIGN_TEST_SECURITY_CALLS"
case "$1" in
  unlock-keychain) exit 0 ;;
  find-identity)
    print -- "  1) $SIGN_TEST_IDENTITY \"AgentDock Local Code Signing\""
    ;;
  *) exit 2 ;;
esac
SCRIPT
cat > "$sign_fake_bin/codesign" <<'SCRIPT'
#!/bin/zsh
set -euo pipefail
print -r -- "${(j:|:)@}" >> "$SIGN_TEST_CODESIGN_CALLS"
case "$1" in
  --force|--verify) exit 0 ;;
  -dv)
    print -u2 -- "Identifier=$SIGN_TEST_IDENTIFIER"
    ;;
  *) exit 2 ;;
esac
SCRIPT
chmod 0755 "$sign_fake_bin/security" "$sign_fake_bin/codesign"
sign_target="$sign_state/agentdock"
sign_keychain="$sign_state/agentdock-codesign.keychain-db"
: > "$sign_target"
: > "$sign_keychain"
env -i \
  HOME="$home_dir" \
  PATH="$sign_fake_bin:$TEST_PATH" \
  SIGN_TEST_SECURITY_CALLS="$sign_state/security.calls" \
  SIGN_TEST_CODESIGN_CALLS="$sign_state/codesign.calls" \
  SIGN_TEST_IDENTITY=test-identity \
  SIGN_TEST_IDENTIFIER=com.local.agentdock \
  AGENTDOCK_CODESIGN_IDENTITY=test-identity \
  AGENTDOCK_CODESIGN_KEYCHAIN="$sign_keychain" \
  AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD='password with spaces' \
  AGENTDOCK_CODESIGN_IDENTIFIER=com.local.agentdock \
  zsh "$ROOT_DIR/scripts/sign-macos.sh" "$sign_target"
assert_file_contains "$sign_state/security.calls" "unlock-keychain|-p|password with spaces|$sign_keychain"
assert_file_contains "$sign_state/security.calls" "find-identity|-v|-p|codesigning|$sign_keychain"
assert_file_contains "$sign_state/codesign.calls" "--force|--keychain|$sign_keychain|--sign|test-identity"

# 签名钥匙串必须是普通文件，避免脚本和 Go 自更新侧安全规则不一致。
sign_keychain_link="$sign_state/agentdock-codesign-link.keychain-db"
ln -s "$sign_keychain" "$sign_keychain_link"
if env -i \
  HOME="$home_dir" \
  PATH="$sign_fake_bin:$TEST_PATH" \
  AGENTDOCK_CODESIGN_IDENTITY=test-identity \
  AGENTDOCK_CODESIGN_KEYCHAIN="$sign_keychain_link" \
  zsh "$ROOT_DIR/scripts/sign-macos.sh" "$sign_target" >/dev/null 2>&1; then
  print -u2 -- "sign script accepted a symlink keychain"
  exit 1
fi

print -- "macOS installer and uninstaller tests passed"
