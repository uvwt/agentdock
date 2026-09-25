package runtimeapi_test

import (
	"testing"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/runtimeapi"
)

func TestAppRuntimeImplementsMCPOAuthRuntime(t *testing.T) {
	var runtime any = (*app.Runtime)(nil)
	if _, ok := runtime.(runtimeapi.MCPOAuthRuntime); !ok {
		t.Fatal("*app.Runtime must implement runtimeapi.MCPOAuthRuntime")
	}
}
