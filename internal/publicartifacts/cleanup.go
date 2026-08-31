package publicartifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s Store) Cleanup(now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return fmt.Errorf("create public artifacts root: %w", err)
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	var cleanupErrs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.Root, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("inspect artifact directory %s: %w", dir, statErr))
			continue
		}
		meta, metaErr := readMetadataPath(filepath.Join(dir, "metadata.json"))
		payloadInfo, payloadErr := os.Stat(filepath.Join(dir, "payload"))
		oldBroken := now.Sub(info.ModTime()) > 24*time.Hour
		remove := metaErr == nil && meta.ExpiresAt.Before(now) ||
			metaErr != nil && oldBroken ||
			payloadErr != nil && oldBroken ||
			payloadErr == nil && !payloadInfo.Mode().IsRegular() && oldBroken
		if remove {
			if err := os.RemoveAll(dir); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("remove artifact directory %s: %w", dir, err))
			}
		}
	}
	return errors.Join(cleanupErrs...)
}

func (s Store) resolveArtifact(id string) (Metadata, string, error) {
	// Artifact IDs are generated as 16 random bytes encoded as lowercase hex.
	// IsLocal plus the hex whitelist rejects absolute and traversing identifiers;
	// os.Root below additionally prevents symlink traversal from escaping Root.
	if !filepath.IsLocal(id) || !validArtifactID(id) {
		return Metadata{}, "", errors.New("invalid artifact id")
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return Metadata{}, "", err
	}
	defer root.Close()
	metadataPath := filepath.Join(id, "metadata.json")
	data, err := root.ReadFile(metadataPath)
	if err != nil {
		return Metadata{}, "", err
	}
	meta, err := decodeMetadata(data)
	if err != nil {
		return Metadata{}, "", err
	}
	if meta.ArtifactID != id {
		return Metadata{}, "", errors.New("artifact metadata id does not match path")
	}
	return meta, filepath.Join(id, "payload"), nil
}

func validArtifactID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if (id[i] < '0' || id[i] > '9') && (id[i] < 'a' || id[i] > 'f') {
			return false
		}
	}
	return true
}

func readMetadataPath(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	return decodeMetadata(data)
}

func decodeMetadata(data []byte) (Metadata, error) {
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, err
	}
	if meta.ArtifactID == "" || meta.Filename == "" || meta.SHA256 == "" || meta.Size < 0 || meta.ExpiresAt.IsZero() {
		return Metadata{}, errors.New("invalid metadata")
	}
	return meta, nil
}
