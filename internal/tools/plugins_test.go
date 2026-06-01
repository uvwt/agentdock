package tools

import "testing"

func TestValidPluginName(t *testing.T) {
	valid := []string{"rsshub", "cloudsaver", "baidu-netdisk", "foo_bar", "foo.bar", "A1"}
	for _, name := range valid {
		if !validPluginName(name) {
			t.Fatalf("validPluginName rejected %q", name)
		}
	}
	invalid := []string{"", ".", "..", "../x", "x/y", `x\\y`, "x y", "x$"}
	for _, name := range invalid {
		if validPluginName(name) {
			t.Fatalf("validPluginName accepted %q", name)
		}
	}
}

func TestSafeJoinStaysInsidePluginRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "subdir"); err != nil {
		t.Fatalf("safeJoin rejected subdir: %v", err)
	}
	if _, err := safeJoin(root, "../escape"); err == nil {
		t.Fatalf("safeJoin accepted path outside root")
	}
}
