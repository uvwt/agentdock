package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const inputSchemaResource = "urn:agentdock:tool-input-schema"

type InputValidator struct {
	schema *jsonschema.Schema
}

type localSchemaLoader struct{}

func (localSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is not allowed: %s", url)
}

func CompileInputSchema(raw map[string]any) (*InputValidator, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("inputSchema must not be empty")
	}
	if !schemaRootAllowsObject(raw["type"]) {
		return nil, fmt.Errorf("inputSchema root must allow object")
	}

	// Schema 也先归一化到真实 JSON 数据模型。各 capability 为了可读性会使用
	// []string / []map[string]any 构造 enum、oneOf 等字段，但 metaschema 校验只接受
	// JSON 解码后的 []any。通过一次编解码既统一具体 Go 类型，也保证不修改调用方 map。
	normalized := normalizeNullableSchema(raw)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode inputSchema: %w", err)
	}
	var schemaJSON any
	if err := json.Unmarshal(encoded, &schemaJSON); err != nil {
		return nil, fmt.Errorf("decode inputSchema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(localSchemaLoader{})
	if err := compiler.AddResource(inputSchemaResource, schemaJSON); err != nil {
		return nil, fmt.Errorf("load inputSchema: %w", err)
	}
	compiled, err := compiler.Compile(inputSchemaResource)
	if err != nil {
		return nil, fmt.Errorf("compile inputSchema: %w", err)
	}
	return &InputValidator{schema: compiled}, nil
}

func (v *InputValidator) Validate(arguments map[string]any) error {
	if v == nil || v.schema == nil {
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
	return v.schema.Validate(normalized)
}

func CompactValidationError(err error) string {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return strings.Join(strings.Fields(err.Error()), " ")
	}
	leaf := firstSchemaErrorLeaf(validationErr)
	path := schemaInstancePath(leaf.InstanceLocation)
	switch failure := leaf.ErrorKind.(type) {
	case *kind.Required:
		if len(failure.Missing) > 0 {
			return path + "." + failure.Missing[0] + " is required"
		}
	case *kind.Type:
		if len(failure.Want) > 0 {
			return path + " must be " + strings.Join(failure.Want, " or ")
		}
	}
	return strings.Join(strings.Fields(leaf.Error()), " ")
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
			normalized[key] = normalizeNullableSchema(child)
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

func firstSchemaErrorLeaf(err *jsonschema.ValidationError) *jsonschema.ValidationError {
	for len(err.Causes) > 0 {
		err = err.Causes[0]
	}
	return err
}

func schemaInstancePath(parts []string) string {
	var path strings.Builder
	path.WriteByte('$')
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			path.WriteByte('[')
			path.WriteString(part)
			path.WriteByte(']')
			continue
		}
		path.WriteByte('.')
		path.WriteString(part)
	}
	return path.String()
}
