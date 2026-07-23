package file

import (
	"context"

	"github.com/uvwt/agentdock/internal/workspace"
)

func (s *Service) EditFile(args map[string]any) (Result, error) {
	return s.editFile(args)
}

func (s *Service) ApplyPatch(ctx context.Context, args map[string]any) (Result, error) {
	return s.applyPatch(ctx, args)
}

func (s *Service) SearchTextGo(ctx context.Context, path workspace.Path, options SearchOptions) (Result, error) {
	return s.searchTextGo(ctx, path, options)
}

func (s *Service) ParseRGJSON(output []byte, options SearchOptions) ([]map[string]any, bool, bool) {
	return s.parseRGJSON(output, options)
}

type FileRuntimeSelection = fileRuntimeSelection

func SelectFileRuntime(args map[string]any) (FileRuntimeSelection, error) {
	return selectFileRuntime(args)
}
