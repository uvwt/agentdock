package commandqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type HealthChecker interface {
	Health(context.Context) (any, error)
}

type MemorySyncer interface {
	Sync(context.Context, json.RawMessage) (any, error)
}

type SkillRouter interface {
	ExecuteSkillCommand(context.Context, string, json.RawMessage, ProgressReporter) (HandlerResult, error)
}

type EnvManager interface {
	ExecuteEnvCommand(context.Context, json.RawMessage, ProgressReporter) (HandlerResult, error)
}

type ServiceController interface {
	InspectService(context.Context, json.RawMessage) (any, error)
	RestartService(context.Context, json.RawMessage) (any, error)
}

type DiagnosticsCollector interface {
	CollectDiagnostics(context.Context, json.RawMessage) (HandlerResult, error)
}

type Reloader interface {
	Reload(context.Context, json.RawMessage) (any, error)
}

type AdapterDependencies struct {
	Health      HealthChecker
	Memory      MemorySyncer
	Skills      SkillRouter
	Env         EnvManager
	Services    ServiceController
	Diagnostics DiagnosticsCollector
	Reloader    Reloader
}

// RegisterAdapters wires the fixed V1 command set to controlled local
// capabilities. It deliberately has no arbitrary shell command adapter.
func RegisterAdapters(executor *Executor, dependencies AdapterDependencies) error {
	if executor == nil {
		return errors.New("executor is required")
	}
	registrations := []CommandHandler{
		FuncHandler{CommandType: "health.check", Run: func(ctx context.Context, _ json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Health == nil {
				return HandlerResult{}, missingDependency("health.check")
			}
			output, err := dependencies.Health.Health(ctx)
			return HandlerResult{Output: output}, err
		}},
		FuncHandler{CommandType: "memory.sync", Run: func(ctx context.Context, payload json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Memory == nil {
				return HandlerResult{}, missingDependency("memory.sync")
			}
			output, err := dependencies.Memory.Sync(ctx, payload)
			return HandlerResult{Output: output}, err
		}},
		FuncHandler{CommandType: "service.inspect", Run: func(ctx context.Context, payload json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Services == nil {
				return HandlerResult{}, missingDependency("service.inspect")
			}
			output, err := dependencies.Services.InspectService(ctx, payload)
			return HandlerResult{Output: output}, err
		}},
		FuncHandler{CommandType: "service.restart", Run: func(ctx context.Context, payload json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Services == nil {
				return HandlerResult{}, missingDependency("service.restart")
			}
			output, err := dependencies.Services.RestartService(ctx, payload)
			return HandlerResult{Output: output}, err
		}},
		FuncHandler{CommandType: "diagnostics.collect", Run: func(ctx context.Context, payload json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Diagnostics == nil {
				return HandlerResult{}, missingDependency("diagnostics.collect")
			}
			return dependencies.Diagnostics.CollectDiagnostics(ctx, payload)
		}},
		FuncHandler{CommandType: "agentdock.reload", Run: func(ctx context.Context, payload json.RawMessage, _ ProgressReporter) (HandlerResult, error) {
			if dependencies.Reloader == nil {
				return HandlerResult{}, missingDependency("agentdock.reload")
			}
			output, err := dependencies.Reloader.Reload(ctx, payload)
			return HandlerResult{Output: output}, err
		}},
		FuncHandler{CommandType: "env.manage", Run: func(ctx context.Context, payload json.RawMessage, progress ProgressReporter) (HandlerResult, error) {
			if dependencies.Env == nil {
				return HandlerResult{}, missingDependency("env.manage")
			}
			return dependencies.Env.ExecuteEnvCommand(ctx, payload, progress)
		}},
	}
	for _, commandType := range []string{"skill.install", "skill.run", "skill.rollback"} {
		commandType := commandType
		registrations = append(registrations, FuncHandler{CommandType: commandType, Run: func(ctx context.Context, payload json.RawMessage, progress ProgressReporter) (HandlerResult, error) {
			if dependencies.Skills == nil {
				return HandlerResult{}, missingDependency(commandType)
			}
			return dependencies.Skills.ExecuteSkillCommand(ctx, commandType, payload, progress)
		}})
	}
	for _, registration := range registrations {
		if err := executor.Register(registration); err != nil {
			return fmt.Errorf("register %s adapter: %w", registration.Type(), err)
		}
	}
	return nil
}

func missingDependency(commandType string) error {
	return &HandlerError{Code: "HANDLER_UNAVAILABLE", Message: "local adapter unavailable for " + commandType, Retryable: false}
}
