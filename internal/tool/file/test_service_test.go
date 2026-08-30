package file

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/uvwt/agentdock/internal/workspace"
)

func newFileTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return New(ws, nil, nil), root
}

func decodeFileTestRequest[T any](input any) (T, error) {
	var request T
	if typed, ok := input.(T); ok {
		return typed, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return request, err
	}
	return request, json.Unmarshal(data, &request)
}

func (s *Service) readFileTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeFileTestRequest[ReadRequest](input)
	if err != nil {
		return nil, err
	}
	return s.ReadFile(ctx, request)
}
func (s *Service) listDirTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeFileTestRequest[ListRequest](input)
	if err != nil {
		return nil, err
	}
	return s.ListDir(ctx, request)
}
func (s *Service) searchTextTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeFileTestRequest[SearchRequest](input)
	if err != nil {
		return nil, err
	}
	return s.SearchText(ctx, request)
}
func (s *Service) editTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeFileTestRequest[EditRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Edit(ctx, request)
}
func selectFileRuntimeTest(input any) (fileRuntimeSelection, error) {
	request, err := decodeFileTestRequest[RuntimeOptions](input)
	if err != nil {
		return fileRuntimeSelection{}, fmt.Errorf("decode runtime options: %w", err)
	}
	return selectFileRuntime(request)
}

func (s *Service) editFileTest(input any) (Result, error) {
	request, err := decodeFileTestRequest[EditRequest](input)
	if err != nil {
		return nil, err
	}
	return s.editFile(request)
}
func (s *Service) applyPatchTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeFileTestRequest[EditRequest](input)
	if err != nil {
		return nil, err
	}
	return s.applyPatch(ctx, request)
}
