//go:build windows

package nexusbridge

type fileOwner struct{}

func detectInheritedOwner(string) (fileOwner, bool)       { return fileOwner{}, false }
func applyInheritedOwner(string, string, fileOwner) error { return nil }
