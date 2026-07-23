//go:build !windows

package file

import "context"

func unsupportedWSLFileRuntime() (Result, error) {
	return nil, toolError("INVALID_ARGUMENT", "runtime=wsl file tools are only supported by AgentDock on Windows", "validation")
}

func (svc *Service) readFileWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) listDirWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) listFilesWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) searchTextWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) fileEditWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}
