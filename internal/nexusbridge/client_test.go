package nexusbridge

import (
	"reflect"
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

func TestBridgeHelloSeparatesToolsFromBridgeCapabilities(t *testing.T) {
	tools := []string{"read_file", "exec_command"}
	hello := bridgeHello(
		Identity{DeviceID: "device_abcdefgh"},
		tools,
		[]protocol.ToolDescriptor{{Name: "read_file"}, {Name: "exec_command"}},
		[]protocol.UIResourceCapability{},
		"sha256:test",
	)
	if !reflect.DeepEqual(hello.Capabilities, tools) {
		t.Fatalf("capabilities = %#v, want tools %#v", hello.Capabilities, tools)
	}
	if len(hello.BridgeCapabilities) != 1 || hello.BridgeCapabilities[0] != protocol.ArtifactReadCapability {
		t.Fatalf("bridge_capabilities = %#v", hello.BridgeCapabilities)
	}
	for _, capability := range hello.Capabilities {
		if capability == protocol.ArtifactReadCapability {
			t.Fatal("Bridge capability leaked into model-facing tool capabilities")
		}
	}
}
