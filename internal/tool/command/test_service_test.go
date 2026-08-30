package command

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/workspace"
)

func newCommandTestService(t *testing.T) (*Service, *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(cfg.AgentDockDefaultDir)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := envstore.New(cfg.AgentDockHome)
	if err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithCancel(context.Background())
	service := New(
		func() config.Config { return *cfg },
		ws,
		envs,
		nil,
		func() (context.Context, error) { return commandCtx, nil },
	)
	t.Cleanup(func() {
		cancel()
		_ = service.Close()
	})
	return service, cfg
}

func decodeCommandTestRequest[T any](args map[string]any) (T, error) {
	var request T
	data, err := json.Marshal(args)
	if err != nil {
		return request, err
	}
	return request, json.Unmarshal(data, &request)
}

func (s *Service) execArgs(ctx context.Context, args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[ExecRequest](args)
	if err != nil {
		return nil, err
	}
	return s.Exec(ctx, request)
}

func (s *Service) observeArgs(args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[SessionObserveRequest](args)
	if err != nil {
		return nil, err
	}
	return s.Observe(request)
}

func (s *Service) actArgs(args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[SessionActRequest](args)
	if err != nil {
		return nil, err
	}
	return s.Act(request)
}

func (s *Service) prepareCommandInvocationArgs(args map[string]any, command string) (commandInvocation, error) {
	request, err := decodeCommandTestRequest[ExecRequest](args)
	if err != nil {
		return commandInvocation{}, err
	}
	request.Cmd = command
	return s.prepareCommandInvocation(request)
}

func (s *Service) killSessionArgs(args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[SessionActRequest](args)
	if err != nil {
		return nil, err
	}
	return s.killSession(request)
}

func (s *Service) sessionStatusArgs(args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[SessionObserveRequest](args)
	if err != nil {
		return nil, err
	}
	return s.sessionStatus(request)
}

func (s *Service) writeStdinArgs(args map[string]any) (Result, error) {
	request, err := decodeCommandTestRequest[SessionActRequest](args)
	if err != nil {
		return nil, err
	}
	return s.writeStdin(request)
}

func commandOutputLimitArgs(args map[string]any) int {
	request, err := decodeCommandTestRequest[SessionObserveRequest](args)
	if err != nil {
		return 65536
	}
	return commandOutputLimit(request.MaxOutputBytes)
}
