package app

import (
	"os"
	"path/filepath"
)

func sameExistingTestPath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
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
