//go:build windows

package tools

import "testing"

func TestWindowsFileToolSchemasExposeWSLRuntime(t *testing.T) {
	for _, name := range []string{"read_file", "list_dir", "list_files", "search_text", "file_edit"} {
		properties := InputSchema(name)["properties"].(map[string]any)
		runtimeProperty, ok := properties["runtime"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema is missing runtime: %#v", name, properties)
		}
		enum, ok := runtimeProperty["enum"].([]string)
		if !ok || len(enum) != 2 || enum[0] != "windows" || enum[1] != "wsl" {
			t.Fatalf("%s runtime enum = %#v", name, runtimeProperty["enum"])
		}
		if _, ok := properties["wsl_distribution"]; !ok {
			t.Fatalf("%s schema is missing wsl_distribution", name)
		}

		outputProperties := OutputSchema(name)["properties"].(map[string]any)
		if _, ok := outputProperties["runtime"]; !ok {
			t.Fatalf("%s output schema is missing runtime", name)
		}
		if _, ok := outputProperties["wsl_distribution"]; !ok {
			t.Fatalf("%s output schema is missing wsl_distribution", name)
		}
	}
}

func TestResolveWSLFilePathAcceptsLinuxAndWindowsDrivePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "Linux home", path: "/home/a/project", want: "/home/a/project"},
		{name: "Linux mount", path: "/mnt/d/Project/demo", want: "/mnt/d/Project/demo"},
		{name: "Windows drive", path: `D:\Project\demo`, want: "/mnt/d/Project/demo"},
		{name: "extended Windows drive", path: `\\?\E:\Work`, want: "/mnt/e/Work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveWSLFilePath(test.path)
			if err != nil {
				t.Fatalf("resolveWSLFilePath(%q): %v", test.path, err)
			}
			if got != test.want {
				t.Fatalf("resolveWSLFilePath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
	if _, err := resolveWSLFilePath("relative/path"); err == nil {
		t.Fatal("expected relative WSL file path to be rejected")
	}
}

func TestWSLFileErrorPhaseSeparatesValidationFromRuntime(t *testing.T) {
	if phase := wslFileErrorPhase("SYMLINK_NOT_ALLOWED"); phase != "validation" {
		t.Fatalf("symlink phase = %q", phase)
	}
	if phase := wslFileErrorPhase("WSL_FILE_RUNTIME_ERROR"); phase != "runtime" {
		t.Fatalf("runtime phase = %q", phase)
	}
}

func TestSelectWindowsFileRuntime(t *testing.T) {
	selection, err := selectFileRuntime(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Runtime != "windows" || selection.Distribution != "" {
		t.Fatalf("default selection = %#v", selection)
	}

	selection, err = selectFileRuntime(map[string]any{"runtime": "wsl", "wsl_distribution": "Ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Runtime != "wsl" || selection.Distribution != "Ubuntu" {
		t.Fatalf("WSL selection = %#v", selection)
	}

	if _, err := selectFileRuntime(map[string]any{"runtime": "windows", "wsl_distribution": "Ubuntu"}); err == nil {
		t.Fatal("expected Windows runtime to reject wsl_distribution")
	}
}
