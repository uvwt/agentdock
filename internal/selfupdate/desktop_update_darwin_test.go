//go:build darwin

package selfupdate

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractDesktopUpdateArchiveValidatesSignedApp(t *testing.T) {
	dir := t.TempDir()
	sourceRoot := filepath.Join(dir, "source")
	appPath := writeSignedMacOSApp(t, sourceRoot, "0.7.1")
	archivePath := filepath.Join(dir, macOSDesktopArchiveName)
	runTestCommand(t, "/usr/bin/ditto", "-c", "-k", "--keepParent", appPath, archivePath)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	extracted, err := extractDesktopUpdateArchive(
		context.Background(),
		archiveData,
		filepath.Join(dir, "extract"),
		"v0.7.1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(extracted) != "AgentDock.app" {
		t.Fatalf("unexpected extracted App path: %s", extracted)
	}
	if err := validateMacOSDesktopVersion(context.Background(), extracted, "v0.7.1"); err != nil {
		t.Fatal(err)
	}
}

func TestExtractDesktopUpdateArchiveTreatsSignedAppContentsAsOpaque(t *testing.T) {
	dir := t.TempDir()
	appPath := writeSignedMacOSApp(t, filepath.Join(dir, "source"), "0.7.1")

	// updater 的稳定边界只有 Bundle 身份、版本和签名。包内 Helper、组件和资源
	// 可以随产品演进增删，不能再成为旧 updater 拒绝新版本的理由。
	for _, relative := range []string{
		"Contents/Helpers/cloudflared",
		"Contents/Helpers/agentdock-arbiter",
		"Contents/Helpers/AgentDockLoginHelper",
		"Contents/Library/LaunchAgents/com.uvwt.agentdock.menu-login.plist",
		"Contents/Resources/core-skills/manifest.json",
	} {
		if err := os.Remove(filepath.Join(appPath, filepath.FromSlash(relative))); err != nil {
			t.Fatal(err)
		}
	}
	runTestCommand(t, "/usr/bin/codesign", "--force", "--deep", "--sign", "-", "--identifier", "com.uvwt.agentdock", appPath)

	archivePath := filepath.Join(dir, macOSDesktopArchiveName)
	runTestCommand(t, "/usr/bin/ditto", "-c", "-k", "--keepParent", appPath, archivePath)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	extracted, err := extractDesktopUpdateArchive(
		context.Background(),
		archiveData,
		filepath.Join(dir, "extract"),
		"v0.7.1",
	)
	if err != nil {
		t.Fatalf("signed App with changed internal layout was rejected: %v", err)
	}
	if err := validateMacOSDesktopVersion(context.Background(), extracted, "v0.7.1"); err != nil {
		t.Fatal(err)
	}
}

func TestMacOSDesktopUpdateInstallsAndRestoresApp(t *testing.T) {
	dir := t.TempDir()
	target := writeSignedMacOSApp(t, filepath.Join(dir, "installed"), "0.7.0")
	staged := writeSignedMacOSApp(t, filepath.Join(dir, "staged"), "0.7.1")

	transaction, err := prepareDesktopUpdate(
		context.Background(),
		target,
		staged,
		"v0.7.1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMacOSAppVersion(t, target, "v0.7.1")
	if err := transaction.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMacOSAppVersion(t, target, "v0.7.0")
}

func TestApplyPlatformUpdateRestoresCoreAndAppWhenSkillBootstrapFails(t *testing.T) {
	dir := t.TempDir()
	coreTarget := filepath.Join(dir, "bin", "agentdock")
	coreStaged := filepath.Join(dir, "staged-agentdock")
	if err := os.MkdirAll(filepath.Dir(coreTarget), 0o700); err != nil {
		t.Fatal(err)
	}
	writeVersionScript(t, coreTarget, "v0.7.0")
	failedCore := `#!/bin/sh
case "${1:-}" in
  --version) printf 'AgentDock v0.7.1\n' ;;
  skill) printf 'bootstrap failed\n' >&2; exit 1 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(coreStaged, []byte(failedCore), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := writeCoreSkillBundle(t, dir)
	appTarget := writeSignedMacOSApp(t, filepath.Join(dir, "Applications"), "0.7.0")
	appStaged := writeSignedMacOSApp(t, filepath.Join(dir, "staged-app"), "0.7.1")

	_, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:       coreTarget,
		CurrentVersion:    "v0.7.0",
		StagedPath:        coreStaged,
		BundlePath:        bundle,
		DesktopTargetPath: appTarget,
		DesktopStagedPath: appStaged,
		TargetVersion:     "v0.7.1",
		Output:            io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "已自动恢复旧版本") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertVersionScript(t, coreTarget, "v0.7.0")
	assertMacOSAppVersion(t, appTarget, "v0.7.0")
}

func TestMacOSDesktopUpdateFinishAcceptsHandoffBeforeServiceRecovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := writeHandoffTestMacOSApp(t, filepath.Join(t.TempDir(), "Applications"))
	update := &macOSDesktopUpdate{
		targetPath:    target,
		targetVersion: "v0.7.1",
		appWasRunning: true,
	}
	t.Cleanup(func() {
		pids, _ := runningMacOSAppPIDs(context.Background(), target)
		_ = terminatePIDs(context.Background(), pids)
	})

	go func() {
		time.Sleep(150 * time.Millisecond)
		directory := filepath.Join(home, "Library", "Application Support", "AgentDock")
		_ = os.MkdirAll(directory, 0o700)
		_ = os.WriteFile(
			filepath.Join(directory, "update-handoff.json"),
			[]byte("{\"schema_version\":1,\"target_version\":\"v0.7.1\"}\n"),
			0o600,
		)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := update.Finish(ctx, desktopUpdateOutcome{
		OK:             true,
		CurrentVersion: "v0.7.0",
		TargetVersion:  "v0.7.1",
		Message:        "更新完成",
	}); err != nil {
		t.Fatal(err)
	}

	// handoff 只确认新版 App 已经接管事务；后台服务恢复期间 update-result 仍可保留。
	resultPath := filepath.Join(home, "Library", "Application Support", "AgentDock", "update-result.json")
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("update result should remain until App finishes recovery: %v", err)
	}

}

func TestWriteMacOSDesktopUpdateResultIsPrivateAndExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outcome := desktopUpdateOutcome{
		OK:             true,
		CurrentVersion: "v0.7.0",
		TargetVersion:  "v0.7.1",
		Message:        "更新完成",
	}
	if err := writeMacOSDesktopUpdateResult(outcome); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, "Library", "Application Support", "AgentDock", "update-result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		SchemaVersion  int    `json:"schema_version"`
		OK             bool   `json:"ok"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || !result.OK || result.CurrentVersion != "v0.7.0" ||
		result.TargetVersion != "v0.7.1" || result.Message != "更新完成" {
		t.Fatalf("unexpected update result: %#v", result)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("update result permissions = %o, want 600", info.Mode().Perm())
	}
}

func writeHandoffTestMacOSApp(t *testing.T, root string) string {
	t.Helper()
	appPath := filepath.Join(root, "AgentDock.app")
	contents := filepath.Join(appPath, "Contents")
	macOSDir := filepath.Join(contents, "MacOS")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "handoff-app.c")
	program := "#include <unistd.h>\nint main(void) { sleep(30); return 0; }\n"
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(macOSDir, "AgentDock")
	runTestCommand(t, "/usr/bin/xcrun", "clang", "-Os", source, "-o", executable)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>AgentDock</string>
<key>CFBundleIdentifier</key><string>com.uvwt.agentdock.handoff-test</string>
<key>CFBundleName</key><string>AgentDock</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.7.1</string>
<key>CFBundleVersion</key><string>0.7.1</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "/usr/bin/codesign", "--force", "--deep", "--sign", "-", "--identifier", "com.uvwt.agentdock.handoff-test", appPath)
	return appPath
}

func writeSignedMacOSApp(t *testing.T, root, version string) string {
	t.Helper()
	return writeSignedMacOSAppWithArbiter(t, root, version, true)
}

func writeSignedMacOSAppWithArbiter(t *testing.T, root, version string, includeArbiter bool) string {
	t.Helper()
	appPath := filepath.Join(root, "AgentDock.app")
	contents := filepath.Join(appPath, "Contents")
	macOSDir := filepath.Join(contents, "MacOS")
	helpersDir := filepath.Join(contents, "Helpers")
	launchAgentsDir := filepath.Join(contents, "Library", "LaunchAgents")
	skillsDir := filepath.Join(contents, "Resources", "core-skills")
	for _, dir := range []string{macOSDir, helpersDir, launchAgentsDir, skillsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(macOSDir, "AgentDock")
	binary, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	coreSource := filepath.Join(root, "core.c")
	coreProgram := "#include <stdio.h>\n#include <string.h>\nint main(int argc, char **argv) { if (argc > 1 && strcmp(argv[1], \"--version\") == 0) { puts(\"AgentDock v" + version + "\"); return 0; } return 0; }\n"
	if err := os.WriteFile(coreSource, []byte(coreProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "/usr/bin/xcrun", "clang", "-Os", coreSource, "-o", filepath.Join(helpersDir, "agentdock"))
	cloudflaredBinary, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helpersDir, "cloudflared"), cloudflaredBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if includeArbiter {
		if err := os.WriteFile(filepath.Join(helpersDir, "agentdock-arbiter"), cloudflaredBinary, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(helpersDir, "AgentDockLoginHelper"), cloudflaredBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	menuAgentPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.uvwt.agentdock.menu-login</string>
<key>BundleProgram</key><string>Contents/Helpers/AgentDockLoginHelper</string>
<key>ProgramArguments</key><array><string>AgentDockLoginHelper</string></array>
<key>RunAtLoad</key><true/>
<key>LimitLoadToSessionType</key><string>Aqua</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(launchAgentsDir, "com.uvwt.agentdock.menu-login.plist"), []byte(menuAgentPlist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>AgentDock</string>
<key>CFBundleIdentifier</key><string>com.uvwt.agentdock</string>
<key>CFBundleName</key><string>AgentDock</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>` + version + `</string>
<key>CFBundleVersion</key><string>` + version + `</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "com.uvwt.agentdock.login-helper", filepath.Join(helpersDir, "AgentDockLoginHelper"))
	runTestCommand(t, "/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "com.uvwt.agentdock.core", filepath.Join(helpersDir, "agentdock"))
	runTestCommand(t, "/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "com.uvwt.agentdock.cloudflared", filepath.Join(helpersDir, "cloudflared"))
	if includeArbiter {
		runTestCommand(t, "/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "com.uvwt.agentdock.arbiter", filepath.Join(helpersDir, "agentdock-arbiter"))
	}
	runTestCommand(t, "/usr/bin/codesign", "--force", "--deep", "--sign", "-", "--identifier", "com.uvwt.agentdock", appPath)
	if err := validateMacOSDesktopVersion(context.Background(), appPath, version); err != nil {
		t.Fatal(err)
	}
	return appPath
}

func assertMacOSAppVersion(t *testing.T, appPath, version string) {
	t.Helper()
	if err := validateMacOSDesktopVersion(context.Background(), appPath, version); err != nil {
		t.Fatal(err)
	}
}

func runTestCommand(t *testing.T, path string, args ...string) {
	t.Helper()
	output, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v: %s", path, args, err, strings.TrimSpace(string(output)))
	}
}
