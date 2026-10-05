//go:build !darwin && !windows

package component

func DefaultRuntimeRoot() string { return "" }
