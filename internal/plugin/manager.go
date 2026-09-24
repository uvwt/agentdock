package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	store *Store

	pendingMu sync.Mutex
	pending   map[string]pendingActivation
}

type pendingActivation struct {
	transaction ActivationTransaction
	commit      candidateCommit
	release     func()
}

func NewManager(agentDockHome string) (*Manager, error) {
	store, err := NewStore(agentDockHome)
	if err != nil {
		return nil, err
	}
	manager := &Manager{store: store, pending: make(map[string]pendingActivation)}
	if err := manager.recoverInterruptedActivations(); err != nil {
		return nil, err
	}
	manager.cleanupStartupObsoleteVersions()
	return manager, nil
}

func (m *Manager) Store() *Store { return m.store }

func (m *Manager) Validate(source string) Review {
	return m.ValidateSource(context.Background(), source)
}

func (m *Manager) ValidateSource(_ context.Context, source string) Review {
	review := emptyReview()
	_, pkg, cleanup, err := m.prepareCandidateSource(source)
	if err != nil {
		review.Issues = append(review.Issues, err.Error())
		return review
	}
	defer cleanup()
	return buildReview(pkg)
}

func (m *Manager) List() ([]Installed, error) {
	states, err := m.store.List()
	if err != nil {
		return nil, err
	}
	items := make([]Installed, 0, len(states))
	for _, state := range states {
		purging, err := m.removalInProgress(state.Name)
		if err != nil {
			return nil, err
		}
		if purging {
			continue
		}
		root, err := m.store.PackagePath(state.Name, state.Version)
		if err != nil {
			return nil, err
		}
		items = append(items, Installed{State: state, Root: root})
	}
	return items, nil
}

func (m *Manager) Inspect(name string) (Installed, error) {
	name = strings.TrimSpace(name)
	purging, err := m.removalInProgress(name)
	if err != nil {
		return Installed{}, err
	}
	if purging {
		return Installed{}, pluginError("PLUGIN_NOT_FOUND", "inspect", fmt.Errorf("Plugin %q is being purged", name))
	}
	state, err := m.store.Load(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Installed{}, pluginError("PLUGIN_NOT_FOUND", "inspect", fmt.Errorf("Plugin %q is not installed", name))
		}
		return Installed{}, err
	}
	root, err := m.store.PackagePath(state.Name, state.Version)
	if err != nil {
		return Installed{}, err
	}
	return Installed{State: state, Root: root}, nil
}

func (m *Manager) Acquire(ctx context.Context, name string) (Installed, func(), error) {
	name = strings.TrimSpace(name)
	if _, active := m.pendingActivation(name); active {
		return Installed{}, nil, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "resolve", fmt.Errorf("Plugin %q is being activated", name))
	}
	releaseBinding, err := m.store.AcquireBinding(ctx, name)
	if err != nil {
		return Installed{}, nil, err
	}
	installed, err := m.Inspect(name)
	if err != nil {
		releaseBinding()
		return Installed{}, nil, err
	}
	if !installed.Enabled {
		releaseBinding()
		return Installed{}, nil, pluginError("PLUGIN_DISABLED", "resolve", fmt.Errorf("Plugin %q is disabled", name))
	}
	releaseVersion, err := m.store.AcquireVersionRead(installed.Name, installed.Version)
	releaseBinding()
	if err != nil {
		return Installed{}, nil, err
	}
	if err := verifyInstalledPackage(installed); err != nil {
		releaseVersion()
		return Installed{}, nil, err
	}
	return installed, func() {
		releaseVersion()
		m.CleanupObsoleteVersions(installed.Name)
	}, nil
}

func (m *Manager) EnsureDataDir(name string) (string, error) {
	return m.store.EnsureDataDir(name)
}

func (m *Manager) removalInProgress(name string) (bool, error) {
	record, err := m.store.LoadRemovalRecord(strings.TrimSpace(name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return record.Phase == removalPhasePurging, nil
}

func (m *Manager) rejectRemovalInProgress(name, stage string) error {
	purging, err := m.removalInProgress(name)
	if err != nil {
		return err
	}
	if purging {
		return pluginError("PLUGIN_REMOVAL_IN_PROGRESS", stage, fmt.Errorf("Plugin %q purge is already committed and must finish before another lifecycle change", name))
	}
	return nil
}

// InstallReviewedSource stages the source once, verifies that the exact staged
// candidate matches a prior security review, and commits that same snapshot.
func (m *Manager) InstallReviewedSource(ctx context.Context, source string, enabled bool, reviewToken string) (ChangeResult, error) {
	stage, pkg, cleanup, err := m.prepareCandidateSource(source)
	if err != nil {
		return ChangeResult{}, err
	}
	defer cleanup()
	if err := verifyReviewToken(pkg, reviewToken); err != nil {
		return ChangeResult{}, err
	}
	return m.installPreparedCandidate(ctx, stage, pkg, enabled)
}

func (m *Manager) installPreparedCandidate(ctx context.Context, stage string, pkg Package, enabled bool) (result ChangeResult, err error) {
	if _, active := m.pendingActivation(pkg.Manifest.Name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "install", fmt.Errorf("Plugin %q is being activated", pkg.Manifest.Name))
	}
	release, err := m.store.AcquireWrite(ctx, pkg.Manifest.Name)
	if err != nil {
		return ChangeResult{}, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			release()
		}
	}()
	if err := m.recoverActivationLocked(pkg.Manifest.Name); err != nil {
		return ChangeResult{}, err
	}
	if err := m.rejectRemovalInProgress(pkg.Manifest.Name, "install"); err != nil {
		return ChangeResult{}, err
	}

	current, currentErr := m.store.Load(pkg.Manifest.Name)
	if currentErr == nil {
		if current.Version == pkg.Manifest.Version && current.PackageDigest == pkg.PackageDigest {
			return ChangeResult{Action: "install", Name: current.Name, Version: current.Version, PackageDigest: current.PackageDigest, Enabled: current.Enabled, Changed: false}, nil
		}
		return ChangeResult{}, pluginError("PLUGIN_ALREADY_INSTALLED", "install", fmt.Errorf("Plugin %q is already installed; use update", pkg.Manifest.Name))
	}
	if !errors.Is(currentErr, os.ErrNotExist) {
		return ChangeResult{}, currentErr
	}

	state := State{
		SchemaVersion: StateSchemaVersion, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, Description: pkg.Manifest.Description,
		PackageDigest: pkg.PackageDigest, Provenance: pkg.Manifest.Provenance, Enabled: enabled, InstalledAt: time.Now().UTC(),
		Components: stateComponentIndex(pkg.Components), MCPStorageKeys: pluginMCPStorageKeys(pkg.Components.MCP),
		Format: pkg.Format, Warnings: append([]string(nil), pkg.Warnings...),
	}
	ownerID, err := newReaderOwner()
	if err != nil {
		return ChangeResult{}, pluginError("PLUGIN_INSTALL_FAILED", "install.owner", err)
	}
	transaction := ActivationTransaction{
		SchemaVersion: ActivationTransactionSchemaVersion,
		Name:          state.Name, OwnerID: ownerID, Kind: "install", Phase: "pending", Candidate: state,
		CreatedAt: time.Now().UTC(),
	}
	// 首次安装也必须先落 durable activation journal，再准备 candidate package。
	// 正式 state 只在 runtime activation 成功后的 FinalizeActivation 中发布。
	if err := m.store.SaveActivationTransaction(transaction); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_INSTALL_FAILED", "install.journal", err)
	}
	commit, err := m.commitActivationCandidate(stage, pkg, transaction)
	if err != nil {
		_ = m.store.DeleteActivationTransaction(transaction.Name)
		return ChangeResult{}, err
	}
	m.pendingMu.Lock()
	m.pending[state.Name] = pendingActivation{transaction: transaction, commit: commit, release: release}
	m.pendingMu.Unlock()
	keepLock = true
	return ChangeResult{Action: "install", Name: state.Name, Version: state.Version, PackageDigest: state.PackageDigest, Enabled: state.Enabled, Changed: true}, nil
}

// UpdateReviewedSource verifies and commits one staged Portable Plugin snapshot.
// beforeSwitch runs only after package/version checks pass, so the runtime layer
// can stop the current owned MCP immediately before the atomic switch.
func (m *Manager) UpdateReviewedSource(ctx context.Context, source string, reviewToken string, beforeSwitch func(State) error) (ChangeResult, error) {
	stage, pkg, cleanup, err := m.prepareCandidateSource(source)
	if err != nil {
		return ChangeResult{}, err
	}
	defer cleanup()
	if err := verifyReviewToken(pkg, reviewToken); err != nil {
		return ChangeResult{}, err
	}
	return m.updatePreparedCandidate(ctx, stage, pkg, beforeSwitch)
}

func (m *Manager) updatePreparedCandidate(ctx context.Context, stage string, pkg Package, beforeSwitch func(State) error) (result ChangeResult, err error) {
	if _, active := m.pendingActivation(pkg.Manifest.Name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "update", fmt.Errorf("Plugin %q is being activated", pkg.Manifest.Name))
	}
	release, err := m.store.AcquireWrite(ctx, pkg.Manifest.Name)
	if err != nil {
		return ChangeResult{}, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			release()
		}
	}()

	if err := m.recoverActivationLocked(pkg.Manifest.Name); err != nil {
		return ChangeResult{}, err
	}
	if err := m.rejectRemovalInProgress(pkg.Manifest.Name, "update"); err != nil {
		return ChangeResult{}, err
	}
	current, err := m.store.Load(pkg.Manifest.Name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "update", fmt.Errorf("Plugin %q is not installed", pkg.Manifest.Name))
		}
		return ChangeResult{}, err
	}
	if current.Version == pkg.Manifest.Version && current.PackageDigest == pkg.PackageDigest {
		return ChangeResult{Action: "update", Name: current.Name, Version: current.Version, PackageDigest: current.PackageDigest, Enabled: current.Enabled, Changed: false}, nil
	}
	if current.Version == pkg.Manifest.Version && current.Version != VersionLocal {
		return ChangeResult{}, pluginError("PLUGIN_VERSION_DIGEST_CONFLICT", "update.digest", fmt.Errorf("Plugin %s %s is already installed with a different package digest", current.Name, current.Version))
	}
	if beforeSwitch != nil {
		if err := beforeSwitch(current); err != nil {
			return ChangeResult{}, err
		}
	}
	// A local Plugin reuses one package path. Deactivate the old runtime first
	// so its MCP/Skill version leases can drain; waiting before beforeSwitch
	// would deadlock on the runtime lease that this update itself must release.
	if current.Version == VersionLocal && pkg.Manifest.Version == VersionLocal {
		if err := m.store.WaitVersionIdle(ctx, current.Name, current.Version); err != nil {
			return ChangeResult{}, err
		}
	}

	ownerID, err := newReaderOwner()
	if err != nil {
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_FAILED", "update.owner", err)
	}
	candidate := current
	candidate.Version = pkg.Manifest.Version
	candidate.Description = pkg.Manifest.Description
	candidate.PackageDigest = pkg.PackageDigest
	candidate.Provenance = pkg.Manifest.Provenance
	candidate.Components = stateComponentIndex(pkg.Components)
	candidate.MCPStorageKeys = mergeSortedStrings(candidate.MCPStorageKeys, pluginMCPStorageKeys(pkg.Components.MCP))
	candidate.Format = pkg.Format
	candidate.Warnings = append([]string(nil), pkg.Warnings...)
	candidate.InstalledAt = time.Now().UTC()
	transaction := ActivationTransaction{
		SchemaVersion: ActivationTransactionSchemaVersion,
		Name:          candidate.Name, OwnerID: ownerID, Kind: "update", Phase: "pending", Previous: &current, Candidate: candidate,
		LocalReplacement: current.Version == VersionLocal && candidate.Version == VersionLocal,
		CreatedAt:        time.Now().UTC(),
	}
	// 激活事务先持久化，再准备 candidate package。正式 state 在 Finalize 前
	// 始终保持 Previous，因此普通 resolver 永远不会看到 pending candidate。
	if err := m.store.SaveActivationTransaction(transaction); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_FAILED", "update.journal", err)
	}
	commit, err := m.commitActivationCandidate(stage, pkg, transaction)
	if err != nil {
		_ = m.store.DeleteActivationTransaction(transaction.Name)
		return ChangeResult{}, err
	}
	if transaction.LocalReplacement {
		if err := m.promoteLocalCandidate(transaction); err != nil {
			rollbackErr := commit.rollback()
			journalErr := m.store.DeleteActivationTransaction(transaction.Name)
			return ChangeResult{}, pluginError(
				"PLUGIN_UPDATE_FAILED",
				"update.local_promote",
				errors.Join(err, rollbackErr, journalErr),
			)
		}
	}
	m.pendingMu.Lock()
	m.pending[candidate.Name] = pendingActivation{transaction: transaction, commit: commit, release: release}
	m.pendingMu.Unlock()
	keepLock = true
	return ChangeResult{
		Action: "update", Name: candidate.Name, Version: candidate.Version,
		PackageDigest: candidate.PackageDigest, Enabled: candidate.Enabled, Changed: true,
	}, nil
}

func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool) (ChangeResult, error) {
	return m.SetEnabledWithLifecycle(ctx, name, enabled, nil)
}

// SetEnabledWithLifecycle keeps runtime reconciliation and formal state mutation
// under the same Plugin writer lease. The callback receives the target state;
// if state persistence fails after runtime switched, it is called again with the
// previous state before the lease is released.
func (m *Manager) SetEnabledWithLifecycle(ctx context.Context, name string, enabled bool, reconcile func(State) error) (ChangeResult, error) {
	name = strings.TrimSpace(name)
	if _, active := m.pendingActivation(name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "enable", fmt.Errorf("Plugin %q is being activated", name))
	}
	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	if err := m.recoverActivationLocked(name); err != nil {
		return ChangeResult{}, err
	}
	if err := m.rejectRemovalInProgress(name, "enable"); err != nil {
		return ChangeResult{}, err
	}
	previous, err := m.store.Load(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "enable", fmt.Errorf("Plugin %q is not installed", name))
		}
		return ChangeResult{}, err
	}
	if previous.Enabled == enabled {
		action := "disable"
		if enabled {
			action = "enable"
		}
		return ChangeResult{Action: action, Name: previous.Name, Version: previous.Version, PackageDigest: previous.PackageDigest, Enabled: previous.Enabled, Changed: false}, nil
	}

	candidate := previous
	candidate.Enabled = enabled
	if reconcile != nil {
		if err := reconcile(candidate); err != nil {
			stage := "disable.runtime"
			code := "PLUGIN_RUNTIME_DEACTIVATION_FAILED"
			if enabled {
				stage = "enable.runtime"
				code = "PLUGIN_RUNTIME_ACTIVATION_FAILED"
			}
			return ChangeResult{}, pluginError(code, stage, err)
		}
	}
	if err := m.store.Save(candidate); err != nil {
		var rollbackErr error
		if reconcile != nil {
			rollbackErr = reconcile(previous)
		}
		return ChangeResult{}, pluginError("PLUGIN_STATE_CHANGE_FAILED", "enable.state", errors.Join(err, rollbackErr))
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	return ChangeResult{Action: action, Name: candidate.Name, Version: candidate.Version, PackageDigest: candidate.PackageDigest, Enabled: candidate.Enabled, Changed: true}, nil
}

type RemoveLifecycle struct {
	BeforeDelete func(State) error
	Restore      func(State) error
	Purge        func(PurgeOwnership) error
}

func (m *Manager) Remove(ctx context.Context, name, dataPolicy string) (ChangeResult, error) {
	if strings.ToLower(strings.TrimSpace(dataPolicy)) != "keep" {
		return ChangeResult{}, pluginError("PLUGIN_DATA_POLICY_REQUIRED", "remove", errors.New("purge removal requires lifecycle cleanup"))
	}
	return m.RemoveWithLifecycle(ctx, name, dataPolicy, RemoveLifecycle{})
}

// RemoveWithLifecycle 把正式 state、package ownership、Plugin data 和外部 owned
// state 的清理放在同一个 Plugin writer lease 中。purge 先持久化 tombstone 再进入
// 不可逆清理，因此失败后只允许向前重试，不会重新暴露为 installed。
func (m *Manager) RemoveWithLifecycle(ctx context.Context, name, dataPolicy string, lifecycle RemoveLifecycle) (ChangeResult, error) {
	name = strings.TrimSpace(name)
	dataPolicy = strings.ToLower(strings.TrimSpace(dataPolicy))
	if dataPolicy != "keep" && dataPolicy != "purge" {
		return ChangeResult{}, pluginError("PLUGIN_DATA_POLICY_REQUIRED", "remove", errors.New("data_policy must be keep or purge"))
	}
	if dataPolicy == "purge" && lifecycle.Purge == nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.purge", errors.New("purge lifecycle callback is required"))
	}
	if _, active := m.pendingActivation(name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "remove", fmt.Errorf("Plugin %q is being activated", name))
	}

	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	if err := m.recoverActivationLocked(name); err != nil {
		return ChangeResult{}, err
	}

	record, recordErr := m.store.LoadRemovalRecord(name)
	hasRecord := recordErr == nil
	if recordErr != nil && !errors.Is(recordErr, os.ErrNotExist) {
		return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.ownership", recordErr)
	}
	purging := hasRecord && record.Phase == removalPhasePurging
	if purging && dataPolicy != "purge" {
		return ChangeResult{}, pluginError("PLUGIN_REMOVAL_IN_PROGRESS", "remove", fmt.Errorf("Plugin %q purge is already committed", name))
	}

	state, loadErr := m.store.Load(name)
	missing := errors.Is(loadErr, os.ErrNotExist)
	if loadErr != nil && !missing {
		return ChangeResult{}, loadErr
	}
	if missing && dataPolicy == "keep" {
		return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "remove", fmt.Errorf("Plugin %q is not installed", name))
	}

	previousOwnership := PurgeOwnership{Name: name}
	if hasRecord {
		previousOwnership = record.PurgeOwnership
	}
	currentOwnership := PurgeOwnership{Name: name}
	if !missing {
		currentOwnership = purgeOwnershipFromState(state)
	}
	ownership := mergePurgeOwnership(name, previousOwnership, currentOwnership)

	removedState := State{}
	hasRemovedState := false
	if record.RemovedState != nil {
		removedState = *record.RemovedState
		hasRemovedState = true
	}
	if !missing {
		removedState = state
		hasRemovedState = true
	}

	if !missing && lifecycle.BeforeDelete != nil {
		if err := lifecycle.BeforeDelete(state); err != nil {
			return ChangeResult{}, err
		}
	}
	restoreRuntime := func(primary error) error {
		if missing || lifecycle.Restore == nil {
			return primary
		}
		return errors.Join(primary, lifecycle.Restore(state))
	}

	if dataPolicy == "keep" {
		if err := m.store.SaveRemovalOwnership(ownership); err != nil {
			return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.ownership", restoreRuntime(err))
		}
		if err := m.store.Delete(name); err != nil {
			return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.state", restoreRuntime(err))
		}
		m.cleanupObsoleteVersions(name, "")
		return ChangeResult{
			Action: "remove", Name: state.Name, Version: state.Version, PackageDigest: state.PackageDigest,
			Enabled: false, Changed: true,
		}, nil
	}

	if !purging {
		// purging tombstone 是不可逆删除的 durable commit point。它必须先于
		// formal state 和任何 data/env 清理落盘；从这里开始失败只能向前重试。
		tombstone := removalRecord{
			SchemaVersion:  removalRecordSchemaVersion,
			Phase:          removalPhasePurging,
			PurgeOwnership: ownership,
		}
		if hasRemovedState {
			stateCopy := removedState
			tombstone.RemovedState = &stateCopy
		}
		if err := m.store.SaveRemovalRecord(tombstone); err != nil {
			return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.tombstone", restoreRuntime(err))
		}
		purging = true
	}

	// tombstone 已经承诺删除，因此 state 删除失败也不能恢复 runtime/state。
	// Inspect/List/Acquire 会把这个 Plugin 视为不存在，重试继续同一 purge。
	if !missing {
		if err := m.store.Delete(name); err != nil {
			return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.state", err)
		}
	}
	purgeErr := m.store.RemoveData(name)
	purgeErr = errors.Join(purgeErr, lifecycle.Purge(ownership))
	if purgeErr != nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.purge", purgeErr)
	}
	if err := m.store.DeleteRemovalOwnership(name); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.tombstone", err)
	}
	m.cleanupObsoleteVersions(name, "")

	result := ChangeResult{Action: "remove", Name: name, Enabled: false, Changed: !missing}
	if hasRemovedState {
		result.Version = removedState.Version
		result.PackageDigest = removedState.PackageDigest
	}
	return result, nil
}

func pluginMCPStorageKeys(components []MCPComponent) []string {
	keys := make([]string, 0, len(components))
	for _, component := range components {
		if key := strings.TrimSpace(component.StorageKey); key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return uniqueStrings(keys)
}

func mergeSortedStrings(groups ...[]string) []string {
	all := make([]string, 0)
	for _, group := range groups {
		all = append(all, group...)
	}
	sort.Strings(all)
	return uniqueStrings(all)
}

func stateComponentIndex(components ComponentIndex) ComponentIndex {
	out := ComponentIndex{
		Skills: append([]SkillComponent(nil), components.Skills...),
		MCP:    make([]MCPComponent, 0, len(components.MCP)),
	}
	for _, component := range components.MCP {
		out.MCP = append(out.MCP, MCPComponent{
			Name: component.Name, Description: component.Description, Transport: component.Transport,
			RequiredEnv: append([]string(nil), component.RequiredEnv...),
			RuntimeName: component.RuntimeName, StorageKey: component.StorageKey, RelativeSource: component.RelativeSource,
		})
	}
	return out
}

func reviewMCPComponents(components []MCPComponent) []MCPReview {
	items := make([]MCPReview, 0, len(components))
	for _, component := range components {
		envNames := make([]string, 0, len(component.Environment)+len(component.EnvBindings))
		for name := range component.Environment {
			envNames = append(envNames, name)
		}
		for _, name := range component.EnvBindings {
			envNames = append(envNames, name)
		}
		envNames = append(envNames, component.RequiredEnv...)
		sort.Strings(envNames)
		envNames = uniqueStrings(envNames)

		headerNames := make([]string, 0, len(component.Headers)+len(component.HeaderEnv))
		for name := range component.Headers {
			headerNames = append(headerNames, name)
		}
		for name := range component.HeaderEnv {
			headerNames = append(headerNames, name)
		}
		sort.Strings(headerNames)
		headerNames = uniqueStrings(headerNames)

		items = append(items, MCPReview{
			Name: component.Name, Description: component.Description, Transport: component.Transport,
			URL: component.URL, Command: component.Command, CWD: component.CWD,
			EnvironmentNames: envNames, HeaderNames: headerNames,
			RuntimeName: component.RuntimeName,
		})
	}
	return items
}

func (m *Manager) prepareCandidateSource(source string) (stage string, pkg Package, cleanup func(), err error) {
	staged, err := m.stagePluginSource(source)
	if err != nil {
		return "", Package{}, func() {}, err
	}
	normalizedRoot, pkg, cleanupNormalized, err := m.normalizeStagedPlugin(staged)
	if err != nil {
		staged.Cleanup()
		return "", Package{}, func() {}, err
	}
	cleanup = func() {
		cleanupNormalized()
		staged.Cleanup()
	}
	return normalizedRoot, pkg, cleanup, nil
}

type candidateCommit struct {
	destination string
	finish      func()
	rollback    func() error
}

func (m *Manager) commitCandidate(stage string, pkg Package) (candidateCommit, error) {
	destination, err := m.store.PackagePath(pkg.Manifest.Name, pkg.Manifest.Version)
	if err != nil {
		return candidateCommit{}, err
	}
	parent, err := m.store.EnsurePackageParent(pkg.Manifest.Name)
	if err != nil {
		return candidateCommit{}, err
	}
	if filepath.Dir(destination) != parent {
		return candidateCommit{}, pluginError("PLUGIN_INSTALL_FAILED", "package.destination", errors.New("Plugin destination escaped package parent"))
	}

	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return candidateCommit{}, pluginError("PLUGIN_INSTALL_FAILED", "package.destination", errors.New("Plugin destination is not a regular directory"))
		}
		installed, loadErr := LoadPackage(destination)
		if loadErr != nil {
			return candidateCommit{}, loadErr
		}
		if installed.PackageDigest == pkg.PackageDigest {
			return candidateCommit{
				destination: destination,
				finish:      func() {},
				rollback:    func() error { return nil },
			}, nil
		}
		if pkg.Manifest.Version != VersionLocal {
			return candidateCommit{}, pluginError("PLUGIN_VERSION_DIGEST_CONFLICT", "package.destination", fmt.Errorf("Plugin %s %s already exists with a different digest", pkg.Manifest.Name, pkg.Manifest.Version))
		}

		backup, err := m.store.UpdateBackupPath(pkg.Manifest.Name)
		if err != nil {
			return candidateCommit{}, err
		}
		if _, err := os.Lstat(backup); err == nil {
			return candidateCommit{}, pluginError("PLUGIN_UPDATE_PENDING", "package.backup", errors.New("Plugin update backup already exists"))
		} else if !errors.Is(err, os.ErrNotExist) {
			return candidateCommit{}, err
		}
		if err := os.Rename(destination, backup); err != nil {
			return candidateCommit{}, err
		}
		if err := os.Rename(stage, destination); err != nil {
			restoreErr := os.Rename(backup, destination)
			return candidateCommit{}, errors.Join(err, restoreErr)
		}
		return candidateCommit{
			destination: destination,
			finish:      func() { _ = os.RemoveAll(backup) },
			rollback: func() error {
				removeErr := os.RemoveAll(destination)
				restoreErr := os.Rename(backup, destination)
				return errors.Join(removeErr, restoreErr)
			},
		}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return candidateCommit{}, err
	}

	if err := os.Rename(stage, destination); err != nil {
		return candidateCommit{}, err
	}
	return candidateCommit{
		destination: destination,
		finish:      func() {},
		rollback: func() error {
			return os.RemoveAll(destination)
		},
	}, nil
}

func (m *Manager) commitActivationCandidate(stage string, pkg Package, transaction ActivationTransaction) (candidateCommit, error) {
	if !transaction.LocalReplacement {
		return m.commitCandidate(stage, pkg)
	}
	candidatePath, err := m.store.UpdateCandidatePath(transaction.Name, transaction.OwnerID)
	if err != nil {
		return candidateCommit{}, err
	}
	if _, err := os.Lstat(candidatePath); err == nil {
		return candidateCommit{}, pluginError("PLUGIN_UPDATE_PENDING", "package.candidate", errors.New("Plugin activation candidate already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return candidateCommit{}, err
	}
	if err := os.Rename(stage, candidatePath); err != nil {
		return candidateCommit{}, err
	}
	backup, err := m.store.UpdateBackupPath(transaction.Name)
	if err != nil {
		_ = os.RemoveAll(candidatePath)
		return candidateCommit{}, err
	}
	destination, err := m.store.PackagePath(transaction.Name, VersionLocal)
	if err != nil {
		_ = os.RemoveAll(candidatePath)
		return candidateCommit{}, err
	}
	return candidateCommit{
		destination: candidatePath,
		finish: func() {
			_ = os.RemoveAll(candidatePath)
			_ = os.RemoveAll(backup)
		},
		rollback: func() error {
			var result error
			if err := os.RemoveAll(candidatePath); err != nil {
				result = errors.Join(result, err)
			}
			if _, err := os.Lstat(backup); err == nil {
				if removeErr := os.RemoveAll(destination); removeErr != nil {
					result = errors.Join(result, removeErr)
				} else if restoreErr := os.Rename(backup, destination); restoreErr != nil {
					result = errors.Join(result, restoreErr)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, err)
			}
			return result
		},
	}, nil
}

func (m *Manager) promoteLocalCandidate(transaction ActivationTransaction) error {
	if !transaction.LocalReplacement {
		return nil
	}
	candidatePath, err := m.store.UpdateCandidatePath(transaction.Name, transaction.OwnerID)
	if err != nil {
		return err
	}
	destination, err := m.store.PackagePath(transaction.Name, VersionLocal)
	if err != nil {
		return err
	}
	backup, err := m.store.UpdateBackupPath(transaction.Name)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(backup); err == nil {
		return errors.New("Plugin update backup already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		return err
	}
	if err := os.Rename(candidatePath, destination); err != nil {
		restoreErr := os.Rename(backup, destination)
		return errors.Join(err, restoreErr)
	}
	return nil
}

func (m *Manager) activationCandidateRoot(transaction ActivationTransaction) (string, error) {
	if transaction.LocalReplacement && transaction.Phase == "pending" {
		candidatePath, err := m.store.UpdateCandidatePath(transaction.Name, transaction.OwnerID)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(candidatePath); err == nil {
			return candidatePath, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return m.store.PackagePath(transaction.Candidate.Name, transaction.Candidate.Version)
}

func (m *Manager) pendingActivation(name string) (pendingActivation, bool) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pending[name]
	return pending, ok
}

func (m *Manager) finishPendingActivation(name, ownerID string) {
	m.pendingMu.Lock()
	pending, ok := m.pending[name]
	if ok && pending.transaction.OwnerID == ownerID {
		delete(m.pending, name)
	}
	m.pendingMu.Unlock()
	if ok && pending.transaction.OwnerID == ownerID && pending.release != nil {
		pending.release()
	}
}

// AcquireActivationCandidate 是唯一允许读取 pending candidate 的入口。
// 普通 Acquire 会被 activation writer lease 阻塞，因此正式 resolver 在
// Finalize 前 update 只能看到 Previous，首次 install 则仍表现为未安装。
func (m *Manager) AcquireActivationCandidate(name string) (Installed, func(), error) {
	name = strings.TrimSpace(name)
	pending, ok := m.pendingActivation(name)
	if !ok {
		return Installed{}, nil, pluginError("PLUGIN_UPDATE_NOT_OWNER", "activation", fmt.Errorf("Plugin %q has no activation owned by this Manager", name))
	}
	transaction, err := m.store.LoadActivationTransaction(name)
	if err != nil {
		return Installed{}, nil, err
	}
	if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID {
		return Installed{}, nil, pluginError("PLUGIN_UPDATE_NOT_OWNER", "activation", errors.New("Plugin activation journal owner changed"))
	}
	root, err := m.activationCandidateRoot(transaction)
	if err != nil {
		return Installed{}, nil, err
	}
	installed := Installed{State: transaction.Candidate, Root: root}
	if err := verifyInstalledPackage(installed); err != nil {
		return Installed{}, nil, err
	}
	release, err := m.store.AcquireVersionRead(installed.Name, installed.Version)
	if err != nil {
		return Installed{}, nil, err
	}
	return installed, release, nil
}

// FinalizeActivation 在 runtime activation 成功后才发布 candidate。整个过程
// 仍持有跨进程 writer lease，并对 journal owner 与正式 state 做 CAS 校验；
// install 要求正式 state 仍不存在，update 则要求它仍等于 Previous。
func (m *Manager) FinalizeActivation(name string) error {
	name = strings.TrimSpace(name)
	pending, ok := m.pendingActivation(name)
	if !ok {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize", fmt.Errorf("Plugin %q activation is no longer owned by this Manager", name))
	}
	transaction, err := m.store.LoadActivationTransaction(name)
	if err != nil {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize.journal", err)
	}
	if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize.owner", errors.New("Plugin activation journal owner changed"))
	}
	if err := m.verifyActivationCAS(transaction); err != nil {
		return pluginError("PLUGIN_UPDATE_CAS_FAILED", "finalize.state", err)
	}
	candidatePath, err := m.activationCandidateRoot(transaction)
	if err != nil {
		return err
	}
	if err := verifyInstalledPackage(Installed{State: transaction.Candidate, Root: candidatePath}); err != nil {
		return err
	}

	// 正式 state 是唯一 publish commit point。它一旦持久化就不能因为随后
	// journal 清理失败而回滚；若进程在两次 durable write 之间退出，恢复逻辑
	// 会识别 formal state == Candidate，并继续向前完成。
	if err := m.store.Save(transaction.Candidate); err != nil {
		return pluginError("PLUGIN_UPDATE_FINALIZE_FAILED", "finalize.state", err)
	}
	pending.commit.finish()
	transaction.Phase = "committed"
	if err := m.store.SaveActivationTransaction(transaction); err != nil {
		// state 已经是 durable commit point；这里降级为清理告警而不是回滚。
		// 若 journal 仍为 pending，下一次 recovery 会从 formal Candidate 向前收口。
		slog.Warn("mark Plugin activation journal committed failed after publish", "plugin", name, "error", err)
	}
	if err := m.store.DeleteActivationTransaction(name); err != nil {
		slog.Warn("remove committed Plugin activation journal failed", "plugin", name, "error", err)
	}
	m.finishPendingActivation(name, transaction.OwnerID)
	m.CleanupObsoleteVersions(name)
	return nil
}

func (m *Manager) verifyActivationCAS(transaction ActivationTransaction) error {
	current, err := m.store.Load(transaction.Name)
	if transaction.Previous == nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("formal Plugin state appeared during install activation")
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, *transaction.Previous) {
		return errors.New("formal Plugin state changed during activation")
	}
	return nil
}

// CleanupObsoleteVersions removes package versions that are no longer selected.
func (m *Manager) CleanupObsoleteVersions(name string) {
	name = strings.TrimSpace(name)
	current := ""
	state, err := m.store.Load(name)
	if err == nil {
		current = state.Version
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	m.cleanupObsoleteVersions(name, current)
}

// AbortActivation 回滚当前 Manager 拥有的 live activation。若当前进程没有
// owner，则在取得 writer lease 后恢复持久化的 pending transaction。
func (m *Manager) AbortActivation(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if pending, ok := m.pendingActivation(name); ok {
		transaction, err := m.store.LoadActivationTransaction(name)
		if err != nil {
			return pluginError("PLUGIN_UPDATE_NOT_OWNER", "restore.journal", err)
		}
		if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID {
			return pluginError("PLUGIN_UPDATE_NOT_OWNER", "restore.owner", errors.New("pending Plugin activation owner changed"))
		}
		rollbackErr := pending.commit.rollback()
		var stateErr error
		if transaction.Previous == nil {
			stateErr = m.store.Delete(name)
		} else {
			stateErr = m.store.Save(*transaction.Previous)
		}
		journalErr := m.store.DeleteActivationTransaction(name)
		m.finishPendingActivation(name, transaction.OwnerID)
		m.CleanupObsoleteVersions(name)
		return errors.Join(rollbackErr, stateErr, journalErr)
	}

	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return err
	}
	defer release()
	transaction, err := m.store.LoadActivationTransaction(name)
	if err != nil {
		return err
	}
	if transaction.Phase != "pending" {
		return errors.New("Plugin activation is not pending")
	}
	if err := m.rollbackActivationTransaction(transaction); err != nil {
		return err
	}
	m.cleanupObsoleteVersions(name, "")
	return nil
}

func samePluginStateIdentity(left, right State) bool {
	return left.Name == right.Name && left.Version == right.Version && left.PackageDigest == right.PackageDigest
}

func (m *Manager) recoverInterruptedActivations() error {
	transactions, err := m.store.ListActivationTransactions()
	if err != nil {
		return fmt.Errorf("load Plugin activation transactions: %w", err)
	}
	for _, transaction := range transactions {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		release, lockErr := m.store.AcquireWrite(ctx, transaction.Name)
		cancel()
		if lockErr != nil {
			if errors.Is(lockErr, context.DeadlineExceeded) {
				// 另一个活进程仍持有 activation lease；正式 state 还是 Previous，
				// 当前 Manager 可以正常启动，但绝不能抢占或回滚它的事务。
				continue
			}
			return fmt.Errorf("lock Plugin activation transaction %s: %w", transaction.Name, lockErr)
		}
		recoverErr := m.recoverActivationLocked(transaction.Name)
		release()
		if recoverErr != nil {
			return fmt.Errorf("recover Plugin activation %s: %w", transaction.Name, recoverErr)
		}
	}
	return nil
}

// recoverActivationLocked 只能在持有 Plugin writer lease 时调用。能拿到 lease
// 就证明没有活 activation owner。pending 通常回滚，但正式 state 若已经等于
// Candidate，说明 publish commit point 已经持久化，必须向前完成而不能反悔。
func (m *Manager) recoverActivationLocked(name string) error {
	transaction, err := m.store.LoadActivationTransaction(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	switch transaction.Phase {
	case "pending":
		published, err := m.activationCandidatePublished(transaction)
		if err != nil {
			return err
		}
		if published {
			return m.finalizeActivationTransaction(transaction)
		}
		return m.rollbackActivationTransaction(transaction)
	case "committed":
		return m.finalizeActivationTransaction(transaction)
	default:
		return fmt.Errorf("Plugin activation %s has invalid transaction phase %q", transaction.Name, transaction.Phase)
	}
}

func (m *Manager) activationCandidatePublished(transaction ActivationTransaction) (bool, error) {
	current, err := m.store.Load(transaction.Name)
	if errors.Is(err, os.ErrNotExist) {
		if transaction.Previous == nil {
			return false, nil
		}
		return false, errors.New("formal Plugin state disappeared during update activation recovery")
	}
	if err != nil {
		return false, err
	}
	if reflect.DeepEqual(current, transaction.Candidate) {
		return true, nil
	}
	if transaction.Previous != nil && reflect.DeepEqual(current, *transaction.Previous) {
		return false, nil
	}
	return false, errors.New("formal Plugin state does not match activation transaction during recovery")
}

func (m *Manager) rollbackActivationTransaction(transaction ActivationTransaction) error {
	if transaction.Previous == nil {
		// 首次 install 在 pending 阶段没有正式 state；即使 candidate package 已经
		// 准备好，也只需撤销可能意外出现的同一 candidate state 并删除 journal。
		current, err := m.store.Load(transaction.Name)
		if err == nil {
			if !samePluginStateIdentity(current, transaction.Candidate) {
				return errors.New("formal Plugin state changed during install activation recovery")
			}
			if err := m.store.Delete(transaction.Name); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return m.store.DeleteActivationTransaction(transaction.Name)
	}

	previous := *transaction.Previous
	previousPath, err := m.store.PackagePath(previous.Name, previous.Version)
	if err != nil {
		return err
	}
	if transaction.LocalReplacement {
		candidatePath, err := m.store.UpdateCandidatePath(transaction.Name, transaction.OwnerID)
		if err != nil {
			return err
		}
		backup, err := m.store.UpdateBackupPath(transaction.Name)
		if err != nil {
			return err
		}
		if _, backupErr := os.Lstat(backup); backupErr == nil {
			currentPath, err := m.store.PackagePath(transaction.Candidate.Name, transaction.Candidate.Version)
			if err != nil {
				return err
			}
			if err := os.RemoveAll(currentPath); err != nil {
				return fmt.Errorf("remove interrupted local Plugin candidate: %w", err)
			}
			if err := os.Rename(backup, previousPath); err != nil {
				return fmt.Errorf("restore local Plugin backup: %w", err)
			}
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return backupErr
		}
		if err := os.RemoveAll(candidatePath); err != nil {
			return fmt.Errorf("remove pending local Plugin candidate: %w", err)
		}
	}
	installed := Installed{State: previous, Root: previousPath}
	if err := verifyInstalledPackage(installed); err != nil {
		return fmt.Errorf("verify previous Plugin package during recovery: %w", err)
	}
	if err := m.store.Save(previous); err != nil {
		return fmt.Errorf("restore previous Plugin state: %w", err)
	}
	return m.store.DeleteActivationTransaction(transaction.Name)
}

func (m *Manager) finalizeActivationTransaction(transaction ActivationTransaction) error {
	candidatePath, err := m.store.PackagePath(transaction.Candidate.Name, transaction.Candidate.Version)
	if err != nil {
		return err
	}
	installed := Installed{State: transaction.Candidate, Root: candidatePath}
	if err := verifyInstalledPackage(installed); err != nil {
		return fmt.Errorf("verify committed Plugin candidate: %w", err)
	}
	if err := m.store.Save(transaction.Candidate); err != nil {
		return fmt.Errorf("persist committed Plugin state: %w", err)
	}
	if transaction.LocalReplacement {
		backup, err := m.store.UpdateBackupPath(transaction.Name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove committed Plugin rollback backup: %w", err)
		}
	}
	return m.store.DeleteActivationTransaction(transaction.Name)
}

func (m *Manager) cleanupStartupObsoleteVersions() {
	transactions, err := m.store.ListActivationTransactions()
	if err != nil {
		return
	}
	protectedNames := make(map[string]struct{}, len(transactions))
	for _, transaction := range transactions {
		protectedNames[transaction.Name] = struct{}{}
	}
	entries, err := os.ReadDir(m.store.pluginRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || ValidateName(entry.Name()) != nil {
			continue
		}
		if _, protected := protectedNames[entry.Name()]; protected {
			continue
		}
		current := ""
		state, loadErr := m.store.Load(entry.Name())
		switch {
		case loadErr == nil:
			current = state.Version
		case errors.Is(loadErr, os.ErrNotExist):
			// An orphaned package tree has no current version. Remove only
			// versions that are not protected by an active reader lease.
		default:
			// Corrupt or unreadable state may still be recoverable by the user;
			// never turn startup cleanup into destructive recovery.
			continue
		}
		m.cleanupObsoleteVersions(entry.Name(), current)
	}
}

func (m *Manager) cleanupObsoleteVersions(name, current string) {
	protected := map[string]struct{}{}
	if current != "" {
		protected[current] = struct{}{}
	}
	if transaction, err := m.store.LoadActivationTransaction(name); err == nil {
		if transaction.Previous != nil && transaction.Previous.Version != "" {
			protected[transaction.Previous.Version] = struct{}{}
		}
		if transaction.Candidate.Version != "" {
			protected[transaction.Candidate.Version] = struct{}{}
		}
	}

	root := filepath.Join(m.store.pluginRoot, name)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if _, keep := protected[entry.Name()]; keep {
			continue
		}
		if ValidateVersion(entry.Name()) != nil {
			continue
		}
		_, _ = m.store.RemoveVersionIfIdle(name, entry.Name())
	}
	entries, err = os.ReadDir(root)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(root)
	}
}

func verifyInstalledPackage(installed Installed) error {
	info, err := os.Lstat(installed.Root)
	if err != nil {
		return pluginError("PLUGIN_PACKAGE_UNAVAILABLE", "runtime.package", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return pluginError("PLUGIN_PACKAGE_UNAVAILABLE", "runtime.package", errors.New("installed Plugin root is not a regular directory"))
	}
	pkg, err := LoadPackage(installed.Root)
	if err != nil {
		return err
	}
	if pkg.Manifest.Name != installed.Name || pkg.Manifest.Version != installed.Version || pkg.PackageDigest != installed.PackageDigest {
		return pluginError("PLUGIN_PACKAGE_DRIFT", "runtime.package", errors.New("installed Plugin package does not match persisted state"))
	}
	return nil
}

func removeRegularDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a regular directory", path)
	}
	return os.RemoveAll(path)
}

func sortInstalled(items []Installed) {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
}
