//go:build windows

package file

func ResolveWSLFilePath(raw string) (string, error) {
	return resolveWSLFilePath(raw)
}

func WSLFileErrorPhase(code string) string {
	return wslFileErrorPhase(code)
}
