#!/bin/zsh
set -euo pipefail

ROOT_DIR="${0:A:h:h:h}"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentdock-macos-app-test.XXXXXX")"
MOUNT_POINT="$TMP_ROOT/mount"
MOUNTED=false

cleanup() {
  if [[ "$MOUNTED" == true ]]; then
    hdiutil detach "$MOUNT_POINT" -force >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

python3 "$ROOT_DIR/scripts/test/check-macos-i18n.py"

swiftc \
  -swift-version 5 \
  -parse-as-library \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/Localization.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Tests/LocalizationPreferenceTests.swift" \
  -o "$TMP_ROOT/localization-preference-tests"
"$TMP_ROOT/localization-preference-tests"

swiftc \
  -swift-version 5 \
  -parse-as-library \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/Localization.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/InstallerConfiguration.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/AppVersion.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/DesktopUpdateResult.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/DesktopUpdateServiceState.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/DesktopUpdateHandoff.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ManagedEnvironment.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/AppPaths.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ACPConfiguration.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/TunnelTokenStore.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/PublicEndpointChecker.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ServicePortValidation.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Tests/InstallerConfigurationTests.swift" \
  -o "$TMP_ROOT/installer-configuration-tests"
"$TMP_ROOT/installer-configuration-tests"

swiftc \
  -swift-version 5 \
  -parse-as-library \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/Localization.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/InstallerConfiguration.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/AppVersion.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/DesktopUpdateServiceState.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ManagedEnvironment.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/AppPaths.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ACPConfiguration.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/TunnelTokenStore.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/LegacyDesktopRuntimeMigration.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/ServiceController.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/InstallerRunner.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Tests/ServiceControllerValidationTests.swift" \
  -o "$TMP_ROOT/service-controller-validation-tests"
"$TMP_ROOT/service-controller-validation-tests"

swiftc \
  -swift-version 5 \
  -parse-as-library \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/Localization.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/DesktopPermissionChecker.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/FileAccessPermissionChecker.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Sources/PermissionUIComponents.swift" \
  "$ROOT_DIR/desktop/macos/AgentDockApp/Tests/PermissionCheckerTests.swift" \
  -o "$TMP_ROOT/permission-checker-tests"
"$TMP_ROOT/permission-checker-tests"

mkdir -p "$TMP_ROOT/output"
: > "$TMP_ROOT/output/AgentDock-macos-universal.zip"
: > "$TMP_ROOT/output/AgentDock-macos-universal.zip.sha256"

case "$(uname -m)" in
  arm64|aarch64) release_arch="arm64" ;;
  x86_64|amd64) release_arch="amd64" ;;
  *) print -u2 -- "unsupported test architecture: $(uname -m)"; exit 1 ;;
esac

# 构建当前架构的真实 Core 与最小 cloudflared 构建输入。最终 App 必须把它们
# 收进 Contents/Helpers，而不是继续携带安装后再解包的离线载荷。
payload_dir="$TMP_ROOT/offline-payload"
payload_build="$TMP_ROOT/offline-build"
mkdir -p "$payload_dir" "$payload_build/bin" "$payload_build/share/agentdock"
(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$release_arch" \
    go build -trimpath -o "$payload_build/bin/agentdock" ./cmd/agentdock
)
python3 "$ROOT_DIR/packaging/build-core-skill-bundle.py" \
  --output "$payload_build/share/agentdock/core-skills"
agentdock_archive="agentdock_darwin_${release_arch}.tar.gz"
tar -C "$payload_build" -czf "$payload_dir/$agentdock_archive" \
  bin/agentdock share/agentdock/core-skills
(
  cd "$payload_dir"
  shasum -a 256 "$agentdock_archive" > "$agentdock_archive.sha256"
)

cat > "$TMP_ROOT/cloudflared.go" <<'GO'
package main

import "fmt"

func main() {
	fmt.Println("cloudflared version test")
}
GO
cloudflared_binary="cloudflared_darwin_${release_arch}"
CGO_ENABLED=0 GOOS=darwin GOARCH="$release_arch" \
  go build -trimpath -o "$payload_dir/$cloudflared_binary" "$TMP_ROOT/cloudflared.go"
chmod 0755 "$payload_dir/$cloudflared_binary"
(
  cd "$payload_dir"
  shasum -a 256 "$cloudflared_binary" > "$cloudflared_binary.sha256"
)

AGENTDOCK_MACOS_ARCHES="$(uname -m)" \
AGENTDOCK_MACOS_APP_OUTPUT_DIR="$TMP_ROOT/output" \
AGENTDOCK_MACOS_OFFLINE_PAYLOAD_DIR="$payload_dir" \
  "$ROOT_DIR/packaging/macos/build-app.sh"

APP="$TMP_ROOT/output/AgentDock.app"
DMG="$TMP_ROOT/output/AgentDock-macos-universal.dmg"
ZIP="$TMP_ROOT/output/AgentDock-macos-universal.zip"
test -x "$APP/Contents/MacOS/AgentDock"
MENU_LOGIN_HELPER="$APP/Contents/Helpers/AgentDockLoginHelper"
test -x "$MENU_LOGIN_HELPER"
test ! -e "$APP/Contents/Library/LoginItems"
test ! -e "$APP/Contents/Resources/install-macos-platform.sh"
test ! -e "$APP/Contents/Resources/install-browser-runner-macos.sh"
test ! -e "$APP/Contents/Resources/browser-runner"
test ! -e "$APP/Contents/Resources/offline-payload"
test -f "$APP/Contents/Resources/AgentDock.icns"
CORE_HELPER="$APP/Contents/Helpers/agentdock"
CLOUDFLARED_HELPER="$APP/Contents/Helpers/cloudflared"
CORE_AGENT_PLIST="$APP/Contents/Library/LaunchAgents/com.uvwt.agentdock.core.plist"
TUNNEL_AGENT_PLIST="$APP/Contents/Library/LaunchAgents/com.uvwt.agentdock.tunnel.plist"
MENU_AGENT_PLIST="$APP/Contents/Library/LaunchAgents/com.uvwt.agentdock.menu-login.plist"
test -x "$CORE_HELPER"
test -x "$CLOUDFLARED_HELPER"
test -f "$APP/Contents/Resources/core-skills/manifest.json"
test -f "$CORE_AGENT_PLIST"
test -f "$TUNNEL_AGENT_PLIST"
test -f "$MENU_AGENT_PLIST"
plutil -lint "$CORE_AGENT_PLIST" >/dev/null
plutil -lint "$TUNNEL_AGENT_PLIST" >/dev/null
plutil -lint "$MENU_AGENT_PLIST" >/dev/null
test "$(plutil -extract Label raw -o - "$CORE_AGENT_PLIST")" = "com.uvwt.agentdock.core"
test "$(plutil -extract BundleProgram raw -o - "$CORE_AGENT_PLIST")" = "Contents/Helpers/agentdock"
test "$(plutil -extract Label raw -o - "$TUNNEL_AGENT_PLIST")" = "com.uvwt.agentdock.tunnel"
test "$(plutil -extract BundleProgram raw -o - "$TUNNEL_AGENT_PLIST")" = "Contents/Helpers/agentdock"
test "$(plutil -extract RunAtLoad raw -o - "$TUNNEL_AGENT_PLIST")" = "true"
test "$(plutil -extract KeepAlive raw -o - "$TUNNEL_AGENT_PLIST")" = "true"
test "$(plutil -extract ProcessType raw -o - "$TUNNEL_AGENT_PLIST")" = "Background"
test "$(plutil -extract ThrottleInterval raw -o - "$TUNNEL_AGENT_PLIST")" = "5"
test "$(plutil -extract Label raw -o - "$MENU_AGENT_PLIST")" = "com.uvwt.agentdock.menu-login"
test "$(plutil -extract BundleProgram raw -o - "$MENU_AGENT_PLIST")" = "Contents/Helpers/AgentDockLoginHelper"
test "$(plutil -extract ProgramArguments.0 raw -o - "$MENU_AGENT_PLIST")" = "AgentDockLoginHelper"
! plutil -extract ProgramArguments.1 raw -o - "$MENU_AGENT_PLIST" >/dev/null 2>&1
test "$(plutil -extract RunAtLoad raw -o - "$MENU_AGENT_PLIST")" = "true"
test "$(plutil -extract LimitLoadToSessionType raw -o - "$MENU_AGENT_PLIST")" = "Aqua"
! plutil -extract KeepAlive raw -o - "$MENU_AGENT_PLIST" >/dev/null 2>&1
! plutil -extract ProcessType raw -o - "$MENU_AGENT_PLIST" >/dev/null 2>&1
! grep -Eq '/Users/|\.local/bin|Library/LaunchAgents' "$CORE_AGENT_PLIST" "$TUNNEL_AGENT_PLIST" "$MENU_AGENT_PLIST"
# pipefail 下不要用 grep -q 提前关闭命令输出，避免上游偶发 SIGPIPE(141)。
core_helper_version="$("$CORE_HELPER" --version)"
[[ "$core_helper_version" == "AgentDock v"* ]]
cloudflared_helper_version="$("$CLOUDFLARED_HELPER" --version)"
[[ "$cloudflared_helper_version" == "cloudflared version test"* ]]
codesign --verify --strict --verbose=2 "$MENU_LOGIN_HELPER"
codesign --verify --strict --verbose=2 "$CORE_HELPER"
codesign --verify --strict --verbose=2 "$CLOUDFLARED_HELPER"
menu_helper_signature="$(codesign -dv --verbose=4 "$MENU_LOGIN_HELPER" 2>&1)"
core_signature="$(codesign -dv --verbose=4 "$CORE_HELPER" 2>&1)"
cloudflared_signature="$(codesign -dv --verbose=4 "$CLOUDFLARED_HELPER" 2>&1)"
grep -q '^Identifier=com.uvwt.agentdock.login-helper$' <<< "$menu_helper_signature"
grep -q '^Identifier=com.uvwt.agentdock.core$' <<< "$core_signature"
grep -q '^Identifier=com.uvwt.agentdock.cloudflared$' <<< "$cloudflared_signature"
test -f "$DMG"
test -f "$DMG.sha256"
test -f "$ZIP"
test -f "$ZIP.sha256"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
test "$(plutil -extract CFBundleIdentifier raw -o - "$APP/Contents/Info.plist")" = "com.uvwt.agentdock"
test "$(plutil -extract CFBundleIconFile raw -o - "$APP/Contents/Info.plist")" = "AgentDock.icns"
test "$(plutil -extract CFBundleDevelopmentRegion raw -o - "$APP/Contents/Info.plist")" = "en"
test "$(plutil -extract LSUIElement raw -o - "$APP/Contents/Info.plist")" = "true"
test -n "$(plutil -extract NSAppleEventsUsageDescription raw -o - "$APP/Contents/Info.plist")"
for localization in en zh-Hans; do
  lproj="$APP/Contents/Resources/$localization.lproj"
  test -f "$lproj/Localizable.strings"
  test -f "$lproj/InfoPlist.strings"
  plutil -lint "$lproj/Localizable.strings" >/dev/null
  plutil -lint "$lproj/InfoPlist.strings" >/dev/null
done
codesign --verify --deep --strict --verbose=2 "$APP"
hdiutil verify "$DMG" >/dev/null
(
  cd "$TMP_ROOT/output"
  shasum -a 256 -c "${DMG:t}.sha256"
  shasum -a 256 -c "${ZIP:t}.sha256"
)

zip_extract="$TMP_ROOT/zip-extract"
mkdir -p "$zip_extract"
ditto -x -k "$ZIP" "$zip_extract"
test -d "$zip_extract/AgentDock.app"
codesign --verify --deep --strict --verbose=2 "$zip_extract/AgentDock.app"
cmp "$APP/Contents/MacOS/AgentDock" "$zip_extract/AgentDock.app/Contents/MacOS/AgentDock"
cmp "$MENU_LOGIN_HELPER" "$zip_extract/AgentDock.app/Contents/Helpers/AgentDockLoginHelper"
cmp "$MENU_AGENT_PLIST" "$zip_extract/AgentDock.app/Contents/Library/LaunchAgents/com.uvwt.agentdock.menu-login.plist"
cmp "$CORE_HELPER" "$zip_extract/AgentDock.app/Contents/Helpers/agentdock"
cmp "$CLOUDFLARED_HELPER" "$zip_extract/AgentDock.app/Contents/Helpers/cloudflared"

mkdir -p "$MOUNT_POINT"
hdiutil attach -readonly -nobrowse -mountpoint "$MOUNT_POINT" "$DMG" >/dev/null
MOUNTED=true
mounted_entries="$(find "$MOUNT_POINT" -mindepth 1 -maxdepth 1 -exec basename {} \; | LC_ALL=C sort)"
test "$mounted_entries" = $'AgentDock.app\nApplications'
test -d "$MOUNT_POINT/AgentDock.app"
test -L "$MOUNT_POINT/Applications"
test "$(readlink "$MOUNT_POINT/Applications")" = "/Applications"
codesign --verify --deep --strict --verbose=2 "$MOUNT_POINT/AgentDock.app"
cmp "$APP/Contents/MacOS/AgentDock" "$MOUNT_POINT/AgentDock.app/Contents/MacOS/AgentDock"
cmp "$MENU_LOGIN_HELPER" "$MOUNT_POINT/AgentDock.app/Contents/Helpers/AgentDockLoginHelper"
cmp "$MENU_AGENT_PLIST" "$MOUNT_POINT/AgentDock.app/Contents/Library/LaunchAgents/com.uvwt.agentdock.menu-login.plist"
cmp "$CORE_HELPER" "$MOUNT_POINT/AgentDock.app/Contents/Helpers/agentdock"
cmp "$CLOUDFLARED_HELPER" "$MOUNT_POINT/AgentDock.app/Contents/Helpers/cloudflared"
cmp \
  "$APP/Contents/Resources/core-skills/manifest.json" \
  "$MOUNT_POINT/AgentDock.app/Contents/Resources/core-skills/manifest.json"
hdiutil detach "$MOUNT_POINT" >/dev/null
MOUNTED=false

print -- "macOS menu app DMG tests passed"
