package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	opts  Options
	store *sessionStore

	mu                 sync.RWMutex
	process            *agentProcess
	sessions           map[string]SessionRecord
	remoteToLocal      map[string]string
	loaded             map[string]sessionLifecycleResponse
	projections        map[string]SessionProjection
	runs               map[string]*Run
	activeRunBySession map[string]string
	historyCollectors  map[string]*historyCollector
	interactions       map[string]*Interaction
	terminalSessions   map[string]SessionStatus
	sessionOperations  map[string]int
	operationsCond     *sync.Cond
	closed             bool

	startMu   sync.Mutex
	runSlots  chan struct{}
	closedCh  chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func NewManager(opts Options) (*Manager, error) {
	if strings.TrimSpace(opts.Home) == "" {
		return nil, errors.New("ACP manager home is required")
	}
	if strings.TrimSpace(opts.DefaultCWD) == "" {
		return nil, errors.New("ACP default cwd is required")
	}
	if strings.TrimSpace(opts.Agent.Name) == "" || strings.TrimSpace(opts.Agent.Command) == "" {
		return nil, errors.New("ACP agent name and command are required")
	}
	if opts.MaxConcurrentRuns <= 0 {
		opts.MaxConcurrentRuns = 2
	}
	if opts.InteractionTimeout <= 0 {
		opts.InteractionTimeout = 5 * time.Minute
	}
	store, err := newSessionStore(opts.Home, opts.Agent.Name)
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		opts:               opts,
		store:              store,
		sessions:           make(map[string]SessionRecord),
		remoteToLocal:      make(map[string]string),
		loaded:             make(map[string]sessionLifecycleResponse),
		projections:        make(map[string]SessionProjection),
		runs:               make(map[string]*Run),
		activeRunBySession: make(map[string]string),
		historyCollectors:  make(map[string]*historyCollector),
		interactions:       make(map[string]*Interaction),
		terminalSessions:   make(map[string]SessionStatus),
		sessionOperations:  make(map[string]int),
		runSlots:           make(chan struct{}, opts.MaxConcurrentRuns),
		closedCh:           make(chan struct{}),
	}
	manager.operationsCond = sync.NewCond(&manager.mu)
	records, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if existing := manager.remoteToLocal[record.RemoteSessionID]; existing != "" && existing != record.ID {
			return nil, newError("ACP_SESSION_STATE_INVALID", "multiple local ACP sessions reference the same remote session id", false, map[string]any{"remote_session_id": record.RemoteSessionID, "session_ids": []string{existing, record.ID}}, nil)
		}
		manager.sessions[record.ID] = record
		manager.remoteToLocal[record.RemoteSessionID] = record.ID
	}
	return manager, nil
}

func (m *Manager) AgentInfo(ctx context.Context) (InitializeResult, error) {
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return InitializeResult{}, err
	}
	return process.initialize, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		close(m.closedCh)
		m.operationsCond.Broadcast()
		activeRuns := make([]*Run, 0, len(m.activeRunBySession))
		for _, run := range m.runs {
			activeRuns = append(activeRuns, run)
		}
		for _, interaction := range m.interactions {
			if interaction.Status == InteractionPending {
				interaction.Status = InteractionExpired
				select {
				case interaction.respond <- interactionResponse{cancelled: true}:
				default:
				}
			}
		}
		process := m.process
		m.process = nil
		m.loaded = make(map[string]sessionLifecycleResponse)
		m.projections = make(map[string]SessionProjection)
		m.mu.Unlock()
		for _, run := range activeRuns {
			if runStatus(run) != RunRunning {
				continue
			}
			// Settle the run before cancelling its request context. Otherwise the
			// request goroutine can win the race and persist a normal cancellation,
			// hiding the fact that AgentDock itself interrupted the turn.
			m.finishRun(run, RunInterrupted, "agentdock_shutdown", nil)
			if run.cancel != nil {
				run.cancel()
			}
		}
		if process != nil {
			m.closeErr = process.Close()
		}
	})
	return m.closeErr
}

func (m *Manager) ensureProcess(ctx context.Context) (*agentProcess, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, newError("ACP_MANAGER_CLOSED", "ACP manager is closed", false, nil, nil)
	}
	process := m.process
	if process != nil {
		select {
		case <-process.connection.Closed():
			process = nil
		default:
		}
	}
	m.mu.RUnlock()
	if process != nil {
		return process, nil
	}

	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	process = m.process
	m.mu.RUnlock()
	if process != nil {
		select {
		case <-process.connection.Closed():
			_ = process.Close()
		default:
			return process, nil
		}
	}

	started, err := startAgentProcess(ctx, m.opts.Agent, m.opts.DefaultCWD, m.handleRequest, m.handleNotification)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = started.Close()
		return nil, newError("ACP_MANAGER_CLOSED", "ACP manager closed during agent startup", false, nil, nil)
	}
	old := m.process
	m.process = started
	m.loaded = make(map[string]sessionLifecycleResponse)
	m.projections = make(map[string]SessionProjection)
	m.mu.Unlock()
	if old != nil && old != started {
		_ = old.Close()
	}
	return started, nil
}

func (m *Manager) session(id string) (SessionRecord, error) {
	m.mu.RLock()
	record, exists := m.sessions[id]
	m.mu.RUnlock()
	if exists {
		return record, nil
	}
	record, err := m.store.Get(id)
	if err != nil {
		return SessionRecord{}, err
	}
	m.mu.Lock()
	if existing := m.remoteToLocal[record.RemoteSessionID]; existing != "" && existing != record.ID {
		m.mu.Unlock()
		return SessionRecord{}, newError("ACP_SESSION_STATE_INVALID", "multiple local ACP sessions reference the same remote session id", false, map[string]any{"remote_session_id": record.RemoteSessionID, "session_ids": []string{existing, record.ID}}, nil)
	}
	m.sessions[id] = record
	m.remoteToLocal[record.RemoteSessionID] = record.ID
	m.mu.Unlock()
	return record, nil
}

func (m *Manager) run(id string) (*Run, error) {
	m.mu.RLock()
	run := m.runs[id]
	m.mu.RUnlock()
	if run == nil {
		return nil, newError("ACP_RUN_NOT_FOUND", "ACP prompt run was not found", false, map[string]any{"run_id": id}, nil)
	}
	return run, nil
}

func newID(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", newError("ACP_ID_FAILED", "generate ACP identifier", false, nil, err)
	}
	return prefix + "_" + hex.EncodeToString(random[:]), nil
}
