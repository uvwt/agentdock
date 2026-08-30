package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// decodeToolInput 是动态 MCP/HTTP 参数进入强类型工具核心的唯一转换点。
// 这里故意不做 fmt.Sprint / ParseBool 一类宽松转换：已声明字段必须满足公开 Schema 的真实类型。
// 未知字段是否允许由 Runtime 的 Schema 校验决定，避免改变 additionalProperties=true 工具的既有契约。
func decodeToolInput(tool string, args map[string]any, target any) error {
	if err := rejectDeclaredNullArguments(tool, args); err != nil {
		return err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments cannot be encoded", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments do not match the declared input types", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	if decoder.More() {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments contain trailing JSON data", "validation", map[string]any{"tool": tool})
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return toolErrorDetails("INVALID_ARGUMENT", "tool arguments contain trailing JSON data", "validation", map[string]any{"tool": tool, "reason": err.Error()})
	}
	return nil
}

func rejectDeclaredNullArguments(tool string, args map[string]any) error {
	properties, _ := InputSchema(tool)["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}
	fields := make([]string, 0)
	for field, value := range args {
		if value != nil {
			continue
		}
		if _, declared := properties[field]; declared {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	return toolErrorDetails(
		"INVALID_ARGUMENT",
		"declared tool arguments cannot be null",
		"validation",
		map[string]any{"tool": tool, "fields": fields},
	)
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

// typedToolHandler 把 map[string]any 限制在 Runtime 的动态工具分发边界。
// 解码成功后，capability Service 只接收明确 request，不再依赖字符串 key 和宽松类型转换。
func typedToolHandler[T any](tool string, call func(context.Context, *Runtime, T) (Result, error)) ToolHandler {
	return func(ctx context.Context, runtime *Runtime, args map[string]any) (Result, error) {
		var request T
		if err := decodeToolInput(tool, args, &request); err != nil {
			return nil, err
		}
		return call(ctx, runtime, request)
	}
}
