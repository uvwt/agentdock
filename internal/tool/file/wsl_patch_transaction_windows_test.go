//go:build windows

package file

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/workspace"
)

func newWSLPatchIntegrationService(t *testing.T) (*Service, fileRuntimeSelection, string) {
	t.Helper()
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		t.Skip("wsl.exe is required for WSL patch integration tests")
	}
	helperPath := strings.TrimSpace(os.Getenv(wslHelperOverrideEnv))
	if helperPath == "" {
		t.Skipf("%s must point to an explicitly built Go helper for WSL integration tests", wslHelperOverrideEnv)
	}
	if output, err := exec.Command(wslPath, "--exec", helperPath, "--protocol-version").CombinedOutput(); err != nil {
		t.Skipf("Go WSL helper is not runnable in the default distribution: %v (%s)", err, output)
	}

	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(ws, nil, func(string, map[string]string) ([]string, error) {
		return os.Environ(), nil
	})
	selection := fileRuntimeSelection{Runtime: "wsl"}
	root := fmt.Sprintf("/tmp/agentdock-wsl-patch-e2e-%d", time.Now().UnixNano())
	if output, err := exec.Command(wslPath, "--exec", "mkdir", "-p", root).CombinedOutput(); err != nil {
		t.Fatalf("create WSL integration root: %v (%s)", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command(wslPath, "--exec", "rm", "-rf", root).Run()
	})
	return service, selection, root
}

func writeWSLPatchFixture(t *testing.T, service *Service, selection fileRuntimeSelection, target, content string, mode int) {
	t.Helper()
	if _, err := service.callWSLFileHelper(context.Background(), selection, map[string]any{
		"action": "write_atomic", "path": target, "content": content, "mode": mode,
	}); err != nil {
		t.Fatalf("write WSL fixture %s: %#v", target, err)
	}
}

func readWSLPatchFixture(t *testing.T, service *Service, selection fileRuntimeSelection, target string, allowMissing bool) Result {
	t.Helper()
	result, err := service.callWSLFileHelper(context.Background(), selection, map[string]any{
		"action": "read", "path": target, "reject_symlink": true, "allow_missing": allowMissing,
	})
	if err != nil {
		t.Fatalf("read WSL fixture %s: %v", target, err)
	}
	return result
}

func TestWSLFileEditPatchMultiFileIntegration(t *testing.T) {
	service, selection, root := newWSLPatchIntegrationService(t)
	writeWSLPatchFixture(t, service, selection, path.Join(root, "update.txt"), "old update\n", 0o640)
	writeWSLPatchFixture(t, service, selection, path.Join(root, "delete.txt"), "delete me\n", 0o600)
	writeWSLPatchFixture(t, service, selection, path.Join(root, "move.txt"), "move me\n", 0o750)
	moveBefore := readWSLPatchFixture(t, service, selection, path.Join(root, "move.txt"), false)

	patch := `*** Begin Patch
*** Update File: update.txt
@@
-old update
+new update
*** Delete File: delete.txt
*** Add File: add.txt
+added
*** Update File: move.txt
*** Move to: moved.txt
@@
-move me
+move changed
*** End Patch`
	result, err := service.Edit(context.Background(), EditRequest{
		RuntimeOptions: RuntimeOptions{Runtime: "wsl"},
		Action:         "patch",
		Workdir:        root,
		Patch:          patch,
	})
	if err != nil {
		t.Fatalf("multi-file WSL patch failed: %v", err)
	}
	if result["action"] != "patch" || result["runtime"] != "wsl" || result["dry_run"] != false {
		t.Fatalf("unexpected patch result: %#v", result)
	}

	for name, want := range map[string]string{
		"update.txt": "new update\n",
		"add.txt":    "added\n",
		"moved.txt":  "move changed\n",
	} {
		loaded := readWSLPatchFixture(t, service, selection, path.Join(root, name), false)
		if loaded["content"] != want {
			t.Fatalf("%s content = %#v, want %q", name, loaded["content"], want)
		}
	}
	moved := readWSLPatchFixture(t, service, selection, path.Join(root, "moved.txt"), false)
	if resultInt(moved, "mode") != 0o750 {
		t.Fatalf("moved mode = %o, want 750", resultInt(moved, "mode"))
	}
	if resultInt(moved, "uid") != resultInt(moveBefore, "uid") || resultInt(moved, "gid") != resultInt(moveBefore, "gid") {
		t.Fatalf(
			"moved owner = %d:%d, want %d:%d",
			resultInt(moved, "uid"), resultInt(moved, "gid"), resultInt(moveBefore, "uid"), resultInt(moveBefore, "gid"),
		)
	}
	for _, name := range []string{"delete.txt", "move.txt"} {
		loaded := readWSLPatchFixture(t, service, selection, path.Join(root, name), true)
		if exists, _ := loaded["exists"].(bool); exists {
			t.Fatalf("%s still exists after patch: %#v", name, loaded)
		}
	}
}
