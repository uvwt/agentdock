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
		`$ROOT_DIR/internal/buildinfo/buildinfo.go`,
	} {
		if !strings.Contains(build, want) {
			t.Fatalf("build-app.sh missing macOS desktop update archive behavior %q", want)
		}
	}
	if strings.Contains(build, "internal/config/config.go") {
		t.Fatal("build-app.sh must read the App version from shared buildinfo, not config.Version")
	}
}
