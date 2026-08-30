//go:build !windows

package file

import "context"

func unsupportedWSLFileRuntime() (Result, error) {
	return nil, toolError("INVALID_ARGUMENT", "runtime=wsl file tools are only supported by AgentDock on Windows", "validation")
}

func (svc *Service) readFileWSL(context.Context, ReadRequest, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) listDirWSL(context.Context, ListRequest, fileRuntimeSelection, listDirOptions) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) searchTextWSL(context.Context, SearchRequest, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (svc *Service) fileEditWSL(context.Context, EditRequest, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}
