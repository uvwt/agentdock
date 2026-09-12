package scripts

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestScriptGovernanceInventoryCoversTrackedScripts(t *testing.T) {
	root := filepath.Join("..", "..")
	inventoryPath := filepath.Join(root, "scripts", "governance", "inventory.yaml")
	inventory, err := parseGovernanceInventory(inventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Threshold <= 0 {
		t.Fatal("review_threshold_lines is required")
	}

	listed := map[string]governanceScript{}
	for _, script := range inventory.Scripts {
		if script.Path == "" || script.Class == "" {
			t.Fatalf("inventory entry missing path or class: %+v", script)
		}
		listed[filepath.ToSlash(script.Path)] = script
	}

	tracked, err := gitTrackedScripts(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range tracked {
		entry, ok := listed[path]
		if !ok {
			t.Fatalf("tracked script is missing from scripts/governance/inventory.yaml: %s", path)
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		lines, err := countLines(full)
		if err != nil {
			t.Fatal(err)
		}
		if lines > inventory.Threshold && !entry.LegacyOversize && strings.TrimSpace(entry.OversizeJustification) == "" {
			t.Fatalf("%s has %d lines (> %d) and needs oversize_justification or legacy_oversize", path, lines, inventory.Threshold)
		}
		if entry.Class == "legacy" && !entry.LegacyOversize && lines > inventory.Threshold {
			t.Fatalf("legacy script %s must be marked legacy_oversize", path)
		}
	}
	for path := range listed {
		found := false
		for _, trackedPath := range tracked {
			if trackedPath == path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("inventory lists a script that is not tracked: %s", path)
		}
	}
}

func TestManageWindowsActionsStayFrozen(t *testing.T) {
	root := filepath.Join("..", "..")
	inventory, err := parseGovernanceInventory(filepath.Join(root, "scripts", "governance", "inventory.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	frozen := inventory.Frozen["manage-windows.ps1"]
	if len(frozen) == 0 {
		t.Fatal("frozen manage-windows.ps1 actions are required")
	}
	data, err := os.ReadFile(filepath.Join(root, "scripts", "install", "manage-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	actual := parsePowerShellValidateSetActions(script)
	if !sameStringSet(actual, frozen) {
		t.Fatalf("manage-windows.ps1 ValidateSet actions %v must equal frozen set %v", actual, frozen)
	}
	if !strings.Contains(script, "Compatibility shim") && !strings.Contains(script, "兼容垫片") {
		t.Fatal("manage-windows.ps1 must declare itself as a compatibility shim")
	}
}

func parsePowerShellValidateSetActions(script string) []string {
	start := strings.Index(script, "ValidateSet(")
	if start < 0 {
		return nil
	}
	rest := script[start:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return nil
	}
	var actions []string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(strings.Trim(line, ","))
		if strings.HasPrefix(line, "'") && strings.HasSuffix(line, "'") {
			actions = append(actions, strings.Trim(line, "'"))
		}
	}
	return actions
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	count := map[string]int{}
	for _, value := range left {
		count[value]++
	}
	for _, value := range right {
		if count[value] == 0 {
			return false
		}
		count[value]--
	}
	return true
}

func TestNewCodeMustNotAddManageWindowsCallers(t *testing.T) {
	root := filepath.Join("..", "..")
	inventory, err := parseGovernanceInventory(filepath.Join(root, "scripts", "governance", "inventory.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	allow := map[string]struct{}{}
	for _, path := range inventory.Allowlist {
		allow[filepath.ToSlash(path)] = struct{}{}
	}
	cmd := exec.Command("git", "-C", root, "grep", "-l", "manage-windows.ps1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		path := filepath.ToSlash(strings.TrimSpace(line))
		if path == "" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		if _, ok := allow[path]; !ok {
			t.Fatalf("new manage-windows.ps1 caller is forbidden: %s", path)
		}
	}
}

type governanceInventory struct {
	Threshold int
	Frozen    map[string][]string
	Allowlist []string
	Scripts   []governanceScript
}

type governanceScript struct {
	Path                  string
	Class                 string
	LegacyOversize        bool
	OversizeJustification string
}

func parseGovernanceInventory(path string) (governanceInventory, error) {
	file, err := os.Open(path)
	if err != nil {
		return governanceInventory{}, err
	}
	defer file.Close()

	inventory := governanceInventory{Frozen: map[string][]string{}}
	scanner := bufio.NewScanner(file)
	section := ""
	current := governanceScript{}
	flush := func() {
		if current.Path != "" {
			inventory.Scripts = append(inventory.Scripts, current)
			current = governanceScript{}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "review_threshold_lines:"):
			inventory.Threshold, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "review_threshold_lines:")))
			section = ""
		case line == "frozen_legacy_actions:":
			flush()
			section = "frozen"
		case line == "manage_windows_allowlist:":
			flush()
			section = "allow"
		case line == "scripts:":
			flush()
			section = "scripts"
		case section == "frozen" && strings.HasSuffix(line, ":"):
			section = "frozen-item:" + strings.TrimSuffix(line, ":")
		case strings.HasPrefix(section, "frozen-item:") && strings.HasPrefix(line, "- "):
			key := strings.TrimPrefix(section, "frozen-item:")
			inventory.Frozen[key] = append(inventory.Frozen[key], strings.Trim(strings.TrimPrefix(line, "- "), `"'`))
		case section == "allow" && strings.HasPrefix(line, "- "):
			inventory.Allowlist = append(inventory.Allowlist, strings.Trim(strings.TrimPrefix(line, "- "), `"'`))
		case section == "scripts" && strings.HasPrefix(line, "- path:"):
			flush()
			current.Path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "- path:")), `"'`)
		case section == "scripts" && strings.HasPrefix(line, "class:"):
			current.Class = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "class:")), `"'`)
		case section == "scripts" && strings.HasPrefix(line, "legacy_oversize:"):
			current.LegacyOversize = strings.TrimSpace(strings.TrimPrefix(line, "legacy_oversize:")) == "true"
		case section == "scripts" && strings.HasPrefix(line, "oversize_justification:"):
			current.OversizeJustification = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "oversize_justification:")), `"'`)
		}
	}
	flush()
	return inventory, scanner.Err()
}

func gitTrackedScripts(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "*.sh", "*.ps1", "*.py", "*.iss")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		path := filepath.ToSlash(strings.TrimSpace(line))
		if path == "" {
			continue
		}
		if strings.HasPrefix(path, "scripts/") || strings.HasPrefix(path, "packaging/") || path == "docker-entrypoint.sh" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	return strings.Count(string(data), "\n") + 1, nil
}
