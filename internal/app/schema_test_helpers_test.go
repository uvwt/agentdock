package app

import "github.com/uvwt/agentdock/internal/config"

var fullContractTestConfig = config.Config{NexusEndpoint: "http://127.0.0.1:18777"}

func testInputSchema(name string) map[string]any {
	return testInputSchemaForConfig(name, fullContractTestConfig)
}

func testOutputSchema(name string) map[string]any {
	return testOutputSchemaForConfig(name, fullContractTestConfig)
}

func testInputSchemaForConfig(name string, cfg config.Config) map[string]any {
	definition, ok := toolDefinitionForConfig(name, cfg)
	if !ok {
		return nil
	}
	return definition.InputSchema
}

func testOutputSchemaForConfig(name string, cfg config.Config) map[string]any {
	definition, ok := toolDefinitionForConfig(name, cfg)
	if !ok {
		return nil
	}
	return definition.OutputSchema
}
