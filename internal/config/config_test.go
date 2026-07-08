package config

import (
	"os"
	"testing"
)

func TestNormalizeDefaultsToWorkspaceRuntimeProfile(t *testing.T) {
	cfg := Config{}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if cfg.RuntimeProfile != RuntimeProfileWorkspace {
		t.Fatalf("RuntimeProfile = %q, want %q", cfg.RuntimeProfile, RuntimeProfileWorkspace)
	}
	if cfg.HostPaths() {
		t.Fatal("workspace profile should not allow host paths")
	}
	if !cfg.CommandSandboxEnabled() {
		t.Fatal("workspace profile should enable command sandbox")
	}
	if cfg.PathPolicyName() != "workspace" {
		t.Fatalf("PathPolicyName() = %q, want workspace", cfg.PathPolicyName())
	}
	if cfg.CommandSandboxName() != "landlock" {
		t.Fatalf("CommandSandboxName() = %q, want landlock", cfg.CommandSandboxName())
	}
}

func TestRuntimeProfileHostDerivesHostPathsAndNoCommandSandbox(t *testing.T) {
	cfg := Config{RuntimeProfile: RuntimeProfileHost}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if !cfg.HostPaths() {
		t.Fatal("host profile should allow host paths")
	}
	if cfg.CommandSandboxEnabled() {
		t.Fatal("host profile should disable command sandbox")
	}
	if cfg.PathPolicyName() != "host" {
		t.Fatalf("PathPolicyName() = %q, want host", cfg.PathPolicyName())
	}
	if cfg.CommandSandboxName() != "none" {
		t.Fatalf("CommandSandboxName() = %q, want none", cfg.CommandSandboxName())
	}
}

func TestNormalizeRejectsInvalidRuntimeProfile(t *testing.T) {
	cfg := Config{RuntimeProfile: "sandboxed"}
	if err := cfg.Normalize(); err == nil {
		t.Fatal("Normalize() should reject invalid runtime profile")
	}
}

func TestNormalizeToolProfileOnlyAllowsFullAndReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to full", in: "", want: ProfileFull},
		{name: "full stays full", in: ProfileFull, want: ProfileFull},
		{name: "read only stays read only", in: ProfileReadOnly, want: ProfileReadOnly},
		{name: "removed old full-access profile is rejected", in: "uni" + "fied", wantErr: true},
		{name: "removed compat profile is rejected", in: "compat-readonly-" + "all", wantErr: true},
		{name: "unknown profile is rejected", in: "legacy", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ToolProfile: tt.in}
			err := cfg.Normalize()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Normalize() should reject invalid tool profile")
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if cfg.ToolProfile != tt.want {
				t.Fatalf("ToolProfile = %q, want %q", cfg.ToolProfile, tt.want)
			}
		})
	}
}

func TestFromEnvReadsRuntimeProfile(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_PROFILE", RuntimeProfileHost)

	cfg := FromEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.RuntimeProfile != RuntimeProfileHost {
		t.Fatalf("RuntimeProfile = %q, want %q", cfg.RuntimeProfile, RuntimeProfileHost)
	}
	if cfg.PathPolicyName() != "host" || cfg.CommandSandboxName() != "none" {
		t.Fatalf("derived profile = path %q sandbox %q, want host/none", cfg.PathPolicyName(), cfg.CommandSandboxName())
	}
}

func TestFromEnvTaskVectorSearchConfig(t *testing.T) {
	t.Setenv("AGENTDOCK_TASK_EMBEDDING_ENDPOINT", "http://127.0.0.1:18788/v1/embeddings")
	t.Setenv("AGENTDOCK_TASK_EMBEDDING_MODEL", "test-model")
	t.Setenv("AGENTDOCK_TASK_VECTOR_TIMEOUT_MS", "1234")
	t.Setenv("AGENTDOCK_TASK_VECTOR_MIN_SCORE", "0.67")
	cfg := FromEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if !cfg.TaskVectorSearch {
		t.Fatal("TaskVectorSearch should default to enabled when the embedding endpoint is configured")
	}
	if cfg.TaskEmbeddingEndpoint != "http://127.0.0.1:18788/v1/embeddings" {
		t.Fatalf("TaskEmbeddingEndpoint = %q", cfg.TaskEmbeddingEndpoint)
	}
	if cfg.TaskEmbeddingModel != "test-model" {
		t.Fatalf("TaskEmbeddingModel = %q", cfg.TaskEmbeddingModel)
	}
	if cfg.TaskVectorTimeoutMS != 1234 {
		t.Fatalf("TaskVectorTimeoutMS = %d", cfg.TaskVectorTimeoutMS)
	}
	if cfg.TaskVectorMinScore != 0.67 {
		t.Fatalf("TaskVectorMinScore = %v", cfg.TaskVectorMinScore)
	}
}

func TestTaskVectorSearchCanBeDisabled(t *testing.T) {
	t.Setenv("AGENTDOCK_TASK_VECTOR_SEARCH", "false")
	t.Setenv("AGENTDOCK_TASK_EMBEDDING_ENDPOINT", "http://127.0.0.1:18788/v1/embeddings")
	cfg := FromEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.TaskVectorSearch {
		t.Fatal("TaskVectorSearch should respect AGENTDOCK_TASK_VECTOR_SEARCH=false")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
