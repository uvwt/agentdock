package device

import (
	"testing"
)

func TestRegistryDevicesAndHandoff(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d1, err := reg.UpsertDevice(Device{ID: "mac-studio", Name: "Mac Studio", Role: "builder", MCPURL: "http://10.0.0.2:8765/mcp"})
	if err != nil || d1.ID != "mac-studio" {
		t.Fatalf("%#v %v", d1, err)
	}
	if _, err := reg.UpsertDevice(Device{ID: "mac-mini", Name: "Mac mini", Role: "tester"}); err != nil {
		t.Fatal(err)
	}
	list, err := reg.ListDevices()
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	h, err := reg.CreateHandoff("goal_1", "mac-studio", "mac-mini", "tester", "run soak tests")
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "pending" {
		t.Fatalf("%#v", h)
	}
	h, err = reg.UpdateHandoff(h.ID, "accepted")
	if err != nil || h.Status != "accepted" {
		t.Fatalf("%#v %v", h, err)
	}
	items, err := reg.ListHandoffs("goal_1")
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	if _, err := reg.CreateHandoff("goal_1", "mac-studio", "nope", "", ""); err == nil {
		t.Fatal("expected unknown device error")
	}
}
