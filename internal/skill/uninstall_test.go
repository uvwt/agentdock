package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestUninstallRejectsBundledSkill(t *testing.T) {
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	packagePath, err := state.InstalledPath("demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(packagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.ReplaceBundledSkills(context.Background(), []string{"demo"}); err != nil {
		t.Fatal(err)
	}

	_, err = manager.Uninstall(context.Background(), "demo", "")
	var packageErr *Error
	if !errors.As(err, &packageErr) || packageErr.Code != ErrUninstallFailed || packageErr.Stage != "uninstall.bundled" {
		t.Fatalf("bundled uninstall error = %#v", err)
	}
	if installed, checkErr := state.IsInstalled("demo", "1.0.0"); checkErr != nil || !installed {
		t.Fatalf("bundled Skill changed after rejected uninstall: installed=%v err=%v", installed, checkErr)
	}
}
