package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

const ManifestFile = "manifest.json"

type Manifest struct {
	Skills []ManifestSkill `json:"skills"`
}

type ManifestSkill struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type Result struct {
	Skills []InstalledSkill `json:"skills"`
}

type InstalledSkill struct {
	Name          string `json:"name"`
	ContentDigest string `json:"content_digest"`
	Changed       bool   `json:"changed"`
}

type candidate struct {
	manifest   ManifestSkill
	path       string
	existed    bool
	backupPath string
	changed    bool
}

// Bootstrap installs the release Skill bundle as current managed content.
// Temporary backups exist only for this transaction and are removed after commit.
func Bootstrap(ctx context.Context, state *skillstate.Store, manager *skills.Manager, bundleDir string) (Result, error) {
	if state == nil {
		return Result{}, errors.New("managed Skill store is required")
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

	transactionRoot, err := state.TempPath("bundle")
	if err != nil {
		return Result{}, fmt.Errorf("create bundled Skill transaction: %w", err)
	}
	defer os.RemoveAll(transactionRoot)

	for index := range candidates {
		item := &candidates[index]
		if !item.existed {
			continue
		}
		release, err := state.AcquireRead(ctx, item.manifest.Name)
		if err != nil {
			return Result{}, fmt.Errorf("lock bundled skill %s for snapshot: %w", item.manifest.Name, err)
		}
		current, resolveErr := state.Resolve(item.manifest.Name)
		if resolveErr == nil {
			item.backupPath = filepath.Join(transactionRoot, fmt.Sprintf("%03d-%s", index, item.manifest.Name))
			resolveErr = copyDirectory(current, item.backupPath)
		}
		release()
		if resolveErr != nil {
			return Result{}, fmt.Errorf("snapshot bundled skill %s: %w", item.manifest.Name, resolveErr)
		}
	}

	results := make([]InstalledSkill, 0, len(candidates))
	for index := range candidates {
		item := &candidates[index]
		installed, err := manager.Install(ctx, skills.InstallRequest{
			Source:       item.path,
			DigestSHA256: item.manifest.Digest,
		})
		if err != nil {
			rollbackErr := rollbackBundle(context.WithoutCancel(ctx), manager, candidates[:index])
			return Result{}, errors.Join(fmt.Errorf("install bundled skill %s: %w", item.manifest.Name, err), rollbackErr)
		}
		item.changed = installed.Changed
		results = append(results, InstalledSkill{
			Name: installed.Skill, ContentDigest: installed.ContentDigest, Changed: installed.Changed,
		})
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
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Digest = strings.TrimSpace(entry.Digest)
		if entry.Name == "" || entry.Path == "" || entry.Digest == "" {
			return nil, errors.New("each bundled skill requires name, path, and digest")
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
		if validated.Document.Name != entry.Name {
			return nil, fmt.Errorf("bundled skill %s manifest identity does not match SKILL.md", entry.Name)
		}
		existed, err := state.IsInstalled(entry.Name)
		if err != nil {
			return nil, err
		}
		items = append(items, candidate{manifest: entry, path: packageDir, existed: existed})
	}
	return items, nil
}

func rollbackBundle(ctx context.Context, manager *skills.Manager, installed []candidate) error {
	var rollbackErrors []error
	for index := len(installed) - 1; index >= 0; index-- {
		item := installed[index]
		if !item.changed {
			continue
		}
		if item.existed {
			if _, err := manager.Install(ctx, skills.InstallRequest{Source: item.backupPath}); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore bundled skill %s: %w", item.manifest.Name, err))
			}
			continue
		}
		if _, err := manager.Remove(ctx, item.manifest.Name); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove newly installed bundled skill %s: %w", item.manifest.Name, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func copyDirectory(source, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in managed Skill: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()&0o755)
	})
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

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing skill bundle data: %w", err)
	}
	return errors.New("skill bundle manifest contains multiple JSON values")
}
