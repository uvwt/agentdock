package app

import (
	"context"
	"fmt"

	mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"
	"github.com/uvwt/agentdock/internal/config"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

type ToolHandler func(context.Context, *Runtime, map[string]any) (Result, error)

type ToolContract struct {
	InputSchema  map[string]any
	OutputSchema map[string]any
}

type ToolContractProvider func(string, config.Config) (ToolContract, bool)

// ToolSpec 是工具公开注册的单一入口：描述、契约所有者、配置开关和 handler 在这里显式绑定。
// Schema 字段细节由对应 capability package 或共享 mcpcontract 拥有，app 不再按名字二次寻找 owner。
type ToolSpec struct {
	Name                   string
	Title                  string
	Description            string
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	Contract               ToolContractProvider
	Annotations            *ToolAnnotations
	Availability           func(config.Config) bool
	Handler                ToolHandler
}

type ToolAnnotations struct {
	Title           string
	ReadOnlyHint    bool
	DestructiveHint *bool
	IdempotentHint  bool
	OpenWorldHint   *bool
}

type ToolDefinition struct {
	Name                   string
	Title                  string
	Description            string
	UIBinding              *UIBinding
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	InputSchema            map[string]any
	OutputSchema           map[string]any
	Annotations            *ToolAnnotations
}

// toolDefinitionsForConfig 从同一个 ToolSpec registry 生成协议定义。
// cfg 只影响 capability-aware schema，不改变注册顺序。
func toolDefinitionsForConfig(cfg config.Config) []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		defs = append(defs, spec.definition(cfg))
	}
	return defs
}

// ToolDefinitions 返回无外部能力配置时的完整注册表定义，供静态检查与测试使用。
func ToolDefinitions() []ToolDefinition {
	return toolDefinitionsForConfig(config.Config{})
}

func toolDefinitionForConfig(name string, cfg config.Config) (ToolDefinition, bool) {
	spec, ok := toolSpecByName(name)
	if !ok {
		return ToolDefinition{}, false
	}
	return spec.definition(cfg), true
}

func (s ToolSpec) definition(cfg config.Config) ToolDefinition {
	contract := ToolContract{}
	if s.Contract != nil {
		contract, _ = s.Contract(s.Name, cfg)
	}
	annotations := cloneToolAnnotations(s.Annotations)
	if canonical, ok := mcpcontract.AnnotationContract(s.Name); ok {
		annotations = canonicalToolAnnotations(canonical)
	}
	return ToolDefinition{
		Name:                   s.Name,
		Title:                  s.Title,
		Description:            s.Description,
		UIBinding:              toolUIBinding(s.Name),
		FileArgRewritePaths:    append([]string(nil), s.FileArgRewritePaths...),
		FileResultRewritePaths: append([]string(nil), s.FileResultRewritePaths...),
		InputSchema:            contract.InputSchema,
		OutputSchema:           contract.OutputSchema,
		Annotations:            annotations,
	}
}

func cloneToolAnnotations(value *ToolAnnotations) *ToolAnnotations {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.DestructiveHint != nil {
		flag := *value.DestructiveHint
		cloned.DestructiveHint = &flag
	}
	if value.OpenWorldHint != nil {
		flag := *value.OpenWorldHint
		cloned.OpenWorldHint = &flag
	}
	return &cloned
}

func (s ToolSpec) available(cfg config.Config) bool {
	if s.Availability == nil {
		return true
	}
	return s.Availability(cfg)
}

func toolSpecByName(name string) (ToolSpec, bool) {
	spec, ok := toolSpecIndex[name]
	return spec, ok
}

func compileAvailableToolContracts(cfg config.Config) ([]string, map[string]*toolcontract.InputValidator, error) {
	names := make([]string, 0, len(toolSpecs))
	validators := make(map[string]*toolcontract.InputValidator, len(toolSpecs))
	for _, spec := range toolSpecs {
		if !spec.available(cfg) {
			continue
		}
		definition := spec.definition(cfg)
		if len(definition.InputSchema) == 0 {
			return nil, nil, fmt.Errorf("tool %s has no input schema", spec.Name)
		}
		if len(definition.OutputSchema) == 0 {
			return nil, nil, fmt.Errorf("tool %s has no output schema", spec.Name)
		}
		validator, err := compileBuiltInInputValidator(definition.InputSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("compile %s input schema: %w", spec.Name, err)
		}
		names = append(names, spec.Name)
		validators[spec.Name] = validator
	}
	return names, validators, nil
}

func requiresNexus(cfg config.Config) bool   { return cfg.NexusEndpoint != "" }
func requiresBrowser(cfg config.Config) bool { return cfg.BrowserEnabled }
func requiresACP(cfg config.Config) bool     { return cfg.ACPEnabled }

func readOnlyToolAnnotations(openWorld bool) *ToolAnnotations {
	return &ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(openWorld)}
}

func mutatingToolAnnotations(destructive, openWorld bool) *ToolAnnotations {
	return &ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPointer(destructive), OpenWorldHint: boolPointer(openWorld)}
}

func boolPointer(value bool) *bool { return &value }

func canonicalToolAnnotations(value mcpcontract.Annotations) *ToolAnnotations {
	annotations := &ToolAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
	}
	if value.IdempotentHint != nil {
		annotations.IdempotentHint = *value.IdempotentHint
	}
	return annotations
}

func ctxToolHandler(fn func(*Runtime, context.Context, map[string]any) (Result, error)) ToolHandler {
	return func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) { return fn(r, ctx, args) }
}
