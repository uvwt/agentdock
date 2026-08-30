package app

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/evolution"
	toolacp "github.com/uvwt/agentdock/internal/tool/acp"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
	toolmcp "github.com/uvwt/agentdock/internal/tool/mcp"
	toolmedia "github.com/uvwt/agentdock/internal/tool/media"
	toolrecall "github.com/uvwt/agentdock/internal/tool/recall"
	toolskill "github.com/uvwt/agentdock/internal/tool/skill"
	tooltask "github.com/uvwt/agentdock/internal/tool/task"
)

func TestAllToolDefinitionsHaveStrictCompilableInputContracts(t *testing.T) {
	cfg := config.Config{
		NexusEndpoint:  "http://127.0.0.1:18777",
		BrowserEnabled: true,
		ACPEnabled:     true,
	}
	for _, definition := range toolDefinitionsForConfig(cfg) {
		if got := definition.InputSchema["additionalProperties"]; got != false {
			t.Fatalf("%s root additionalProperties = %#v, want false", definition.Name, got)
		}
		if _, err := toolcontract.CompileInputSchema(definition.InputSchema); err != nil {
			t.Fatalf("compile %s input schema: %v", definition.Name, err)
		}
		if len(definition.OutputSchema) == 0 {
			t.Fatalf("%s output schema is empty", definition.Name)
		}
	}
}

func TestBuiltInInputValidatorCacheUsesSchemaContent(t *testing.T) {
	withoutNexus := tooltask.ManageInputSchema(config.Config{})
	first, err := compileBuiltInInputValidator(withoutNexus)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compileBuiltInInputValidator(tooltask.ManageInputSchema(config.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical built-in schemas should reuse the compiled validator")
	}

	withNexus, err := compileBuiltInInputValidator(tooltask.ManageInputSchema(config.Config{NexusEndpoint: "http://127.0.0.1:18777"}))
	if err != nil {
		t.Fatal(err)
	}
	if first == withNexus {
		t.Fatal("config-aware task schemas must not share a validator when their JSON contracts differ")
	}
}

func TestTypedToolRequestFieldsMatchPublishedSchemas(t *testing.T) {
	fullConfig := config.Config{NexusEndpoint: "http://127.0.0.1:18777"}
	tests := []struct {
		name       string
		request    any
		exact      bool
		allowExtra []string
	}{
		{name: toolfile.ToolReadFile, request: toolfile.ReadRequest{}, exact: true, allowExtra: []string{"runtime", "wsl_distribution"}},
		{name: toolfile.ToolListDir, request: toolfile.ListRequest{}, exact: true, allowExtra: []string{"runtime", "wsl_distribution"}},
		{name: toolfile.ToolSearchText, request: toolfile.SearchRequest{}, exact: true, allowExtra: []string{"runtime", "wsl_distribution"}},
		{name: toolfile.ToolFileEdit, request: toolfile.EditRequest{}, exact: true, allowExtra: []string{"runtime", "wsl_distribution"}},
		{name: toolcommand.ToolExecCommand, request: toolcommand.ExecRequest{}, exact: true, allowExtra: []string{"runtime", "wsl_distribution"}},
		{name: toolcommand.ToolSessionObserve, request: toolcommand.SessionObserveRequest{}, exact: true},
		{name: toolcommand.ToolSessionAct, request: toolcommand.SessionActRequest{}, exact: true},
		{name: tooltask.ToolTaskManage, request: tooltask.ManageRequest{}, exact: true},
		{name: "workflow_template_manage", request: tooltask.WorkflowRequest{}},
		{name: evolution.ToolName, request: evolution.Request{}},
		{name: toolacp.ToolSession, request: toolacp.SessionRequest{}, exact: true},
		{name: toolacp.ToolPrompt, request: toolacp.PromptRequest{}, exact: true},
		{name: toolacp.ToolInteraction, request: toolacp.InteractionRequest{}, exact: true},
		{name: toolskill.ToolPackage, request: toolskill.PackageRequest{}, exact: true},
		{name: toolmcp.ToolManage, request: toolmcp.ManageRequest{}, exact: true},
		{name: toolmcp.ToolSearch, request: toolmcp.SearchRequest{}, exact: true},
		{name: toolmcp.ToolInspect, request: toolmcp.InspectRequest{}, exact: true},
		{name: toolmcp.ToolCall, request: toolmcp.CallRequest{}, exact: true},
		{name: toolmedia.ToolViewImage, request: toolmedia.ViewImageRequest{}, exact: true},
		{name: toolmedia.ToolFilePublish, request: toolmedia.FilePublishRequest{}, exact: true},
		{name: "recall_search", request: toolrecall.SearchRequest{}},
		{name: "recall_read", request: toolrecall.ReadRequest{}},
		{name: "recall_write", request: toolrecall.WriteRequest{}},
		{name: "recall_maintain", request: toolrecall.MaintainRequest{}},
		{name: "private_note_manage", request: toolrecall.PrivateNoteRequest{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition, ok := toolDefinitionForConfig(test.name, fullConfig)
			if !ok {
				t.Fatalf("missing tool definition for %s", test.name)
			}
			assertSchemaMatchesRequestType(t, test.name, definition.InputSchema, reflect.TypeOf(test.request), test.exact, test.allowExtra)
		})
	}
}

func assertSchemaMatchesRequestType(t *testing.T, path string, schema map[string]any, requestType reflect.Type, exact bool, allowExtra []string) {
	t.Helper()
	requestType = dereferenceType(requestType)
	if requestType.Kind() != reflect.Struct {
		t.Fatalf("%s request type = %s, want struct", path, requestType)
	}
	properties, _ := schema["properties"].(map[string]any)
	fields := jsonFields(requestType)

	for name, property := range properties {
		fieldType, ok := fields[name]
		if !ok {
			t.Errorf("%s schema field %q has no matching request field", path, name)
			continue
		}
		propertySchema, _ := property.(map[string]any)
		assertSchemaPropertyMatchesGoType(t, path+"."+name, propertySchema, fieldType)
	}
	if !exact {
		return
	}
	allowed := make(map[string]struct{}, len(allowExtra))
	for _, name := range allowExtra {
		allowed[name] = struct{}{}
	}
	var extras []string
	for name := range fields {
		if _, ok := properties[name]; ok {
			continue
		}
		if _, ok := allowed[name]; ok {
			continue
		}
		extras = append(extras, name)
	}
	if len(extras) > 0 {
		sort.Strings(extras)
		t.Errorf("%s request fields missing from schema: %s", path, strings.Join(extras, ", "))
	}
}

func assertSchemaPropertyMatchesGoType(t *testing.T, path string, schema map[string]any, goType reflect.Type) {
	t.Helper()
	if len(schema) == 0 {
		return
	}
	goType = dereferenceType(goType)
	if goType.Kind() == reflect.Interface {
		return
	}

	schemaType, _ := schema["type"].(string)
	matches := true
	switch schemaType {
	case "string":
		matches = goType.Kind() == reflect.String
	case "integer":
		matches = goType.Kind() >= reflect.Int && goType.Kind() <= reflect.Int64 || goType.Kind() >= reflect.Uint && goType.Kind() <= reflect.Uint64
	case "number":
		matches = goType.Kind() == reflect.Float32 || goType.Kind() == reflect.Float64
	case "boolean":
		matches = goType.Kind() == reflect.Bool
	case "array":
		matches = goType.Kind() == reflect.Slice || goType.Kind() == reflect.Array
	case "object":
		matches = goType.Kind() == reflect.Struct || goType.Kind() == reflect.Map
	case "":
		return
	}
	if !matches {
		t.Errorf("%s schema type %q does not match Go type %s", path, schemaType, goType)
		return
	}

	if schemaType == "object" && schema["additionalProperties"] == false && goType.Kind() == reflect.Struct {
		assertSchemaMatchesRequestType(t, path, schema, goType, true, nil)
		return
	}
	if schemaType == "array" && (goType.Kind() == reflect.Slice || goType.Kind() == reflect.Array) {
		items, _ := schema["items"].(map[string]any)
		itemType := dereferenceType(goType.Elem())
		if itemType.Kind() == reflect.Struct && len(items) > 0 {
			assertSchemaPropertyMatchesGoType(t, path+"[]", items, itemType)
		}
	}
}

func jsonFields(value reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	var visit func(reflect.Type)
	visit = func(current reflect.Type) {
		current = dereferenceType(current)
		if current.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < current.NumField(); i++ {
			field := current.Field(i)
			if field.PkgPath != "" && !field.Anonymous {
				continue
			}
			tag := field.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				visit(field.Type)
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
	}
	visit(value)
	return fields
}

func dereferenceType(value reflect.Type) reflect.Type {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}
