#!/bin/zsh
set -euo pipefail

ROOT_DIR="${0:A:h:h:h}"
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
  python3 packaging/build-core-skill-bundle.py --output "$build_dir/share/agentdock/core-skills"
)

tar -C "$build_dir" -czf "$release_dir/$asset" bin/agentdock share/agentdock/core-skills
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
    zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" "$@"
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

read_env_key() {
  local file_path="$1"
  local key="$2"
  /bin/zsh -c '
    source "$1" >/dev/null
    print -r -- "${(P)2:-}"
  ' _ "$file_path" "$key"
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
result_file="$TMP_ROOT/install-result.json"

# 全新安装使用 --no-start，只生成标准服务文件，不接触当前用户真实 LaunchAgent。
# --register-service 默认仅本机运行，不能因为启用后台服务就隐式创建公网入口。
run_installer \
  --version latest \
  --register-service \
  --non-interactive \
  --result-file "$result_file" \
  --no-start \
  --host 127.0.0.1 \
  --port 18766 \
  --auth-token 'initial token with spaces'

test -x "$binary"
"$binary" --help >/dev/null 2>&1
test -d "$state_dir"
test -f "$state_dir/skill-store/bundled-skills.json"
test -f "$state_dir/skill-store/installed/agentdock-user-guide/1.1.0/SKILL.md"
test -f "$state_dir/skill-store/installed/skill-authoring/1.2.0/SKILL.md"
test -f "$state_dir/skill-store/installed/skill-installation/1.2.0/SKILL.md"
test -f "$state_dir/skill-store/installed/skill-vetter-runtime/0.1.5/SKILL.md"
test -d "$backup_dir"
test -d "$work_dir"
test -d "$app_support"
test -d "$log_dir"
test "$(mode_of "$app_support")" = "700"
test "$(mode_of "$agentdock_env")" = "600"
test ! -e "$start_script"
test "$(mode_of "$log_dir")" = "700"
test "$(mode_of "$log_dir/agentdock.out.log")" = "600"
test "$(mode_of "$log_dir/agentdock.err.log")" = "600"

assert_file_contains "$agentdock_env" 'AGENTDOCK_HOST=127.0.0.1'
assert_file_contains "$agentdock_env" 'AGENTDOCK_PORT=18766'
assert_file_contains "$agentdock_env" 'AGENTDOCK_AUTH_TOKEN=initial\ token\ with\ spaces'
plutil -lint "$plist" >/dev/null
test "$(plutil -extract ProgramArguments.0 raw -o - "$plist")" = "$binary"
test "$(plutil -extract ProgramArguments.1 raw -o - "$plist")" = "service"
test "$(plutil -extract ProgramArguments.2 raw -o - "$plist")" = "launch-core"
test "$(plutil -extract ProgramArguments.3 raw -o - "$plist")" = "--runtime-root"
test "$(plutil -extract ProgramArguments.4 raw -o - "$plist")" = "$app_support"
test "$(plutil -extract WorkingDirectory raw -o - "$plist")" = "$work_dir"
test "$(plutil -extract StandardOutPath raw -o - "$plist")" = "/dev/null"
test "$(plutil -extract StandardErrorPath raw -o - "$plist")" = "/dev/null"
test ! -e "$home_dir/Library/LaunchAgents/com.uvwt.agentdock.cloudflared.plist"
test "$(mode_of "$result_file")" = "600"
test "$(plutil -extract schema_version raw -o - "$result_file")" = "1"
test "$(plutil -extract ok raw -o - "$result_file")" = "true"
test "$(plutil -extract healthy raw -o - "$result_file")" = "false"
test "$(plutil -extract local_mcp_url raw -o - "$result_file")" = "http://127.0.0.1:18766/mcp"
test "$(plutil -extract tunnel_mode raw -o - "$result_file")" = "none"
test "$(plutil -extract public_mcp_url raw -o - "$result_file")" = ""
test "$(plutil -extract auth_token raw -o - "$result_file")" = "initial token with spaces"

# 模拟旧版本遗留 Nexus 环境凭据；重复安装必须删除，配对身份只保存在 device.json。
python3 - "$agentdock_env" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
text = path.read_text()
text += "AGENTDOCK_NEXUS_ENDPOINT=https://nexus.example.test\n"
text += "AGENTDOCK_NEXUS_TOKEN=obsolete-secret\n"
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
assert_file_not_contains "$agentdock_env" 'AGENTDOCK_NEXUS_ENDPOINT'
assert_file_not_contains "$agentdock_env" 'AGENTDOCK_NEXUS_TOKEN'
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

# Named / Quick Tunnel 必须集成进安装器，并保持 Cloudflare Token 与 AgentDock env 隔离。
# 模拟 Homebrew 的 PATH 入口：命令是相对软链接，安装器应复制真实文件到受控路径。
fake_cloudflared="$TMP_ROOT/fake-cloudflared-cellar/cloudflared"
fake_cloudflared_bin="$TMP_ROOT/fake-cloudflared-bin"
mkdir -p "$(dirname "$fake_cloudflared")" "$fake_cloudflared_bin"
cat > "$fake_cloudflared" <<'SCRIPT'
#!/bin/zsh
[[ "${1:-}" == "--version" ]] && print -- "cloudflared version test"
SCRIPT
chmod 0755 "$fake_cloudflared"
ln -s ../fake-cloudflared-cellar/cloudflared "$fake_cloudflared_bin/cloudflared"

# 图形 DMG 使用强制离线模式：核心和 cloudflared 都从本地载荷安装，
# 即使 PATH 中的 curl 被替换为失败程序，也不能发生任何下载尝试。
offline_home="$TMP_ROOT/offline home"
offline_bin="$TMP_ROOT/offline bin"
offline_curl_calls="$TMP_ROOT/offline curl calls"
offline_cloudflared_checksum="$TMP_ROOT/cloudflared.sha256"
mkdir -p "$offline_home" "$offline_bin"
cat > "$offline_bin/curl" <<SCRIPT
#!/bin/zsh
print -r -- "\$*" >> "$offline_curl_calls"
exit 91
SCRIPT
chmod 0755 "$offline_bin/curl"
shasum -a 256 "$fake_cloudflared" > "$offline_cloudflared_checksum"
env -i \
  HOME="$offline_home" \
  PATH="$offline_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --offline \
    --agentdock-archive "$release_dir/$asset" \
    --agentdock-checksum-file "$release_dir/$asset.sha256" \
    --cloudflared-binary "$fake_cloudflared" \
    --cloudflared-checksum-file "$offline_cloudflared_checksum" \
    --register-service \
    --tunnel quick \
    --no-start
test ! -e "$offline_curl_calls"
test -x "$offline_home/.local/bin/agentdock"
test -x "$offline_home/.local/bin/cloudflared"
assert_file_contains "$offline_home/Library/Application Support/AgentDock/cloudflared.env" 'AGENTDOCK_TUNNEL_MODE=quick'

bad_agentdock_checksum="$TMP_ROOT/bad-agentdock.sha256"
print -r -- "$(printf '0%.0s' {1..64})  $asset" > "$bad_agentdock_checksum"
if env -i \
  HOME="$TMP_ROOT/offline bad checksum home" \
  PATH="$offline_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --offline \
    --agentdock-archive "$release_dir/$asset" \
    --agentdock-checksum-file "$bad_agentdock_checksum" \
    --register-service \
    --tunnel none \
    --no-start >/dev/null 2>&1; then
  print -u2 -- "offline installer accepted a corrupted AgentDock checksum"
  exit 1
fi
test ! -e "$TMP_ROOT/offline bad checksum home/.local/bin/agentdock"
test ! -e "$offline_curl_calls"

if env -i \
  HOME="$TMP_ROOT/offline missing payload home" \
  PATH="$offline_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --offline \
    --register-service \
    --tunnel none \
    --no-start >/dev/null 2>&1; then
  print -u2 -- "offline installer accepted missing AgentDock payload"
  exit 1
fi
test ! -e "$offline_curl_calls"

# 图形安装器通过受限临时文件传递 Token。权限过宽时必须在任何安装动作前拒绝并删除文件。
insecure_token_file="$TMP_ROOT/insecure-tunnel-token"
print -rn -- 'must-not-be-used' > "$insecure_token_file"
chmod 0644 "$insecure_token_file"
if env -i \
  HOME="$home_dir" \
  PATH="$fake_cloudflared_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --non-interactive \
    --tunnel named \
    --server-url https://agent.example.test \
    --tunnel-token-file "$insecure_token_file" \
    --no-start >/dev/null 2>&1; then
  print -u2 -- "installer accepted an insecure Tunnel Token file"
  exit 1
fi
test ! -e "$insecure_token_file"

tunnel_token_file="$TMP_ROOT/tunnel-token"
print -rn -- 'named-token-value' > "$tunnel_token_file"
chmod 0600 "$tunnel_token_file"
env -i \
  HOME="$home_dir" \
  PATH="$fake_cloudflared_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --non-interactive \
    --tunnel named \
    --server-url https://agent.example.test \
    --tunnel-token-file "$tunnel_token_file" \
    --no-start
test ! -e "$tunnel_token_file"

tunnel_env="$app_support/cloudflared.env"
tunnel_start="$app_support/start-cloudflared.sh"
tunnel_plist="$home_dir/Library/LaunchAgents/com.uvwt.agentdock.cloudflared.plist"
test -x "$home_dir/.local/bin/cloudflared"
test ! -L "$home_dir/.local/bin/cloudflared"
cmp "$fake_cloudflared" "$home_dir/.local/bin/cloudflared"
test "$(mode_of "$tunnel_env")" = "600"
test ! -e "$tunnel_start"
assert_file_contains "$tunnel_env" 'AGENTDOCK_TUNNEL_MODE=named'
assert_file_contains "$tunnel_env" 'TUNNEL_TOKEN=named-token-value'
assert_file_contains "$agentdock_env" 'AGENTDOCK_SERVER_URL=https://agent.example.test'
assert_file_contains "$agentdock_env" 'AGENTDOCK_OAUTH_ENABLED=true'
assert_file_not_contains "$agentdock_env" 'named-token-value'
plutil -lint "$tunnel_plist" >/dev/null
test "$(plutil -extract ProgramArguments.0 raw -o - "$tunnel_plist")" = "$binary"
test "$(plutil -extract ProgramArguments.1 raw -o - "$tunnel_plist")" = "tunnel"
test "$(plutil -extract ProgramArguments.2 raw -o - "$tunnel_plist")" = "launch"
test "$(plutil -extract ProgramArguments.3 raw -o - "$tunnel_plist")" = "--runtime-root"
test "$(plutil -extract ProgramArguments.4 raw -o - "$tunnel_plist")" = "$app_support"
test "$(plutil -extract StandardOutPath raw -o - "$tunnel_plist")" = "/dev/null"
test "$(plutil -extract StandardErrorPath raw -o - "$tunnel_plist")" = "/dev/null"

# 原生 Tunnel LaunchAgent 的 PID 对应 agentdock wrapper，而不是它启动的 cloudflared 子进程。
# Named Tunnel 的启动与回滚验证都必须接受 plist 中实际运行的命令。
env -i \
  HOME="$home_dir" \
  PATH="$TEST_PATH" \
  zsh -c '
    set -euo pipefail
    source "$1"
    TARGET="$2"
    CLOUDFLARED_TARGET="$3"
    TUNNEL_START_SCRIPT="$4"
    TUNNEL_STDERR_LOG="$5"
    APP_SUPPORT_DIR="$6"
    TUNNEL_MODE=named
    tunnel_launchd_pid() { print -r -- 4321; }
    ps() { print -r -- "$TARGET tunnel launch --runtime-root $APP_SUPPORT_DIR"; }
    sleep() { :; }

    test "$(wait_for_tunnel "gui/501")" = 4321
    wait_for_tunnel_process "gui/501"
  ' _ \
  "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
  "$binary" \
  "$home_dir/.local/bin/cloudflared" \
  "$tunnel_start" \
  "$log_dir/cloudflared.err.log" \
  "$app_support"

named_oauth_password="$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_PASSWORD)"
named_oauth_secret="$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_TOKEN_SECRET)"
(( ${#named_oauth_password} >= 12 ))
(( ${#named_oauth_secret} >= 32 ))

# Named Tunnel 重复安装应自动沿用已有公网模式、Origin、认证凭据和私密 Token。
env -i \
  HOME="$home_dir" \
  PATH="$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --register-service \
    --no-start
assert_file_contains "$tunnel_env" 'TUNNEL_TOKEN=named-token-value'
assert_file_contains "$agentdock_env" 'AGENTDOCK_SERVER_URL=https://agent.example.test'
assert_file_contains "$agentdock_env" 'AGENTDOCK_OAUTH_ENABLED=true'
test "$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_PASSWORD)" = "$named_oauth_password"
test "$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_TOKEN_SECRET)" = "$named_oauth_secret"
assert_file_not_contains "$agentdock_env" 'named-token-value'

env -i \
  HOME="$home_dir" \
  PATH="$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --tunnel quick \
    --no-start
assert_file_contains "$tunnel_env" 'AGENTDOCK_TUNNEL_MODE=quick'
assert_file_contains "$tunnel_env" "TUNNEL_TOKEN=''"
assert_file_contains "$agentdock_env" "AGENTDOCK_SERVER_URL=''"
assert_file_contains "$agentdock_env" 'AGENTDOCK_OAUTH_ENABLED=false'
test "$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_PASSWORD)" = "$named_oauth_password"
test "$(read_env_key "$agentdock_env" AGENTDOCK_OAUTH_TOKEN_SECRET)" = "$named_oauth_secret"
assert_file_not_contains "$tunnel_env" 'named-token-value'

# 真实执行登录后原生 Quick Tunnel 入口：新地址必须原子回写、健康验证后发布，已有凭据不得轮换。
quick_runtime_home="$TMP_ROOT/quick runtime home"
quick_runtime_bin="$TMP_ROOT/quick runtime bin"
quick_runtime_support="$quick_runtime_home/Library/Application Support/AgentDock"
quick_runtime_logs="$quick_runtime_home/Library/Logs/AgentDock"
quick_runtime_env="$quick_runtime_support/agentdock.env"
quick_runtime_url_file="$quick_runtime_support/quick-tunnel-url.txt"
mkdir -p "$quick_runtime_home/.local/bin" "$quick_runtime_bin" "$quick_runtime_support" "$quick_runtime_logs"
quick_runtime_port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
cat > "$quick_runtime_env" <<ENV
AGENTDOCK_HOST=127.0.0.1
AGENTDOCK_PORT=$quick_runtime_port
AGENTDOCK_AUTH_TOKEN=stable-bearer-token
AGENTDOCK_SERVER_URL=https://old.trycloudflare.com
AGENTDOCK_OAUTH_ENABLED=true
AGENTDOCK_OAUTH_PASSWORD=stable-oauth-password
AGENTDOCK_OAUTH_TOKEN_SECRET=stable-oauth-secret-0123456789abcdef
ENV
cat > "$quick_runtime_support/cloudflared.env" <<'ENV'
AGENTDOCK_TUNNEL_MODE=quick
AGENTDOCK_TUNNEL_TARGET=http://127.0.0.1:$quick_runtime_port
TUNNEL_TOKEN=''
ENV
cp "$binary" "$quick_runtime_home/.local/bin/agentdock"
chmod 0600 "$quick_runtime_env" "$quick_runtime_support/cloudflared.env"
chmod 0755 "$quick_runtime_home/.local/bin/agentdock"
cat > "$quick_runtime_home/.local/bin/cloudflared" <<'SCRIPT'
#!/bin/zsh
print -u2 -- 'INF Your quick Tunnel has been created! Visit it at:'
print -u2 -- 'https://rebooted.trycloudflare.com'
sleep 2
SCRIPT
cat > "$quick_runtime_bin/launchctl" <<'SCRIPT'
#!/bin/zsh
exit 0
SCRIPT
chmod 0755 "$quick_runtime_home/.local/bin/cloudflared" "$quick_runtime_bin/launchctl"
python3 - "$quick_runtime_port" <<'PY' &
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import sys

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/healthz":
            self.send_response(404)
            self.end_headers()
            return
        body = json.dumps({"ok": True, "version": "v0.5.4"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass

HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
quick_health_pid=$!
quick_health_ready=false
for _ in {1..50}; do
  if /usr/bin/curl -fsS --max-time 1 "http://127.0.0.1:$quick_runtime_port/healthz" >/dev/null 2>&1; then
    quick_health_ready=true
    break
  fi
  sleep 0.1
done
if [[ "$quick_health_ready" != true ]]; then
  kill "$quick_health_pid" >/dev/null 2>&1 || true
  wait "$quick_health_pid" 2>/dev/null || true
  print -u2 -- "Quick Tunnel fake health server did not become ready"
  exit 1
fi
if ! env -i \
  HOME="$quick_runtime_home" \
  PATH="$quick_runtime_bin:$TEST_PATH" \
  AGENTDOCK_LAUNCHCTL_BIN="$quick_runtime_bin/launchctl" \
  "$quick_runtime_home/.local/bin/agentdock" tunnel launch --runtime-root "$quick_runtime_support"; then
  kill "$quick_health_pid" >/dev/null 2>&1 || true
  wait "$quick_health_pid" 2>/dev/null || true
  print -u2 -- "Quick Tunnel native launch failed"
  exit 1
fi
kill "$quick_health_pid" >/dev/null 2>&1 || true
wait "$quick_health_pid" 2>/dev/null || true
test "$(read_env_key "$quick_runtime_env" AGENTDOCK_SERVER_URL)" = "https://rebooted.trycloudflare.com"
test "$(read_env_key "$quick_runtime_env" AGENTDOCK_OAUTH_ENABLED)" = "true"
test "$(read_env_key "$quick_runtime_env" AGENTDOCK_AUTH_TOKEN)" = "stable-bearer-token"
test "$(read_env_key "$quick_runtime_env" AGENTDOCK_OAUTH_PASSWORD)" = "stable-oauth-password"
test "$(read_env_key "$quick_runtime_env" AGENTDOCK_OAUTH_TOKEN_SECRET)" = "stable-oauth-secret-0123456789abcdef"
test "$(cat "$quick_runtime_url_file")" = "https://rebooted.trycloudflare.com"
test "$(mode_of "$quick_runtime_url_file")" = "600"

# Tunnel 配置失败时必须恢复旧 Tunnel 文件与旧公网认证状态，不能留下半更新状态。
old_tunnel_env_sha="$(sha256_of "$tunnel_env")"
old_tunnel_plist_sha="$(sha256_of "$tunnel_plist")"
old_cloudflared_sha="$(sha256_of "$home_dir/.local/bin/cloudflared")"
invalid_cloudflared="$TMP_ROOT/invalid-cloudflared"
: > "$invalid_cloudflared"
if env -i \
  HOME="$home_dir" \
  PATH="$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  AGENTDOCK_CLOUDFLARED_BINARY="$invalid_cloudflared" \
  AGENTDOCK_CLOUDFLARE_TUNNEL_TOKEN='must-not-survive' \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --tunnel named \
    --server-url https://broken.example.test \
    --no-start >/dev/null 2>&1; then
  print -u2 -- "installer unexpectedly accepted an invalid cloudflared binary"
  exit 1
fi
test "$(sha256_of "$tunnel_env")" = "$old_tunnel_env_sha"
test ! -e "$tunnel_start"
test "$(sha256_of "$tunnel_plist")" = "$old_tunnel_plist_sha"
test "$(sha256_of "$home_dir/.local/bin/cloudflared")" = "$old_cloudflared_sha"
assert_file_contains "$agentdock_env" "AGENTDOCK_SERVER_URL=''"
assert_file_not_contains "$agentdock_env" 'https://broken.example.test'
assert_file_not_contains "$tunnel_env" 'must-not-survive'

# 模拟 Quick Tunnel 刷新：新地址必须回写并重启 AgentDock，已有双认证凭据不得轮换。
quick_refresh_home="$TMP_ROOT/quick refresh home"
quick_refresh_state="$TMP_ROOT/quick refresh state"
mkdir -p "$quick_refresh_home/Library/Application Support/AgentDock" "$quick_refresh_state"
quick_refresh_env="$quick_refresh_home/Library/Application Support/AgentDock/agentdock.env"
cat > "$quick_refresh_env" <<'ENV'
AGENTDOCK_HOST=127.0.0.1
AGENTDOCK_PORT=18766
AGENTDOCK_AUTH_TOKEN=stable-bearer-token
AGENTDOCK_SERVER_URL=https://old.trycloudflare.com
AGENTDOCK_OAUTH_ENABLED=true
AGENTDOCK_OAUTH_PASSWORD=stable-oauth-password
AGENTDOCK_OAUTH_TOKEN_SECRET=stable-oauth-secret-0123456789abcdef
ENV
chmod 0600 "$quick_refresh_env"
env -i \
  HOME="$quick_refresh_home" \
  PATH="$TEST_PATH" \
  TEST_QUICK_REFRESH_STATE="$quick_refresh_state" \
  zsh -c '
    set -euo pipefail
    source "$1"
    TUNNEL_MODE=quick
    NO_START=false
    PUBLIC_AUTH_CONFIGURE=true
    SERVER_URL=https://old.trycloudflare.com
    snapshot_tunnel_state() { :; }
    install_cloudflared() { :; }
    write_tunnel_env() { :; }
    write_tunnel_launch_agent() { :; }
    register_and_start_tunnel() { TUNNEL_PUBLIC_URL=https://fresh.trycloudflare.com; }
    register_and_start_service() { print -- restarted >> "$TEST_QUICK_REFRESH_STATE/restarts"; }
    configure_tunnel
  ' _ "$ROOT_DIR/scripts/install/install-macos-platform.sh"
assert_file_contains "$quick_refresh_env" 'AGENTDOCK_SERVER_URL=https://fresh.trycloudflare.com'
assert_file_contains "$quick_refresh_env" 'AGENTDOCK_OAUTH_ENABLED=true'
assert_file_contains "$quick_refresh_env" 'AGENTDOCK_AUTH_TOKEN=stable-bearer-token'
assert_file_contains "$quick_refresh_env" 'AGENTDOCK_OAUTH_PASSWORD=stable-oauth-password'
assert_file_contains "$quick_refresh_env" 'AGENTDOCK_OAUTH_TOKEN_SECRET=stable-oauth-secret-0123456789abcdef'
test "$(wc -l < "$quick_refresh_state/restarts" | tr -d ' ')" = "1"

# 注册服务必须坚持标准二进制目标，不能把 plist 指向一处、二进制装到另一处。
if run_installer --register-service --no-start --install-dir "$TMP_ROOT/nonstandard" >/dev/null 2>&1; then
  print -u2 -- "installer accepted a non-standard service binary path"
  exit 1
fi

# 目标路径若是目录或符号链接，必须在下载和替换前拒绝。
invalid_home="$TMP_ROOT/invalid target home"
mkdir -p "$invalid_home/.local/bin/agentdock"
if env -i HOME="$invalid_home" PATH="$TEST_PATH" AGENTDOCK_RELEASE_BASE_URL="$release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" >/dev/null 2>&1; then
  print -u2 -- "installer accepted a non-regular binary target"
  exit 1
fi
test -d "$invalid_home/.local/bin/agentdock"

# LaunchAgent 必须直接调用固定二进制和原生 launch-core，不经 shell 读取 HOME/PATH。
test "$(plutil -extract ProgramArguments.0 raw -o - "$plist")" = "$binary"
test "$(plutil -extract ProgramArguments.1 raw -o - "$plist")" = "service"
test "$(plutil -extract ProgramArguments.2 raw -o - "$plist")" = "launch-core"

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
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
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
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
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
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --register-service >/dev/null
assert_file_contains "$fake_state/curl.calls" 'http://127.0.0.1:18767/healthz'
assert_file_not_contains "$fake_state/curl.calls" 'http://127.0.0.1:8765/healthz'
test -f "$fake_state/pid"

# 已有服务升级时，bootstrap 失败必须恢复旧二进制、env、启动脚本、plist 和旧 LaunchAgent。
rollback_release_dir="$TMP_ROOT/rollback release"
rollback_build_dir="$TMP_ROOT/rollback build"
mkdir -p "$rollback_release_dir" "$rollback_build_dir/bin" "$rollback_build_dir/share/agentdock"
cp -R "$build_dir/share/agentdock/core-skills" "$rollback_build_dir/share/agentdock/core-skills"
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
tar -C "$rollback_build_dir" -czf "$rollback_release_dir/$asset" bin/agentdock share/agentdock/core-skills
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
service_plist="$service_home/Library/LaunchAgents/com.uvwt.agentdock.plist"
old_binary_sha="$(sha256_of "$service_binary")"
old_env_sha="$(sha256_of "$service_env")"
old_plist_sha="$(sha256_of "$service_plist")"
: > "$fake_state/fail-bootstrap-once"
if env -i \
  HOME="$service_home" \
  PATH="$fake_bin:$TEST_PATH" \
  TMPDIR="$TMP_ROOT" \
  TEST_LAUNCHCTL_STATE="$fake_state" \
  AGENTDOCK_RELEASE_BASE_URL="$rollback_release_url" \
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
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
test ! -e "$service_home/Library/Application Support/AgentDock/start-agentdock.sh"
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
  zsh "$ROOT_DIR/scripts/install/install-macos-platform.sh" \
    --register-service \
    --host 127.0.0.8 \
    --port 18888 >/dev/null 2>&1; then
  print -u2 -- "installer unexpectedly succeeded after simulated bootout failure"
  exit 1
fi
test "$(cat "$fake_state/pid")" = "$old_pid"
test "$(sha256_of "$service_binary")" = "$old_binary_sha"
test "$(sha256_of "$service_env")" = "$old_env_sha"
test ! -e "$service_home/Library/Application Support/AgentDock/start-agentdock.sh"
test "$(sha256_of "$service_plist")" = "$old_plist_sha"
assert_file_contains "$fake_state/curl.calls" 'http://127.0.0.1:18767/healthz'
assert_file_not_contains "$fake_state/curl.calls" 'http://127.0.0.8:18888/healthz'

# 无法停止已加载服务时，卸载器必须保留全部运行文件。
if env -i HOME="$service_home" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/install/uninstall-macos.sh" >/dev/null 2>&1; then
  print -u2 -- "uninstaller ignored a launchctl bootout failure"
  exit 1
fi
test -f "$fake_state/pid"
test -x "$service_binary"
test -f "$service_env"
test ! -e "$service_home/Library/Application Support/AgentDock/start-agentdock.sh"
test -f "$service_plist"
rm -f "$fake_state/fail-bootout"

# 默认卸载只删服务，保留二进制、状态和工作目录；launchctl 仍使用替身。
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/install/uninstall-macos.sh"
test -x "$binary"
test -d "$state_dir"
test -d "$work_dir"
test ! -e "$app_support"
test ! -e "$plist"
test ! -e "$log_dir"

# 显式删除二进制仍保留数据。
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/install/uninstall-macos.sh" --remove-binary
test ! -e "$binary"
test -d "$state_dir"
test -d "$work_dir"

# 彻底删除必须使用显式参数。
mkdir -p "$home_dir/.local/bin" "$state_dir" "$work_dir"
: > "$binary"
env -i HOME="$home_dir" PATH="$fake_bin:$TEST_PATH" TEST_LAUNCHCTL_STATE="$fake_state" \
  zsh "$ROOT_DIR/scripts/install/uninstall-macos.sh" --purge-data
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
  zsh "$ROOT_DIR/packaging/macos/sign-macos.sh" "$sign_target"
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
  zsh "$ROOT_DIR/packaging/macos/sign-macos.sh" "$sign_target" >/dev/null 2>&1; then
  print -u2 -- "sign script accepted a symlink keychain"
  exit 1
fi

print -- "macOS installer and uninstaller tests passed"
