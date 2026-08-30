package skill

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/workspace"
)

func newSkillTestService(t *testing.T) (*Service, string) {
	t.Helper()
	return newSkillTestServiceAtRoot(t, t.TempDir())
}

func newSkillTestServiceAtRoot(t *testing.T, root string) (*Service, string) {
	t.Helper()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := envstore.New(cfg.AgentDockHome)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(cfg, ws, envs)
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

func decodeSkillTestRequest[T any](input any) (T, error) {
	var request T
	if typed, ok := input.(T); ok {
		return typed, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return request, err
	}
	return request, json.Unmarshal(data, &request)
}
func (s *Service) packageTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeSkillTestRequest[PackageRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Package(ctx, request)
}
func (s *Service) inspectTest(input any) (Result, error) {
	request, err := decodeSkillTestRequest[InspectRequest](input)
	if err != nil {
		return nil, err
	}
	return s.inspect(request)
}
