package tools

import (
	"os"
)

type fileSnapshot struct {
	Info     os.FileInfo
	Identity string
}

func captureFileSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	identity, err := platformFileIdentity(path, info)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{Info: info, Identity: identity}, nil
}

func sameFileSnapshot(expected, actual fileSnapshot) bool {
	return expected.Identity == actual.Identity &&
		expected.Info.Mode() == actual.Info.Mode() &&
		expected.Info.Size() == actual.Info.Size() &&
		expected.Info.ModTime().Equal(actual.Info.ModTime())
}
