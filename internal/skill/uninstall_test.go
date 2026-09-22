package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveDeletesCurrentManagedPackage(t *testing.T) {
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "demo-skill", "Demo", nil)
	installed, err := manager.Install(context.Background(), InstallRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Remove(context.Background(), "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Removed || !result.PreservedEnvironment || !result.PreservedData {
		t.Fatalf("unexpected remove result: %#v", result)
	}
	if _, err := os.Stat(installed.Path); !os.IsNotExist(err) {
		t.Fatalf("managed package still exists after remove: %v", err)
	}
}

func TestRemoveMissingSkillFails(t *testing.T) {
	manager := newManagerForTest(t)
	if _, err := manager.Remove(context.Background(), "missing-skill"); err == nil {
		t.Fatal("Remove() succeeded for missing Skill")
	}
}

func TestRemoveWaitsForActiveReader(t *testing.T) {
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "demo-skill", "Demo", nil)
	if _, err := manager.Install(context.Background(), InstallRequest{Source: source}); err != nil {
		t.Fatal(err)
	}
	release, err := manager.State.AcquireRead(context.Background(), "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := manager.Remove(context.Background(), "demo-skill")
		done <- err
	}()
	select {
	case err := <-done:
		release()
		t.Fatalf("remove completed while reader was active: %v", err)
	default:
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(manager.State.Root(), "demo-skill")); !os.IsNotExist(err) {
		t.Fatalf("Skill remained after reader release: %v", err)
	}
}
