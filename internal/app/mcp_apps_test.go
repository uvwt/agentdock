package app

import (
	"testing"

	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/config"
)

func TestACPUIBindingsOptIntoCompactMode(t *testing.T) {
	tests := map[string]string{
		"acp_session": protocol.ACPStatusUIResourceURI,
		"acp_prompt":  protocol.ACPPromptUIResourceURI,
	}
	for name, wantURI := range tests {
		binding := toolUIBinding(name)
		if binding == nil {
			t.Fatalf("%s UI binding missing", name)
		}
		if binding.ResourceURI != wantURI {
			t.Fatalf("%s resource URI = %q, want %q", name, binding.ResourceURI, wantURI)
		}
		trigger, enabled := binding.Trigger(config.MCPAppsModeCompact)
		if !enabled || trigger.Action != "" {
			t.Fatalf("%s compact trigger = %#v enabled=%v", name, trigger, enabled)
		}
	}
}
