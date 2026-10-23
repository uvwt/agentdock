package contract

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	draft7HTTP   = "http://json-schema.org/draft-07/schema#"
	draft7HTTPS  = "https://json-schema.org/draft-07/schema#"
	draft2020URL = "https://json-schema.org/draft/2020-12/schema"
)

type InputValidator struct {
	resolved *jsonschema.Resolved
}

func CompileInputSchema(raw map[string]any) (*InputValidator, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("inputSchema must not be empty")
	}
	if !schemaRootAllowsObject(raw["type"]) {
		return nil, fmt.Errorf("inputSchema root must allow object")
	}
	if err := validateSchemaDraft(raw); err != nil {
		return nil, err
	}

	// Schema 也先归一化到真实 JSON 数据模型。各 capability 为了可读性会使用
	// []string / []map[string]any 构造 enum、oneOf 等字段，但 metaschema 校验只接受
	// JSON 解码后的 []any。通过一次编解码既统一具体 Go 类型，也保证不修改调用方 map。
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode inputSchema: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("normalize inputSchema: %w", err)
	}
	encoded, err = json.Marshal(normalizeNullableSchema(normalized))
	if err != nil {
		return nil, fmt.Errorf("encode normalized inputSchema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("decode inputSchema: %w", err)
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("compile inputSchema: %w", err)
	}
	return &InputValidator{resolved: resolved}, nil
}

func (v *InputValidator) Validate(arguments map[string]any) error {
	if v == nil || v.resolved == nil {
		return fmt.Errorf("input validator is not initialized")
	}
	if arguments == nil {
		arguments = map[string]any{}
	}

	// Runtime、HTTP 和 MCP 最终都传输 JSON。先归一化一次，避免 Go 调用方的
	// int/int64 或自定义 map 具体类型让校验行为偏离真实协议数据模型。
	raw, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("encode tool arguments: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return v.resolved.Validate(normalized)
}

// CompactValidationError 保持模型可见的校验原因简短、稳定。
// google/jsonschema-go 当前只返回带上下文的字符串错误，没有结构化 instance path。
// 因此这里只归一化 AgentDock 已经对外使用的 required/type 两类错误；其他错误
// 保留上游信息并压缩空白，避免为了“漂亮报错”重新实现一套 JSON Schema 校验器。
func CompactValidationError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	path := validationInstancePath(message)

	if index := strings.LastIndex(message, "required: missing properties: "); index >= 0 {
		missing := message[index+len("required: missing properties: "):]
		if property, ok := firstQuotedString(missing); ok {
			return path + "." + property + " is required"
		}
	}

	if index := strings.LastIndex(message, "type: "); index >= 0 {
		failure := message[index:]
		if wantIndex := strings.LastIndex(failure, ", want "); wantIndex >= 0 {
			want := strings.TrimPrefix(failure[wantIndex+len(", want "):], "one of ")
			if types, ok := firstQuotedString(want); ok {
				return path + " must be " + strings.ReplaceAll(types, ", ", " or ")
			}
		}
	}
	return message
}

func validateSchemaDraft(raw map[string]any) error {
	value, exists := raw["$schema"]
	if !exists {
		return nil
	}
	draft, ok := value.(string)
	if !ok {
		// 交给 json.Unmarshal/Resolve 返回更准确的 schema 类型错误。
		return nil
	}
	switch draft {
	case "", draft7HTTP, draft7HTTPS, draft2020URL:
		return nil
	default:
		// google/jsonschema-go 只实现 Draft-07 与 2020-12。旧 Draft 不能简单
		// 改写版本号，否则 items、$ref siblings 等关键语义会被静默解释错误。
		return fmt.Errorf("inputSchema declares unsupported JSON Schema draft %q; supported drafts are draft-07 and 2020-12", draft)
	}
}

func validationInstancePath(message string) string {
	best := "$"
	for offset := 0; ; {
		index := strings.Index(message[offset:], "validating ")
		if index < 0 {
			break
		}
		start := offset + index + len("validating ")
		end := strings.Index(message[start:], ": ")
		if end < 0 {
			break
		}
		candidate := instancePathFromSchemaPath(message[start : start+end])
		// $ref 会同时留下引用点和被引用 schema 的 validating frame。
		// 优先保留包含更多 properties 层级的引用点，避免把 $.user 退化成 $。
		if len(candidate) > len(best) {
			best = candidate
		}
		offset = start + end + 2
	}
	return best
}

func instancePathFromSchemaPath(schemaPath string) string {
	if schemaPath == "" || schemaPath == "root" || !strings.HasPrefix(schemaPath, "/") {
		return "$"
	}

	path := "$"
	parts := strings.Split(strings.TrimPrefix(schemaPath, "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] != "properties" {
			continue
		}
		property := strings.ReplaceAll(strings.ReplaceAll(parts[index+1], "~1", "/"), "~0", "~")
		path += "." + property
		index++
	}
	return path
}

func firstQuotedString(value string) (string, bool) {
	start := strings.IndexByte(value, '"')
	if start < 0 {
		return "", false
	}
	for end := start + 1; end < len(value); end++ {
		if value[end] != '"' {
			continue
		}
		decoded, err := strconv.Unquote(value[start : end+1])
		if err == nil {
			return decoded, true
		}
	}
	return "", false
}

func schemaRootAllowsObject(rawType any) bool {
	if rawType == nil {
		return true
	}
	switch typed := rawType.(type) {
	case string:
		return typed == "object"
	case []any:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	}
	return false
}

// nullable 是部分第三方 MCP Server 仍会返回的 OpenAPI 兼容扩展。只在
// 编译副本中转换成标准 JSON Schema null 联合类型，不改调用方提供的 map。
func normalizeNullableSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			// 只遍历 JSON Schema 规定的子 Schema 位置；const、enum、default、
			// examples 中的对象是用户数据，即使包含 nullable/type 也不能改写。
			switch key {
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
				if entries, ok := child.(map[string]any); ok {
					children := make(map[string]any, len(entries))
					for name, schema := range entries {
						children[name] = normalizeNullableSchema(schema)
					}
					child = children
				}
			case "items", "additionalItems", "additionalProperties", "contains", "propertyNames",
				"not", "if", "then", "else", "unevaluatedItems", "unevaluatedProperties", "contentSchema",
				"allOf", "anyOf", "oneOf", "prefixItems":
				child = normalizeNullableSchema(child)
			}
			normalized[key] = child
		}
		if nullable, _ := typed["nullable"].(bool); nullable {
			if rawType, exists := normalized["type"]; exists {
				normalized["type"] = appendNullType(rawType)
			}
			delete(normalized, "nullable")
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for i, child := range typed {
			normalized[i] = normalizeNullableSchema(child)
		}
		return normalized
	case []string:
		normalized := make([]any, len(typed))
		for i, child := range typed {
			normalized[i] = child
		}
		return normalized
	default:
		return value
	}
}

func appendNullType(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed == "null" {
			return typed
		}
		return []any{typed, "null"}
	case []any:
		for _, item := range typed {
			if item == "null" {
				return typed
			}
		}
		return append(typed, "null")
	default:
		return value
	}
}
