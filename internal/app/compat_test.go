package app

import (
	"context"
	"io"
	"time"

	"github.com/uvwt/agentdock/internal/session"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
	toolmedia "github.com/uvwt/agentdock/internal/tool/media"
	toolrecall "github.com/uvwt/agentdock/internal/tool/recall"
	"github.com/uvwt/agentdock/internal/workspace"
)

const (
	maxCommandOutputBytes        = toolcommand.MaxOutputBytes
	maxConcurrentCommandSessions = toolcommand.MaxConcurrentSessions
	maxRetainedCommandSessions   = toolcommand.MaxRetainedSessions
	maxTextFileReadBytes         = toolfile.MaxTextFileReadBytes
	maxTextOutputBytes           = toolfile.MaxTextOutputBytes
	maxPrivateNoteSearchResults  = toolrecall.MaxPrivateNoteSearchResults
	maxPrivateNoteReadBytes      = toolrecall.MaxPrivateNoteReadBytes
)

func readBoundedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	return toolcore.ReadBoundedBody(reader, maxBytes)
}

func (r *Runtime) killAllSessions(args map[string]any) (Result, error) {
	return r.command.KillAll(args)
}
func (r *Runtime) commandEnv(skillName string, extra map[string]any) ([]string, error) {
	return r.command.CommandEnv(skillName, extra)
}
func (r *Runtime) internalCommandEnv(extra map[string]string) ([]string, error) {
	return r.command.InternalCommandEnv(extra)
}

type commandInvocation struct {
	workdir string
	env     []string
}

func (r *Runtime) prepareCommandInvocation(args map[string]any, command string) (commandInvocation, error) {
	preview, err := r.command.PreparePreview(args, command)
	if err != nil {
		return commandInvocation{}, err
	}
	return commandInvocation{workdir: preview.Workdir, env: preview.Env}, nil
}
func commandOutputLimit(args map[string]any) int { return toolcommand.OutputLimit(args) }
func waitForSessionsCompletion(sessions []*session.Session, timeout time.Duration) ([]*session.Session, []string) {
	return toolcommand.WaitForSessionsCompletion(sessions, timeout)
}
func (r *Runtime) writeStdin(args map[string]any) (Result, error) {
	return r.command.WriteStdin(args)
}
func (r *Runtime) killSession(args map[string]any) (Result, error) {
	return r.command.KillSession(args)
}
func (r *Runtime) listSessions() (Result, error) { return r.command.ListSessions() }
func (r *Runtime) sessionStatus(args map[string]any) (Result, error) {
	return r.command.SessionStatus(args)
}

func (r *Runtime) editFile(args map[string]any) (Result, error) { return r.files.EditFile(args) }
func (r *Runtime) applyPatch(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ApplyPatch(ctx, args)
}

type searchOptions = toolfile.SearchOptions

func (r *Runtime) searchTextGo(ctx context.Context, path workspace.Path, options searchOptions) (Result, error) {
	return r.files.SearchTextGo(ctx, path, options)
}
func (r *Runtime) parseRGJSON(output []byte, options searchOptions) ([]map[string]any, bool, bool) {
	return r.files.ParseRGJSON(output, options)
}

func (r *Runtime) memoryRequest(ctx context.Context, method, endpoint string, payload any) (Result, error) {
	return r.recall.Request(ctx, method, endpoint, payload)
}
func (r *Runtime) memoryBootstrap(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryBootstrap(ctx, args)
}
func (r *Runtime) memoryRead(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryRead(ctx, args)
}
func (r *Runtime) memoryDiff(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryDiff(ctx, args)
}
func (r *Runtime) memoryPatch(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryPatch(ctx, args)
}
func (r *Runtime) memoryUpdateFact(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryUpdateFact(ctx, args)
}
func (r *Runtime) memoryLint(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryLint(ctx, args)
}
func (r *Runtime) memoryCardCapture(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryCardCapture(ctx, args)
}
func (r *Runtime) memoryCardWrite(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.MemoryCardWrite(ctx, args)
}
func (r *Runtime) notesSearch(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.NotesSearch(ctx, args)
}
func (r *Runtime) notesCapture(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.NotesCapture(ctx, args)
}
func (r *Runtime) notesWrite(ctx context.Context, args map[string]any) (Result, error) {
	return r.recall.NotesWrite(ctx, args)
}

type memoryCardSpec = toolrecall.MemoryCardSpec
type memoryLintFinding = toolrecall.MemoryLintFinding

func memoryCardFromArgs(args map[string]any, requireEvidenceForActive bool) (memoryCardSpec, []string, error) {
	return toolrecall.ParseMemoryCard(args, requireEvidenceForActive)
}
func memoryUnifiedDiff(path, oldText, newText string, maxBytes int) string {
	return toolrecall.MemoryUnifiedDiff(path, oldText, newText, maxBytes)
}
func recallBackendPath(value string) string { return toolrecall.BackendPath(value) }
func recallRequestTimeout(endpoint string) time.Duration {
	return toolrecall.RecallRequestTimeout(endpoint)
}

func (r *Runtime) skillList() (Result, error) { return r.skills.List() }
func (r *Runtime) skillInspect(args map[string]any) (Result, error) {
	return r.skills.Inspect(args)
}
func (r *Runtime) nexusWorkflowJSON(ctx context.Context, method, path string, payload any) (Result, error) {
	return r.taskTools.NexusWorkflowJSON(ctx, method, path, payload)
}
func (r *Runtime) browserRunnerScript() (toolmedia.ControlPath, error) {
	return r.media.BrowserRunnerScript()
}
