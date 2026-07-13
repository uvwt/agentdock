//go:build darwin

package tools

import "golang.org/x/sys/unix"

func renameNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
