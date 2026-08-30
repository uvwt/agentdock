package recall

import (
	"context"
	"encoding/json"
)

// decodeRecallTestRequest 只服务于从 app 搬入 capability 包的旧行为测试。
// 生产入口不保留 map 兼容层；这些测试仍可用原始 JSON 形状表达外部调用场景。
func decodeRecallTestRequest[T any](input any) (T, error) {
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

func (s *Service) writeTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Write(ctx, request)
}

func (s *Service) searchTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[SearchRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Search(ctx, request)
}

func (s *Service) maintainTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[MaintainRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Maintain(ctx, request)
}

func (s *Service) privateNoteManageTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[PrivateNoteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.PrivateNoteManage(ctx, request)
}

func (s *Service) memoryCardCaptureTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryCardCapture(ctx, request)
}

func (s *Service) memoryCardWriteTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryCardWrite(ctx, request)
}

func parseMemoryCardTest(input any, requireEvidenceForActive bool) (memoryCardSpec, []string, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return memoryCardSpec{}, nil, err
	}
	return parseMemoryCard(request, requireEvidenceForActive)
}

func (s *Service) memoryDiffTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryDiff(ctx, request)
}

func (s *Service) memoryPatchTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryPatch(ctx, request)
}

func (s *Service) memoryUpdateFactTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[WriteRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryUpdateFact(ctx, request)
}

func (s *Service) memoryLintTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[MaintainRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryLint(ctx, request)
}

func (s *Service) memoryReadTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeRecallTestRequest[ReadRequest](input)
	if err != nil {
		return nil, err
	}
	return s.memoryRead(ctx, request)
}
