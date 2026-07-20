package skillbundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/skills"
	"github.com/uvwt/agentdock/internal/skillstate"
)

const ManifestFile = "manifest.json"

type Manifest struct {
	Skills []ManifestSkill `json:"skills"`
}

type ManifestSkill struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Digest  string `json:"digest"`
}

type Result struct {
	Skills []InstalledSkill `json:"skills"`
}

type InstalledSkill struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type candidate struct {
	manifest ManifestSkill
	path     string
	existed  bool
}

// Bootstrap 安装并激活 Release 随附的 Skill Bundle。
// 包校验、安装、激活和内置清单提交按顺序执行，失败时恢复原选择并清理本次新增版本。
func Bootstrap(ctx context.Context, state *skillstate.Store, manager *skills.Manager, bundleDir string) (Result, error) {
	if state == nil {
		return Result{}, errors.New("skill state store is required")
	}
	if manager == nil {
		return Result{}, errors.New("skill manager is required")
	}
	root, manifest, err := loadManifest(bundleDir)
	if err != nil {
		return Result{}, err
	}
	candidates, err := validateBundle(ctx, state, manager, root, manifest)
	if err != nil {
		return Result{}, err
	}

	snapshots := make(map[string]skillstate.Selection, len(candidates))
	for _, item := range candidates {
		snapshot, err := state.Snapshot(item.manifest.Name)
		if err != nil {
			return Result{}, err
		}
		snapshots[item.manifest.Name] = snapshot
	}

	installed := make([]candidate, 0, len(candidates))
	results := make([]InstalledSkill, 0, len(candidates))
	for _, item := range candidates {
		result, err := manager.Install(ctx, skills.InstallRequest{
			Source:       item.path,
			DigestSHA256: item.manifest.Digest,
			Activate:     false,
		})
		if err != nil {
			cleanupErr := cleanupNewVersions(context.WithoutCancel(ctx), state, installed)
			return Result{}, errors.Join(fmt.Errorf("install bundled skill %s: %w", item.manifest.Name, err), cleanupErr)
		}
		installed = append(installed, item)
		results = append(results, InstalledSkill{Name: result.Skill, Version: result.Version, Digest: result.Digest})
	}

	activated := make([]candidate, 0, len(candidates))
	for _, item := range candidates {
		if err := state.Activate(ctx, item.manifest.Name, item.manifest.Version); err != nil {
			rollbackErr := rollbackBootstrap(context.WithoutCancel(ctx), state, activated, installed, snapshots)
			return Result{}, errors.Join(fmt.Errorf("activate bundled skill %s: %w", item.manifest.Name, err), rollbackErr)
		}
		activated = append(activated, item)
	}

	names := make([]string, 0, len(candidates))
	for _, item := range candidates {
		names = append(names, item.manifest.Name)
	}
	if err := state.ReplaceBundledSkills(ctx, names); err != nil {
		rollbackErr := rollbackBootstrap(context.WithoutCancel(ctx), state, activated, installed, snapshots)
		return Result{}, errors.Join(fmt.Errorf("commit bundled skill list: %w", err), rollbackErr)
	}
	return Result{Skills: results}, nil
}

func loadManifest(bundleDir string) (string, Manifest, error) {
	root, err := filepath.Abs(strings.TrimSpace(bundleDir))
	if err != nil {
		return "", Manifest{}, fmt.Errorf("resolve skill bundle: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", Manifest{}, fmt.Errorf("stat skill bundle: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", Manifest{}, errors.New("skill bundle must be a regular directory")
	}

	manifestPath := filepath.Join(root, ManifestFile)
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return "", Manifest{}, fmt.Errorf("stat skill bundle manifest: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 {
		return "", Manifest{}, errors.New("skill bundle manifest must be a regular file")
	}
	if manifestInfo.Size() > 1<<20 {
		return "", Manifest{}, errors.New("skill bundle manifest exceeds 1 MiB")
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return "", Manifest{}, fmt.Errorf("open skill bundle manifest: %w", err)
	}
	defer file.Close()

	var manifest Manifest
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return "", Manifest{}, fmt.Errorf("decode skill bundle manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", Manifest{}, err
	}
	if len(manifest.Skills) == 0 {
		return "", Manifest{}, errors.New("skill bundle manifest has no skills")
	}
	return root, manifest, nil
}

func validateBundle(ctx context.Context, state *skillstate.Store, manager *skills.Manager, root string, manifest Manifest) ([]candidate, error) {
	seenNames := make(map[string]struct{}, len(manifest.Skills))
	seenPaths := make(map[string]struct{}, len(manifest.Skills))
	items := make([]candidate, 0, len(manifest.Skills))
	for _, entry := range manifest.Skills {
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Version = strings.TrimSpace(entry.Version)
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Digest = strings.TrimSpace(entry.Digest)
		if entry.Name == "" || entry.Version == "" || entry.Path == "" || entry.Digest == "" {
			return nil, errors.New("each bundled skill requires name, version, path, and digest")
		}
		if _, exists := seenNames[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate bundled skill %q", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}

		packageDir, err := resolvePackagePath(root, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve bundled skill %s: %w", entry.Name, err)
		}
		if _, exists := seenPaths[packageDir]; exists {
			return nil, fmt.Errorf("duplicate bundled skill path %q", entry.Path)
		}
		seenPaths[packageDir] = struct{}{}

		validated, err := manager.Validate(ctx, skills.ValidateRequest{Source: packageDir, DigestSHA256: entry.Digest})
		if err != nil {
			return nil, fmt.Errorf("validate bundled skill %s: %w", entry.Name, err)
		}
		if !validated.Valid {
			return nil, fmt.Errorf("validate bundled skill %s: %v", entry.Name, validated.Issues)
		}
		if validated.Document.Name != entry.Name || validated.Document.Version != entry.Version {
			return nil, fmt.Errorf("bundled skill %s manifest identity does not match SKILL.md", entry.Name)
		}
		existed, err := state.IsInstalled(entry.Name, entry.Version)
		if err != nil {
			return nil, err
		}
		items = append(items, candidate{manifest: entry, path: packageDir, existed: existed})
	}
	return items, nil
}

func resolvePackagePath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("skill path must stay inside the bundle")
	}
	path := filepath.Join(root, clean)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", errors.New("skill package must be a regular file or directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("skill path resolves outside the bundle")
	}
	return resolvedPath, nil
}

func rollbackBootstrap(ctx context.Context, state *skillstate.Store, activated, installed []candidate, snapshots map[string]skillstate.Selection) error {
	var rollbackErrors []error
	for index := len(activated) - 1; index >= 0; index-- {
		name := activated[index].manifest.Name
		if err := state.RestoreSelection(ctx, name, snapshots[name]); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s selection: %w", name, err))
		}
	}
	if err := cleanupNewVersions(ctx, state, installed); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	return errors.Join(rollbackErrors...)
}

func cleanupNewVersions(ctx context.Context, state *skillstate.Store, installed []candidate) error {
	var cleanupErrors []error
	for index := len(installed) - 1; index >= 0; index-- {
		item := installed[index]
		if item.existed {
			continue
		}
		if err := state.RemoveVersion(ctx, item.manifest.Name, item.manifest.Version); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove bundled skill %s version %s: %w", item.manifest.Name, item.manifest.Version, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing skill bundle data: %w", err)
	}
	return errors.New("skill bundle manifest contains multiple JSON values")
}
