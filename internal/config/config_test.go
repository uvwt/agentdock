package config

import (
	"os"
	"testing"
)

func TestNormalizeDefaultsToSandboxedWorkspace(t *testing.T) {
	cfg := Config{}
	cfg.Normalize()

	if cfg.Mode != ModeSandboxed {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeSandboxed)
	}
	if cfg.SandboxMode != SandboxModeLandlock {
		t.Fatalf("SandboxMode = %q, want %q", cfg.SandboxMode, SandboxModeLandlock)
	}
	if cfg.PathPolicy != PathPolicyWorkspace {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyWorkspace)
	}
}

func TestNormalizeHostModeSelectsNoneAndHostPathPolicy(t *testing.T) {
	cfg := Config{Mode: ModeHost}
	cfg.Normalize()

	if cfg.SandboxMode != SandboxModeNone {
		t.Fatalf("SandboxMode = %q, want %q", cfg.SandboxMode, SandboxModeNone)
	}
	if cfg.PathPolicy != PathPolicyHost {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyHost)
	}
}

func TestNormalizeSandboxNoneDoesNotInferHostMode(t *testing.T) {
	cfg := Config{SandboxMode: SandboxModeNone}
	cfg.Normalize()

	if cfg.Mode != ModeSandboxed {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeSandboxed)
	}
	if cfg.PathPolicy != PathPolicyWorkspace {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyWorkspace)
	}
}

func TestNormalizeToolProfileOnlyAllowsFullAndReadOnly(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to full", in: "", want: ProfileFull},
		{name: "full stays full", in: ProfileFull, want: ProfileFull},
		{name: "read only stays read only", in: ProfileReadOnly, want: ProfileReadOnly},
		{name: "removed old full-access profile falls back", in: "uni" + "fied", want: ProfileFull},
		{name: "removed compat profile falls back", in: "compat-readonly-" + "all", want: ProfileFull},
		{name: "unknown profile falls back", in: "legacy", want: ProfileFull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ToolProfile: tt.in}
			cfg.Normalize()
			if cfg.ToolProfile != tt.want {
				t.Fatalf("ToolProfile = %q, want %q", cfg.ToolProfile, tt.want)
			}
		})
	}
}

func TestFromEnvTaskVectorSearchConfig(t *testing.T) {
	t.Setenv("AGENTDOCK_TASK_EMBEDDING_ENDPOINT", "http://127.0.0.1:18788/v1/embeddings")
	t.Setenv("AGENTDOCK_TASK_EMBEDDING_MODEL", "test-model")
	t.Setenv("AGENTDOCK_TASK_VECTOR_TIMEOUT_MS", "1234")
	t.Setenv("AGENTDOCK_TASK_VECTOR_MIN_SCORE", "0.67")
	cfg := FromEnv()
	cfg.Normalize()

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
	cfg.Normalize()
	if cfg.TaskVectorSearch {
		t.Fatal("TaskVectorSearch should respect AGENTDOCK_TASK_VECTOR_SEARCH=false")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
