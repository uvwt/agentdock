package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// decodeToolInput 是动态 JSON 参数进入强类型 capability 的唯一转换点。
// 参数是否符合公开契约已经由 Runtime 的同一份 JSON Schema 完整校验，这里只负责无损解码。
func decodeToolInput(tool string, args map[string]any, target any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments cannot be encoded", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments cannot be decoded", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	if decoder.More() {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments contain trailing JSON data", "validation", map[string]any{"tool": tool})
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments contain trailing JSON data", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("unexpected trailing JSON value")
	} else if err != io.EOF {
		return err
	}
	return nil
}

// typedToolHandler 把 map[string]any 限制在 Runtime 的协议边界。
// 完整 Schema 校验成功后，capability Service 只接收明确 request。
func typedToolHandler[T any](tool string, call func(context.Context, *Runtime, T) (Result, error)) ToolHandler {
	return func(ctx context.Context, runtime *Runtime, args map[string]any) (Result, error) {
		var request T
		if err := decodeToolInput(tool, args, &request); err != nil {
			return nil, err
		}
		return call(ctx, runtime, request)
	}
}
