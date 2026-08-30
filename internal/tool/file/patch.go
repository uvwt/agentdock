package file

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workspacepkg "github.com/uvwt/agentdock/internal/workspace"
)

func patchPathInBase(basePath, rawPath string) (string, error) {
	cleanRaw, err := workspacepkg.Clean(rawPath)
	if err != nil {
		return "", err
	}
	cleanBase, err := workspacepkg.Clean(basePath)
	if err != nil {
		return "", err
	}
	if cleanBase == "." {
		return cleanRaw, nil
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(cleanBase), filepath.FromSlash(cleanRaw))), nil
}

func (svc *Service) applyEnvelopePatch(patch string, dryRun bool, basePath string) (Result, error) {
	operations, err := parseEnvelopePatch(patch)
	if err != nil {
		return nil, err
	}
	staged := map[string]stagedPatchFile{}
	affected := make([]map[string]any, 0)
	summaries := make([]string, 0)

	for _, op := range operations {
		switch op.Kind {
		case "add":
			targetPath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			target, err := svc.ws.ResolveForWrite(targetPath)
			if err != nil {
				return nil, err
			}
			if target.Exists {
				return nil, toolError("PATCH_FAILED", "cannot add file that already exists", "validation")
			}
			if err := ensurePatchPathUnused(staged, target.Abs, target.Display); err != nil {
				return nil, err
			}
			content := op.AddContent
			staged[target.Abs] = stagedPatchFile{Abs: target.Abs, Display: target.Display, Content: &content, Mode: 0o644}
			affected = append(affected, map[string]any{"path": target.Display, "operation": "add"})
			summaries = append(summaries, "A "+target.Display)
		case "delete":
			targetPath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			target, err := svc.ws.ResolveExisting(targetPath)
			if err != nil {
				return nil, err
			}
			if err := ensurePatchPathUnused(staged, target.Abs, target.Display); err != nil {
				return nil, err
			}
			info, original, err := readPatchFile(target.Abs)
			if err != nil {
				return nil, err
			}
			staged[target.Abs] = stagedPatchFile{Abs: target.Abs, Display: target.Display, Mode: info.Mode().Perm(), Original: original, OriginalExists: true}
			affected = append(affected, map[string]any{"path": target.Display, "operation": "delete"})
			summaries = append(summaries, "D "+target.Display)
		case "update":
			sourcePath, err := patchPathInBase(basePath, op.Path)
			if err != nil {
				return nil, err
			}
			source, err := svc.ws.ResolveExisting(sourcePath)
			if err != nil {
				return nil, err
			}
			current, exists := staged[source.Abs]
			if exists && current.Content == nil {
				return nil, toolError("PATCH_FAILED", "cannot update a deleted file", "validation")
			}
			if !exists {
				info, original, err := readPatchFile(source.Abs)
				if err != nil {
					return nil, err
				}
				content := string(original)
				current = stagedPatchFile{Abs: source.Abs, Display: source.Display, Content: &content, Mode: info.Mode().Perm(), Original: original, OriginalExists: true}
			}
			updated, err := applyUpdateHunks(*current.Content, op.Hunks, source.Display)
			if err != nil {
				return nil, err
			}
			if op.MoveTo == "" {
				current.Content = &updated
				staged[source.Abs] = current
				affected = append(affected, map[string]any{"path": source.Display, "operation": "update"})
				summaries = append(summaries, "M "+source.Display)
				continue
			}

			destPath, err := patchPathInBase(basePath, op.MoveTo)
			if err != nil {
				return nil, err
			}
			dest, err := svc.ws.ResolveForWrite(destPath)
			if err != nil {
				return nil, err
			}
			if dest.Abs == source.Abs {
				current.Content = &updated
				staged[source.Abs] = current
				affected = append(affected, map[string]any{"path": source.Display, "operation": "update"})
				summaries = append(summaries, "M "+source.Display)
				continue
			}
			if dest.Exists {
				return nil, toolError("PATCH_FAILED", "cannot move over an existing file", "validation")
			}
			if err := ensurePatchPathUnused(staged, dest.Abs, dest.Display); err != nil {
				return nil, err
			}
			staged[source.Abs] = stagedPatchFile{Abs: source.Abs, Display: source.Display, Mode: current.Mode, Original: current.Original, OriginalExists: true}
			staged[dest.Abs] = stagedPatchFile{Abs: dest.Abs, Display: dest.Display, Content: &updated, Mode: current.Mode}
			affected = append(affected, map[string]any{"path": source.Display, "operation": "move", "move_to": dest.Display})
			summaries = append(summaries, "R "+source.Display+" -> "+dest.Display)
		}
	}
	if len(affected) == 0 {
		return nil, toolError("PATCH_FAILED", "no files were modified", "validation")
	}
	diffPreview, diffTruncated, stats, err := stagedDiffPreview(staged, 65536)
	if err != nil {
		return nil, err
	}
	if !dryRun {
		if err := commitStagedPatch(staged); err != nil {
			return nil, err
		}
	}
	return Result{"dry_run": dryRun, "workdir": basePath, "affected_files": affected, "summary": strings.Join(summaries, "\n"), "diff_preview": diffPreview, "truncated": diffTruncated, "files_changed": stats.FilesChanged, "insertions": stats.Insertions, "deletions": stats.Deletions}, nil
}

func ensurePatchPathUnused(staged map[string]stagedPatchFile, absPath, displayPath string) error {
	if _, exists := staged[absPath]; !exists {
		return nil
	}
	return toolErrorDetails(
		"PATCH_FAILED",
		"patch contains conflicting operations for the same path",
		"validation",
		map[string]any{"path": displayPath},
	)
}

func readPatchFile(path string) (os.FileInfo, []byte, error) {
	read, err := readBoundedFile(path, int64(maxTextFileReadBytes))
	if err != nil {
		return nil, nil, err
	}
	if read.Info.IsDir() {
		return nil, nil, toolError("PATCH_FAILED", "cannot patch a directory", "validation")
	}
	if read.TooLarge {
		return nil, nil, toolErrorDetails(
			"FILE_TOO_LARGE",
			"patch target exceeds the text file input limit",
			"validation",
			map[string]any{"path": path, "size_bytes": read.Size, "max_size_bytes": maxTextFileReadBytes},
		)
	}
	if looksBinary(read.Data) {
		return nil, nil, toolError("BINARY_FILE", "binary file patch blocked for text tool", "validation")
	}
	if !utf8.Valid(read.Data) {
		return nil, nil, toolError("ENCODING_UNSUPPORTED", "file is not valid utf-8", "validation")
	}
	return read.Info, read.Data, nil
}
