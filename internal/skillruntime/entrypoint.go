package skillruntime

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	RuntimeBinary     = "binary"
	RuntimePython     = "python"
	RuntimeNode       = "node"
	RuntimePowerShell = "powershell"
)

func entrypointCommand(manifest Manifest, entrypoint string) (*exec.Cmd, error) {
	switch strings.ToLower(strings.TrimSpace(manifest.Spec.Runtime)) {
	case "", RuntimeBinary:
		return exec.Command(entrypoint), nil
	case RuntimePython:
		path, err := lookPathCandidates(pythonCandidates())
		if err != nil {
			return nil, fmt.Errorf("resolve Python runtime: %w", err)
		}
		return exec.Command(path, entrypoint), nil
	case RuntimeNode:
		path, err := lookPathCandidates([]string{"node", "node.exe"})
		if err != nil {
			return nil, fmt.Errorf("resolve Node.js runtime: %w", err)
		}
		return exec.Command(path, entrypoint), nil
	case RuntimePowerShell:
		path, err := lookPathCandidates(powershellCandidates())
		if err != nil {
			return nil, fmt.Errorf("resolve PowerShell runtime: %w", err)
		}
		script := powershellSkillUTF8Prefix + "& '" + strings.ReplaceAll(entrypoint, "'", "''") + "'"
		return exec.Command(path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script), nil
	default:
		return nil, fmt.Errorf("unsupported Skill runtime %q", manifest.Spec.Runtime)
	}
}

const powershellSkillUTF8Prefix = "try { [Console]::InputEncoding=[System.Text.UTF8Encoding]::new($false); [Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $OutputEncoding=[System.Text.UTF8Encoding]::new($false) } catch {}; "

func applyRuntimeEnvironment(manifest Manifest, environment []string) []string {
	if strings.EqualFold(strings.TrimSpace(manifest.Spec.Runtime), RuntimePython) {
		environment = upsertEnvironment(environment, "PYTHONUTF8", "1")
		environment = upsertEnvironment(environment, "PYTHONIOENCODING", "utf-8")
	}
	return environment
}

func upsertEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	for index, item := range environment {
		if strings.HasPrefix(strings.ToUpper(item), strings.ToUpper(prefix)) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

func pythonCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{"python.exe", "python"}
	}
	return []string{"python3", "python"}
}

func powershellCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{"pwsh.exe", "powershell.exe"}
	}
	return []string{"pwsh"}
}

func lookPathCandidates(candidates []string) (string, error) {
	var checked []string
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		checked = append(checked, candidate)
	}
	return "", fmt.Errorf("none of %s are available in PATH", strings.Join(checked, ", "))
}
