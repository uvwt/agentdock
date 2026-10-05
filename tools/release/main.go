package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("用法：release <catalog|version|component-version|component-catalog|release-metadata|release-kind|core-version|verify-dist|checksum|mirror-manifest> [参数]")
	}
	switch args[0] {
	case "version":
		if len(args) != 2 {
			return errors.New("用法：release version <tag>")
		}
		metadata, err := releaseMetadataForTag(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, metadata.Version)
		return nil
	case "component-version":
		if len(args) != 2 {
			return errors.New("用法：release component-version <agentdock-version>")
		}
		entry, err := currentCloudflaredCatalogEntry(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, entry.Version)
		return nil
	case "component-catalog":
		if len(args) != 1 {
			return errors.New("用法：release component-catalog")
		}
		return writeCloudflaredComponentCatalog(stdout)
	case "release-metadata":
		if len(args) != 2 {
			return errors.New("用法：release release-metadata <tag>")
		}
		return writeReleaseMetadata(args[1], stdout)
	case "release-kind":
		if len(args) != 2 {
			return errors.New("用法：release release-kind <tag>")
		}
		metadata, err := releaseMetadataForTag(args[1])
		if err != nil {
			return err
		}
		if metadata.Prerelease {
			fmt.Fprintln(stdout, "prerelease")
		} else {
			fmt.Fprintln(stdout, "stable")
		}
		return nil
	case "core-version":
		if len(args) != 2 {
			return errors.New("用法：release core-version <version>")
		}
		metadata, err := releaseMetadataForTag("v" + strings.TrimPrefix(strings.TrimSpace(args[1]), "v"))
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, metadata.Core)
		return nil
	case "catalog":
		return json.NewEncoder(stdout).Encode(ReleaseCatalog())
	case "verify-dist":
		if len(args) != 2 {
			return errors.New("用法：release verify-dist <目录>")
		}
		return verifyDist(args[1], stdout)
	case "checksum":
		if len(args) != 2 {
			return errors.New("用法：release checksum <文件>")
		}
		sum, err := fileSHA256(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s  %s\n", sum, filepath.Base(args[1]))
		return nil
	case "mirror-manifest":
		if len(args) != 4 {
			return errors.New("用法：release mirror-manifest <tag> <public-base-url> <dist-dir>")
		}
		return writeMirrorManifest(args[1], args[2], args[3], stdout)
	default:
		return fmt.Errorf("未知命令：%s", args[0])
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
