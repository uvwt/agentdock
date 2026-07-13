//go:build !windows

package tools

import "context"

func unsupportedWSLFileRuntime() (Result, error) {
	return nil, toolError("INVALID_ARGUMENT", "runtime=wsl file tools are only supported by AgentDock on Windows", "validation")
}

func (r *Runtime) readFileWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (r *Runtime) listDirWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (r *Runtime) listFilesWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (r *Runtime) searchTextWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}

func (r *Runtime) fileEditWSL(context.Context, map[string]any, fileRuntimeSelection) (Result, error) {
	return unsupportedWSLFileRuntime()
}
