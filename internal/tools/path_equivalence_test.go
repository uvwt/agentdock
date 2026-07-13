package tools

import (
	"os"
	"path/filepath"
)

func sameExistingTestPath(left, right string) bool {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false
	}
	rightInfo, err := os.Stat(right)
	return err == nil && os.SameFile(leftInfo, rightInfo)
}

func sameTestPath(left, right string) bool {
	if sameExistingTestPath(left, right) {
		return true
	}
	if filepath.Base(left) != filepath.Base(right) {
		return false
	}
	return sameExistingTestPath(filepath.Dir(left), filepath.Dir(right))
}
