package tools

import "path/filepath"

func sameExistingTestPath(left, right string) bool {
	leftSnapshot, err := captureFileSnapshot(left)
	if err != nil {
		return false
	}
	rightSnapshot, err := captureFileSnapshot(right)
	return err == nil && leftSnapshot.Identity == rightSnapshot.Identity
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
