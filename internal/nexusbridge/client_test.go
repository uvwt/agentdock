package nexusbridge

import (
	"testing"

	protocol "github.com/uvwt/agentdock-protocol"
)

func TestBridgeToolDescriptorsPreservePresentationBinding(t *testing.T) {
	descriptors, err := bridgeToolDescriptors([]map[string]any{
		{
			"name":        "file_edit",
			"inputSchema": map[string]any{"type": "object"},
			"_meta":       map[string]any{"ui": map[string]any{"resourceUri": protocol.FileChangeUIResourceURI}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 || descriptors[0].Name != "file_edit" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	ui, ok := descriptors[0].Meta["ui"].(map[string]any)
	if !ok || ui["resourceUri"] != protocol.FileChangeUIResourceURI {
		t.Fatalf("presentation meta = %#v", descriptors[0].Meta)
	}
}
