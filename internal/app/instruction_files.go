package app

import (
	"context"
	"os"

	"github.com/uvwt/agentdock/internal/agentinstructions"
)

// InstructionFiles selects guidance for this request only. It must not change
// Workspace.DefaultCWD: one Runtime can serve multiple independent clients.
func (r *Runtime) InstructionFiles(ctx context.Context, workdir string) (agentinstructions.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return agentinstructions.Snapshot{}, err
	}
	resolved, err := r.ws.ResolveExisting(workdir)
	if err != nil {
		return agentinstructions.Snapshot{}, toolErrorDetails("INVALID_ARGUMENT", "instruction workdir must resolve to an existing host directory", "validation", map[string]any{"workdir": workdir})
	}
	info, err := os.Stat(resolved.Abs)
	if err != nil || !info.IsDir() {
		return agentinstructions.Snapshot{}, toolErrorDetails("INVALID_ARGUMENT", "instruction workdir must be a directory", "validation", map[string]any{"workdir": workdir})
	}
	return agentinstructions.Load(ctx, agentinstructions.Options{
		Home:            r.cfg.AgentDockHome,
		DefaultDir:      r.ws.Root(),
		Workdir:         resolved.Abs,
		GlobalFile:      r.cfg.InstructionsFile,
		DisableAutoLoad: r.cfg.AgentsAutoLoadDisabled,
	})
}
