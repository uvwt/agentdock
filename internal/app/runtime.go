package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/evolution"
	mcpclient "github.com/uvwt/agentdock/internal/mcp/client"
	"github.com/uvwt/agentdock/internal/taskstate"
	toolacp "github.com/uvwt/agentdock/internal/tool/acp"
	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
	toolmcp "github.com/uvwt/agentdock/internal/tool/mcp"
	toolmedia "github.com/uvwt/agentdock/internal/tool/media"
	toolrecall "github.com/uvwt/agentdock/internal/tool/recall"
	toolskill "github.com/uvwt/agentdock/internal/tool/skill"
	tooltask "github.com/uvwt/agentdock/internal/tool/task"
	"github.com/uvwt/agentdock/internal/workspace"
)

type Result = toolcore.Result

type Runtime struct {
	cfg           config.Config
	ws            *workspace.Workspace
	skills        *toolskill.Service
	mcpClients    *mcpclient.Manager
	tasks         *taskstate.Store
	command       *toolcommand.Service
	files         *toolfile.Service
	dynamicMCP    *toolmcp.Service
	media         *toolmedia.Service
	browser       *toolbrowser.Service
	recall        *toolrecall.Service
	evolution     *evolution.Service
	taskTools     *tooltask.Service
	acp           *toolacp.Service
	lifecycleMu   sync.RWMutex
	commandCtx    context.Context
	commandCancel context.CancelFunc
	closing       bool
	closeOnce     sync.Once
	closeErr      error
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	ws, err := workspace.New(cfg.AgentDockDefaultDir)
	if err != nil {
		return nil, err
	}
	envs, err := envstore.New(cfg.AgentDockHome)
	if err != nil {
		return nil, err
	}
	skills, err := toolskill.New(cfg, ws, envs)
	if err != nil {
		return nil, err
	}
	mcpClients, err := mcpclient.NewManager(cfg.AgentDockHome, envs)
	if err != nil {
		return nil, err
	}
	tasks, err := taskstate.New(filepath.Join(cfg.AgentDockHome, "tasks"))
	if err != nil {
		_ = mcpClients.Close()
		return nil, err
	}
	commandCtx, commandCancel := context.WithCancel(context.Background())
	sessions := toolcommand.NewSessionStore()
	runtime := &Runtime{
		cfg: cfg, ws: ws, skills: skills,
		mcpClients: mcpClients, tasks: tasks, commandCtx: commandCtx, commandCancel: commandCancel,
	}
	runtime.command = toolcommand.New(func() config.Config { return runtime.cfg }, ws, envs, sessions, skills.ResolveActive, runtime.commandExecutionContext)
	runtime.files = toolfile.New(ws, skills.ResolveResource, runtime.command.CommandEnv)
	runtime.dynamicMCP = toolmcp.New(mcpClients, envs)
	runtime.media = toolmedia.New(cfg, ws, runtime.command.InternalCommandEnv)
	runtime.browser = toolbrowser.New(toolbrowser.Config{AgentDockHome: cfg.AgentDockHome, ExecutablePath: cfg.BrowserExecutablePath, CDPURL: cfg.BrowserCDPURL, ReuseExistingCDP: cfg.BrowserReuseExistingCDP})
	runtime.recall = toolrecall.New(func() config.Config { return runtime.cfg })
	runtime.evolution = evolution.New(func() config.Config { return runtime.cfg }, tasks)
	runtime.taskTools = tooltask.New(func() config.Config { return runtime.cfg }, tasks, runtime.evolution)
	if cfg.ACPEnabled {
		acpEnvironment := make(map[string]string, len(cfg.ACPEnvFromEnv))
		for childName, hostName := range cfg.ACPEnvFromEnv {
			value, exists := os.LookupEnv(hostName)
			if !exists {
				_ = runtime.Close()
				return nil, fmt.Errorf("required ACP environment variable %s is missing", hostName)
			}
			acpEnvironment[childName] = value
		}
		manager, err := acpruntime.NewManager(acpruntime.Options{
			Home:       cfg.AgentDockHome,
			DefaultCWD: cfg.AgentDockDefaultDir,
			Agent: acpruntime.AgentSpec{
				Name: cfg.ACPAgentName, Command: cfg.ACPCommand, Args: append([]string(nil), cfg.ACPArgs...), Environment: acpEnvironment,
			},
			MaxConcurrentRuns:  cfg.ACPMaxPrompts,
			InteractionTimeout: time.Duration(cfg.ACPInteractionMS) * time.Millisecond,
		})
		if err != nil {
			_ = runtime.Close()
			return nil, fmt.Errorf("initialize ACP runtime: %w", err)
		}
		runtime.acp = toolacp.New(manager)
	}
	return runtime, nil
}

func (r *Runtime) Config() config.Config           { return r.cfg }
func (r *Runtime) Workspace() *workspace.Workspace { return r.ws }

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var closeErrors []error
		r.lifecycleMu.Lock()
		r.closing = true
		if r.commandCancel != nil {
			r.commandCancel()
		}
		r.lifecycleMu.Unlock()
		if r.acp != nil {
			if err := r.acp.Close(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("close ACP runtime: %w", err))
			}
		}
		if r.browser != nil {
			if err := r.browser.Close(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("close browser runtime: %w", err))
			}
		}
		if r.command != nil {
			if err := r.command.Close(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		if r.mcpClients != nil {
			if err := r.mcpClients.Close(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("close dynamic MCP clients: %w", err))
			}
		}
		r.closeErr = errors.Join(closeErrors...)
	})
	return r.closeErr
}

func (r *Runtime) commandExecutionContext() (context.Context, error) {
	r.lifecycleMu.RLock()
	defer r.lifecycleMu.RUnlock()
	if r.closing || r.commandCtx == nil {
		return nil, toolError("RUNTIME_CLOSING", "AgentDock runtime is shutting down", "runtime")
	}
	return r.commandCtx, nil
}

func (r *Runtime) ToolNames() []string {
	specs := r.availableToolSpecs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func (r *Runtime) Call(ctx context.Context, name string, args map[string]any) (Result, error) {
	if args == nil {
		args = map[string]any{}
	}
	spec, ok := toolSpecByName(name)
	if !ok || !spec.available(r.cfg) {
		return nil, toolErrorDetails("UNKNOWN_TOOL", "tool is not available", "validation", map[string]any{"tool": name})
	}
	if spec.Handler == nil {
		return nil, toolErrorDetails("UNKNOWN_TOOL", "tool has no handler", "validation", map[string]any{"tool": name})
	}
	if err := validateTopLevelArguments(spec, args); err != nil {
		return nil, err
	}
	return spec.Handler(ctx, r, args)
}

// validateTopLevelArguments 只兑现 schema 的顶层严格契约；字段类型和嵌套结构仍由各工具处理。
func validateTopLevelArguments(spec ToolSpec, args map[string]any) error {
	if spec.InputSchema == nil {
		return nil
	}
	schema := spec.InputSchema()
	allowsAdditionalProperties, ok := schema["additionalProperties"].(bool)
	if !ok || allowsAdditionalProperties {
		return nil
	}
	properties, _ := schema["properties"].(map[string]any)
	unknown := make([]string, 0)
	for key := range args {
		if _, allowed := properties[key]; !allowed {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return toolErrorDetails(
		"INVALID_ARGUMENT",
		"unknown tool arguments",
		"validation",
		map[string]any{"tool": spec.Name, "fields": unknown},
	)
}
func (r *Runtime) serverInfo() Result {
	names := r.ToolNames()

	// server_info 是排障入口：这里按主题分组保留字段，避免新增运行能力时
	// 把自检输出重新堆成一行难以审查的 map。
	return Result{
		"server":           config.ServerName,
		"title":            "AgentDock",
		"version":          buildinfo.Version,
		"protocol_version": config.ProtocolVersion,

		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),

		"agentdock_home":        r.cfg.AgentDockHome,
		"agentdock_default_dir": r.cfg.AgentDockDefaultDir,
		"default_cwd":           r.ws.DefaultDisplay(),
		"path_model":            config.PathModel,

		"recall_enabled":               r.cfg.NexusEndpoint != "",
		"nexus_endpoint":               r.cfg.NexusEndpoint,
		"recall_bootstrap_recommended": r.cfg.NexusEndpoint != "",
		"recall_bootstrap_tool":        "recall_bootstrap",
		"recall_bootstrap_args":        map[string]any{},

		"task_state_dir": r.tasks.Root(),
		"command_session_limits": map[string]any{
			"max_running":  toolcommand.MaxConcurrentSessions,
			"max_retained": toolcommand.MaxRetainedSessions,
		},

		"browser_enabled":     r.cfg.BrowserEnabled,
		"acp_enabled":         r.cfg.ACPEnabled,
		"acp_agent":           r.cfg.ACPAgentName,
		"trusted_proxy_cidrs": append([]string{}, r.cfg.TrustedProxyCIDRs...),

		"auth_enabled":  r.authEnabled(),
		"endpoint_path": "/mcp",
		"tools":         names,
		"tool_count":    len(names),
	}
}

func (r *Runtime) authEnabled() bool {
	return r.cfg.AuthRequired()
}
