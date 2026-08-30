package app

import (
	"context"

	mcpcontract "github.com/uvwt/agentdock-protocol/mcpcontract"
	"github.com/uvwt/agentdock/internal/config"
)

type ToolHandler func(context.Context, *Runtime, map[string]any) (Result, error)

// ToolSpec 是工具公开入口的单一事实源：运行时分发、MCP 描述、
// 配置开关都从这里派生，避免多处手写列表漂移。
type ToolSpec struct {
	Name                   string
	Title                  string
	Description            string
	FileArgRewritePaths    []string
	FileResultRewritePaths []string
	InputSchema            func() map[string]any
	OutputSchema           func() map[string]any
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

// ToolDefinitions 只导出 MCP 层需要的描述和 schema，不暴露 handler。
func ToolDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(allToolSpecs()))
	for _, spec := range allToolSpecs() {
		defs = append(defs, spec.definition())
	}
	return defs
}

func (s ToolSpec) definition() ToolDefinition {
	return ToolDefinition{
		Name:                   s.Name,
		Title:                  s.Title,
		Description:            s.Description,
		UIBinding:              toolUIBinding(s.Name),
		FileArgRewritePaths:    append([]string(nil), s.FileArgRewritePaths...),
		FileResultRewritePaths: append([]string(nil), s.FileResultRewritePaths...),
		InputSchema:            s.InputSchema(),
		OutputSchema:           s.OutputSchema(),
		Annotations:            cloneToolAnnotations(s.Annotations),
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

func (r *Runtime) availableToolSpecs() []ToolSpec {
	out := make([]ToolSpec, 0, len(allToolSpecs()))
	for _, spec := range allToolSpecs() {
		if !spec.available(r.cfg) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func (s ToolSpec) available(cfg config.Config) bool {
	if s.Availability == nil {
		return true
	}
	return s.Availability(cfg)
}

func toolSpecByName(name string) (ToolSpec, bool) {
	for _, spec := range allToolSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return ToolSpec{}, false
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

func bindToolSchemas(specs []ToolSpec) []ToolSpec {
	for i := range specs {
		name := specs[i].Name
		if specs[i].InputSchema == nil {
			specs[i].InputSchema = func() map[string]any { return InputSchema(name) }
		}
		if specs[i].OutputSchema == nil {
			specs[i].OutputSchema = func() map[string]any { return OutputSchema(name) }
		}
		if annotations, ok := mcpcontract.AnnotationContract(name); ok {
			specs[i].Annotations = canonicalToolAnnotations(annotations)
		}
	}
	return specs
}
