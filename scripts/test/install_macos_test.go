package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSAppBuildPublishesDesktopUpdateArchive(t *testing.T) {
	buildData, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "build-app.sh"))
	if err != nil {
		t.Fatalf("read macOS App build script: %v", err)
	}
	build := string(buildData)
	for _, want := range []string{
		`ditto -c -k --keepParent "$APP_DIR" "$ZIP_PATH"`,
		`unzip -tq "$ZIP_PATH"`,
		`shasum -a 256 "${ZIP_PATH:t}" > "${ZIP_PATH:t}.sha256"`,
		`VERSION="${1:-0.0.0-dev}"`,
		`go run "$ROOT_DIR/tools/release" core-version "$VERSION"`,
		`<key>CFBundleShortVersionString</key>`,
		`<string>$MARKETING_VERSION</string>`,
		`<key>CFBundleVersion</key>`,
		`<string>$BUNDLE_VERSION</string>`,
		`<key>AgentDockReleaseVersion</key>`,
		`<string>$VERSION</string>`,
	} {
		if !strings.Contains(build, want) {
			t.Fatalf("build-app.sh missing macOS desktop update archive behavior %q", want)
		}
	}
	if strings.Contains(build, `go run "$ROOT_DIR/tools/release" version`) {
		t.Fatal("build-app.sh must not infer a release version from source state")
	}
}
