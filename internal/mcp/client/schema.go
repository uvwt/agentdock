package client

import (
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

type toolInputValidator = toolcontract.InputValidator

func compileToolInputSchema(raw map[string]any) (*toolInputValidator, error) {
	return toolcontract.CompileInputSchema(raw)
}

func validateToolArguments(tool Tool, arguments map[string]any) error {
	if arguments == nil {
		arguments = map[string]any{}
	}
	validator := tool.inputValidator
	if validator == nil {
		compiled, err := compileToolInputSchema(tool.InputSchema)
		if err != nil {
			return newError(
				"MCP_SCHEMA_INVALID",
				"upstream MCP tool returned an invalid input schema",
				false,
				map[string]any{"tool": tool.Name, "reason": err.Error()},
				err,
			)
		}
		validator = compiled
	}
	if err := validator.Validate(arguments); err != nil {
		return newError(
			"MCP_ARGUMENT_INVALID",
			"MCP tool arguments do not match the discovered input schema",
			false,
			map[string]any{"tool": tool.Name, "reason": toolcontract.CompactValidationError(err)},
			err,
		)
	}
	return nil
}
