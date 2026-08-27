package nexusbridge

import "testing"

func TestNexusToolDescriptorsMarkOnlyAgentDockAppResources(t *testing.T) {
	descriptors := []map[string]any{
		{
			"name":  "file_edit",
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://agentdock/file-change"}},
		},
		{
			"name":  "agentdock_context",
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://agentdock/context"}},
		},
		{
			"name":  "foreign_ui",
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "https://example.test/widget"}},
		},
		{"name": "read_file"},
	}

	marked := nexusToolDescriptors(descriptors)
	if marked[0]["nexus_resource_relay"] != true {
		t.Fatalf("AgentDock app descriptor = %#v", marked[0])
	}
	if marked[0]["nexus_resource_contract"] != nil {
		t.Fatalf("non-central app unexpectedly received a central contract: %#v", marked[0])
	}
	if marked[1]["nexus_resource_relay"] != true || marked[1]["nexus_resource_contract"] != contextResourceContract {
		t.Fatalf("central context descriptor = %#v", marked[1])
	}
	for _, index := range []int{2, 3} {
		if marked[index]["nexus_resource_relay"] != nil {
			t.Fatalf("non-AgentDock resource descriptor %d was marked: %#v", index, marked[index])
		}
	}
}
