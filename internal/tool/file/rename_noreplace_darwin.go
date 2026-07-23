//go:build darwin

package file

import "golang.org/x/sys/unix"

func renameNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
