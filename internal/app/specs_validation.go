package app

import (
	"encoding/json"
	"fmt"
	"sync"

	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

var builtInInputValidators = struct {
	sync.Mutex
	bySchema map[string]*toolcontract.InputValidator
}{bySchema: map[string]*toolcontract.InputValidator{}}

// compileBuiltInInputValidator 只缓存 AgentDock 内建工具已经声明出来的契约。
// Runtime 可能在测试、嵌入式调用或重建配置时被重复创建；这些不可变 Schema 不应每次都重新编译。
// key 直接使用 JSON 内容，因此 task_manage 等 config-aware 契约变化时会自然得到独立 validator。
func compileBuiltInInputValidator(schema map[string]any) (*toolcontract.InputValidator, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode built-in input schema: %w", err)
	}
	key := string(encoded)

	builtInInputValidators.Lock()
	defer builtInInputValidators.Unlock()
	if validator := builtInInputValidators.bySchema[key]; validator != nil {
		return validator, nil
	}
	validator, err := toolcontract.CompileInputSchema(schema)
	if err != nil {
		return nil, err
	}
	builtInInputValidators.bySchema[key] = validator
	return validator, nil
}
