//go:build !darwin && !windows

package component

func LegacyPaths(string) []string { return nil }
