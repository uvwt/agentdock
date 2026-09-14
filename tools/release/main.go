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

	"github.com/uvwt/agentdock/internal/buildinfo"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("用法：release <catalog|version|verify-version|verify-dist|checksum> [参数]")
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, strings.TrimPrefix(buildinfo.Version, "v"))
		return nil
	case "verify-version":
		if len(args) != 2 {
			return errors.New("用法：release verify-version <tag>")
		}
		tag := strings.TrimPrefix(strings.TrimSpace(args[1]), "v")
		if tag != strings.TrimPrefix(buildinfo.Version, "v") {
			return fmt.Errorf("release tag v%s does not match buildinfo.Version %s", tag, buildinfo.Version)
		}
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
