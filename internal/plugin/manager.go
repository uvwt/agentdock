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
	pending   map[string]pendingUpdate
}

type pendingUpdate struct {
	transaction UpdateTransaction
	commit      candidateCommit
	release     func()
}

func NewManager(agentDockHome string) (*Manager, error) {
	store, err := NewStore(agentDockHome)
	if err != nil {
		return nil, err
	}
	manager := &Manager{store: store, pending: make(map[string]pendingUpdate)}
	if err := manager.recoverInterruptedUpdates(); err != nil {
		return nil, err
	}
	manager.cleanupStartupObsoleteVersions()
	return manager, nil
}

func (m *Manager) Store() *Store { return m.store }

func (m *Manager) Validate(source string) Review {
	return m.ValidateSource(context.Background(), legacyLocalSourceRequest(source))
}

func (m *Manager) ValidateSource(ctx context.Context, request SourceRequest) Review {
	review := emptyReview()
	_, pkg, source, cleanup, err := m.prepareCandidateSource(ctx, request)
	if err != nil {
		review.Issues = append(review.Issues, err.Error())
		return review
	}
	defer cleanup()
	return buildReview(pkg, source)
}

func (m *Manager) List() ([]Installed, error) {
	states, err := m.store.List()
	if err != nil {
		return nil, err
	}
	items := make([]Installed, 0, len(states))
	for _, state := range states {
		root, err := m.store.PackagePath(state.Name, state.Version)
		if err != nil {
			return nil, err
		}
		items = append(items, Installed{State: state, Root: root})
	}
	return items, nil
}

func (m *Manager) Inspect(name string) (Installed, error) {
	state, err := m.store.Load(strings.TrimSpace(name))
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
	if _, active := m.pendingUpdate(name); active {
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

// InstallReviewedSource stages the source once, verifies that the exact staged
// candidate matches a prior security review, and commits that same snapshot.
func (m *Manager) InstallReviewedSource(ctx context.Context, request SourceRequest, enabled bool, reviewToken string) (ChangeResult, error) {
	stage, pkg, src, cleanup, err := m.prepareCandidateSource(ctx, request)
	if err != nil {
		return ChangeResult{}, err
	}
	defer cleanup()
	if err := verifyReviewToken(pkg, src, reviewToken); err != nil {
		return ChangeResult{}, err
	}
	return m.installPreparedCandidate(ctx, stage, pkg, src, enabled)
}

func (m *Manager) installPreparedCandidate(ctx context.Context, stage string, pkg Package, src Source, enabled bool) (ChangeResult, error) {
	if len(pkg.Unsupported) > 0 {
		return ChangeResult{}, pluginError("PLUGIN_UNSUPPORTED_COMPONENT", "install.validate", fmt.Errorf("unsupported components: %s", strings.Join(pkg.Unsupported, ", ")))
	}
	if _, active := m.pendingUpdate(pkg.Manifest.Name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "install", fmt.Errorf("Plugin %q is being activated", pkg.Manifest.Name))
	}
	release, err := m.store.AcquireWrite(ctx, pkg.Manifest.Name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	if err := m.recoverUpdateLocked(pkg.Manifest.Name); err != nil {
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

	commit, err := m.commitCandidate(stage, pkg)
	if err != nil {
		return ChangeResult{}, err
	}
	state := State{
		SchemaVersion: StateSchemaVersion, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version,
		PackageDigest: pkg.PackageDigest, Source: src, Enabled: enabled, InstalledAt: time.Now().UTC(),
		Components: stateComponentIndex(pkg.Components), MCPStorageKeys: pluginMCPStorageKeys(pkg.Components.MCP),
		Compatibility: pkg.Compatibility,
	}
	if err := m.store.Save(state); err != nil {
		rollbackErr := commit.rollback()
		return ChangeResult{}, pluginError("PLUGIN_INSTALL_FAILED", "install.state", errors.Join(err, rollbackErr))
	}
	commit.finish()
	return ChangeResult{Action: "install", Name: state.Name, Version: state.Version, PackageDigest: state.PackageDigest, Enabled: state.Enabled, Changed: true}, nil
}

// UpdateReviewedSource verifies and commits one staged snapshot. beforeSwitch is
// called only after all review/source/version checks pass and before package/state
// mutation, allowing the runtime layer to stop the old owned MCP without a
// second source read or download.
func (m *Manager) UpdateReviewedSource(ctx context.Context, request SourceRequest, confirmSourceChange bool, reviewToken string, beforeSwitch func(State) error) (ChangeResult, error) {
	stage, pkg, src, cleanup, err := m.prepareCandidateSource(ctx, request)
	if err != nil {
		return ChangeResult{}, err
	}
	defer cleanup()
	if err := verifyReviewToken(pkg, src, reviewToken); err != nil {
		return ChangeResult{}, err
	}
	return m.updatePreparedCandidate(ctx, stage, pkg, src, confirmSourceChange, beforeSwitch)
}

func (m *Manager) updatePreparedCandidate(ctx context.Context, stage string, pkg Package, src Source, confirmSourceChange bool, beforeSwitch func(State) error) (result ChangeResult, err error) {
	if len(pkg.Unsupported) > 0 {
		return ChangeResult{}, pluginError("PLUGIN_UNSUPPORTED_COMPONENT", "update.validate", fmt.Errorf("unsupported components: %s", strings.Join(pkg.Unsupported, ", ")))
	}
	if _, active := m.pendingUpdate(pkg.Manifest.Name); active {
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

	if err := m.recoverUpdateLocked(pkg.Manifest.Name); err != nil {
		return ChangeResult{}, err
	}
	current, err := m.store.Load(pkg.Manifest.Name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "update", fmt.Errorf("Plugin %q is not installed", pkg.Manifest.Name))
		}
		return ChangeResult{}, err
	}
	if !samePluginSourceBinding(current.Source, src) && !confirmSourceChange {
		return ChangeResult{}, pluginError("PLUGIN_SOURCE_CHANGE_CONFIRMATION_REQUIRED", "update.source", errors.New("Plugin source binding changed; set confirmed_source_change=true to rebind the installed Plugin"))
	}
	if current.Version == pkg.Manifest.Version && current.PackageDigest == pkg.PackageDigest {
		if current.Source != src {
			current.Source = src
			if err := m.store.Save(current); err != nil {
				return ChangeResult{}, err
			}
		}
		return ChangeResult{Action: "update", Name: current.Name, Version: current.Version, PreviousVersion: current.Version, PackageDigest: current.PackageDigest, Enabled: current.Enabled, Changed: false}, nil
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
	candidate.PackageDigest = pkg.PackageDigest
	candidate.Source = src
	candidate.Components = stateComponentIndex(pkg.Components)
	candidate.MCPStorageKeys = mergeSortedStrings(candidate.MCPStorageKeys, pluginMCPStorageKeys(pkg.Components.MCP))
	candidate.Compatibility = pkg.Compatibility
	candidate.InstalledAt = time.Now().UTC()
	transaction := UpdateTransaction{
		SchemaVersion: UpdateTransactionSchemaVersion,
		Name:          candidate.Name, OwnerID: ownerID, Phase: "pending", Previous: current, Candidate: candidate,
		LocalReplacement: current.Version == VersionLocal && candidate.Version == VersionLocal,
		CreatedAt:        time.Now().UTC(),
	}
	// 激活事务先持久化，再准备 candidate package。正式 state 在 Finalize 前
	// 始终保持 Previous，因此普通 resolver 永远不会看到 pending candidate。
	if err := m.store.SaveUpdateTransaction(transaction); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_FAILED", "update.journal", err)
	}
	commit, err := m.commitUpdateCandidate(stage, pkg, transaction)
	if err != nil {
		_ = m.store.DeleteUpdateTransaction(transaction.Name)
		return ChangeResult{}, err
	}
	if transaction.LocalReplacement {
		if err := m.promoteLocalCandidate(transaction); err != nil {
			rollbackErr := commit.rollback()
			journalErr := m.store.DeleteUpdateTransaction(transaction.Name)
			return ChangeResult{}, pluginError(
				"PLUGIN_UPDATE_FAILED",
				"update.local_promote",
				errors.Join(err, rollbackErr, journalErr),
			)
		}
	}
	m.pendingMu.Lock()
	m.pending[candidate.Name] = pendingUpdate{transaction: transaction, commit: commit, release: release}
	m.pendingMu.Unlock()
	keepLock = true
	return ChangeResult{
		Action: "update", Name: candidate.Name, Version: candidate.Version, PreviousVersion: current.Version,
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
	if _, active := m.pendingUpdate(name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "enable", fmt.Errorf("Plugin %q is being activated", name))
	}
	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	if err := m.recoverUpdateLocked(name); err != nil {
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
// state 的清理放在同一个 Plugin writer lease 中。purge 对 missing package 也是
// 幂等的，因此 keep 后或上一次 purge 失败后都能用同一个 API 重试。
func (m *Manager) RemoveWithLifecycle(ctx context.Context, name, dataPolicy string, lifecycle RemoveLifecycle) (ChangeResult, error) {
	name = strings.TrimSpace(name)
	dataPolicy = strings.ToLower(strings.TrimSpace(dataPolicy))
	if dataPolicy != "keep" && dataPolicy != "purge" {
		return ChangeResult{}, pluginError("PLUGIN_DATA_POLICY_REQUIRED", "remove", errors.New("data_policy must be keep or purge"))
	}
	if dataPolicy == "purge" && lifecycle.Purge == nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.purge", errors.New("purge lifecycle callback is required"))
	}
	if _, active := m.pendingUpdate(name); active {
		return ChangeResult{}, pluginError("PLUGIN_ACTIVATION_IN_PROGRESS", "remove", fmt.Errorf("Plugin %q is being activated", name))
	}

	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	if err := m.recoverUpdateLocked(name); err != nil {
		return ChangeResult{}, err
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
	if recorded, err := m.store.LoadRemovalOwnership(name); err == nil {
		previousOwnership = recorded
	} else if !errors.Is(err, os.ErrNotExist) {
		return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.ownership", err)
	}
	currentOwnership := PurgeOwnership{Name: name}
	if !missing {
		currentOwnership = purgeOwnershipFromState(state)
	}
	ownership := mergePurgeOwnership(name, previousOwnership, currentOwnership)

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
			Enabled: false, Changed: true, DataPolicy: dataPolicy,
		}, nil
	}

	purgeErr := m.store.RemoveData(name)
	purgeErr = errors.Join(purgeErr, lifecycle.Purge(ownership))
	if purgeErr != nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.purge", restoreRuntime(purgeErr))
	}
	if err := m.store.DeleteRemovalOwnership(name); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.ownership", restoreRuntime(err))
	}
	if !missing {
		if err := m.store.Delete(name); err != nil {
			recordErr := m.store.SaveRemovalOwnership(ownership)
			return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.state", restoreRuntime(errors.Join(err, recordErr)))
		}
	}
	m.cleanupObsoleteVersions(name, "")
	return ChangeResult{
		Action: "remove", Name: name, Version: state.Version, PackageDigest: state.PackageDigest,
		Enabled: false, Changed: !missing, DataPolicy: dataPolicy,
	}, nil
}

func samePluginSourceBinding(left, right Source) bool {
	leftAdapter := strings.TrimSpace(left.Adapter)
	rightAdapter := strings.TrimSpace(right.Adapter)
	// P2 persisted only portable Plugins and had no adapter field. Treat an
	// empty historical adapter as portable so P2 -> P3 does not create a
	// false source-rebind prompt.
	if leftAdapter == "" {
		leftAdapter = "portable"
	}
	if rightAdapter == "" {
		rightAdapter = "portable"
	}
	return left.Type == right.Type &&
		left.Ref == right.Ref &&
		left.Selector == right.Selector &&
		left.Subdir == right.Subdir &&
		leftAdapter == rightAdapter &&
		left.Catalog == right.Catalog &&
		left.CatalogItem == right.CatalogItem &&
		left.ResolvedType == right.ResolvedType &&
		left.ResolvedRef == right.ResolvedRef &&
		left.ResolvedSubdir == right.ResolvedSubdir
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
			RuntimeName: component.RuntimeName, StorageKey: component.StorageKey,
		})
	}
	return items
}

func (m *Manager) prepareCandidateSource(ctx context.Context, request SourceRequest) (stage string, pkg Package, src Source, cleanup func(), err error) {
	staged, err := m.stagePluginSource(ctx, request)
	if err != nil {
		return "", Package{}, Source{}, func() {}, err
	}
	adaptedRoot, adapted, cleanupAdapter, err := m.adaptStagedSource(staged, request)
	if err != nil {
		staged.Cleanup()
		return "", Package{}, Source{}, func() {}, err
	}
	stage, err = m.store.TempPath("candidate")
	if err != nil {
		cleanupAdapter()
		staged.Cleanup()
		return "", Package{}, Source{}, func() {}, err
	}
	cleanup = func() {
		_ = os.RemoveAll(stage)
		cleanupAdapter()
		staged.Cleanup()
	}
	if err := copyPluginTree(adaptedRoot, stage); err != nil {
		cleanup()
		return "", Package{}, Source{}, func() {}, pluginError("PLUGIN_SOURCE_INVALID", "source.copy", err)
	}
	pkg, err = LoadPackage(stage)
	if err != nil {
		cleanup()
		return "", Package{}, Source{}, func() {}, err
	}
	pkg.Unsupported = uniqueSortedStrings(append(pkg.Unsupported, adapted.Unsupported...))
	pkg.Warnings = uniqueSortedStrings(append(pkg.Warnings, adapted.Warnings...))
	pkg.Compatibility = adapted.Compatibility
	pkg.Compatibility.Unsupported = append([]string(nil), pkg.Unsupported...)
	pkg.Compatibility.Warnings = append([]string(nil), pkg.Warnings...)
	src = staged.Source
	src.Adapter = pkg.Compatibility.Adapter
	return stage, pkg, src, cleanup, nil
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

func (m *Manager) commitUpdateCandidate(stage string, pkg Package, transaction UpdateTransaction) (candidateCommit, error) {
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

func (m *Manager) promoteLocalCandidate(transaction UpdateTransaction) error {
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

func (m *Manager) updateCandidateRoot(transaction UpdateTransaction) (string, error) {
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

func (m *Manager) pendingUpdate(name string) (pendingUpdate, bool) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pending[name]
	return pending, ok
}

func (m *Manager) finishPendingUpdate(name, ownerID string) {
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
// Finalize 前始终只能看到 Previous。
func (m *Manager) AcquireActivationCandidate(name string) (Installed, func(), error) {
	name = strings.TrimSpace(name)
	pending, ok := m.pendingUpdate(name)
	if !ok {
		return Installed{}, nil, pluginError("PLUGIN_UPDATE_NOT_OWNER", "activation", fmt.Errorf("Plugin %q has no activation owned by this Manager", name))
	}
	transaction, err := m.store.LoadUpdateTransaction(name)
	if err != nil {
		return Installed{}, nil, err
	}
	if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID {
		return Installed{}, nil, pluginError("PLUGIN_UPDATE_NOT_OWNER", "activation", errors.New("Plugin activation journal owner changed"))
	}
	root, err := m.updateCandidateRoot(transaction)
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

// FinalizeUpdate 在 runtime activation 成功后才发布 candidate。整个过程
// 仍持有跨进程 writer lease，并对 journal owner 与正式 Previous state
// 做 CAS 校验，旧 owner 无法 finalize 别人的事务。
func (m *Manager) FinalizeUpdate(name string) error {
	name = strings.TrimSpace(name)
	pending, ok := m.pendingUpdate(name)
	if !ok {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize", fmt.Errorf("Plugin %q activation is no longer owned by this Manager", name))
	}
	transaction, err := m.store.LoadUpdateTransaction(name)
	if err != nil {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize.journal", err)
	}
	if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID {
		return pluginError("PLUGIN_UPDATE_NOT_OWNER", "finalize.owner", errors.New("Plugin activation journal owner changed"))
	}
	current, err := m.store.Load(name)
	if err != nil {
		return pluginError("PLUGIN_UPDATE_CAS_FAILED", "finalize.state", err)
	}
	if !reflect.DeepEqual(current, transaction.Previous) {
		return pluginError("PLUGIN_UPDATE_CAS_FAILED", "finalize.state", errors.New("formal Plugin state changed during activation"))
	}
	candidatePath, err := m.updateCandidateRoot(transaction)
	if err != nil {
		return err
	}
	if err := verifyInstalledPackage(Installed{State: transaction.Candidate, Root: candidatePath}); err != nil {
		return err
	}
	// state 是正式 resolver 的 commit point。若随后 committed marker 落盘失败，
	// 在同一 lease 内恢复 Previous，避免留下一个未决定的可见状态。
	if err := m.store.Save(transaction.Candidate); err != nil {
		return pluginError("PLUGIN_UPDATE_FINALIZE_FAILED", "finalize.state", err)
	}
	transaction.Phase = "committed"
	if err := m.store.SaveUpdateTransaction(transaction); err != nil {
		restoreErr := m.store.Save(transaction.Previous)
		return pluginError("PLUGIN_UPDATE_FINALIZE_FAILED", "finalize.journal", errors.Join(err, restoreErr))
	}
	pending.commit.finish()
	if err := m.store.DeleteUpdateTransaction(name); err != nil {
		// committed marker 已经是 durable decision；删除 journal 失败只影响清理，
		// 重启时 recoverUpdateLocked 会幂等向前收口。
		slog.Warn("remove committed Plugin update journal failed", "plugin", name, "error", err)
	}
	m.finishPendingUpdate(name, transaction.OwnerID)
	m.CleanupObsoleteVersions(name)
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

// RestoreState aborts this Manager's live activation. If there is no in-memory
// owner it may also recover a persisted pending transaction while holding the
// normal writer lock.
func (m *Manager) RestoreState(ctx context.Context, previous State) error {
	if pending, ok := m.pendingUpdate(previous.Name); ok {
		transaction, err := m.store.LoadUpdateTransaction(previous.Name)
		if err != nil {
			return pluginError("PLUGIN_UPDATE_NOT_OWNER", "restore.journal", err)
		}
		if transaction.Phase != "pending" || transaction.OwnerID != pending.transaction.OwnerID ||
			!samePluginStateIdentity(transaction.Previous, previous) {
			return pluginError("PLUGIN_UPDATE_NOT_OWNER", "restore.owner", errors.New("pending Plugin activation does not match requested restore state"))
		}
		rollbackErr := pending.commit.rollback()
		stateErr := m.store.Save(transaction.Previous)
		journalErr := m.store.DeleteUpdateTransaction(previous.Name)
		m.finishPendingUpdate(previous.Name, transaction.OwnerID)
		m.CleanupObsoleteVersions(previous.Name)
		return errors.Join(rollbackErr, stateErr, journalErr)
	}

	release, err := m.store.AcquireWrite(ctx, previous.Name)
	if err != nil {
		return err
	}
	defer release()
	transaction, err := m.store.LoadUpdateTransaction(previous.Name)
	if err != nil {
		return err
	}
	if transaction.Phase != "pending" || !samePluginStateIdentity(transaction.Previous, previous) {
		return errors.New("pending Plugin update does not match requested restore state")
	}
	if err := m.rollbackUpdateTransaction(transaction); err != nil {
		return err
	}
	m.cleanupObsoleteVersions(previous.Name, previous.Version)
	return nil
}

func samePluginStateIdentity(left, right State) bool {
	return left.Name == right.Name && left.Version == right.Version && left.PackageDigest == right.PackageDigest
}

func (m *Manager) recoverInterruptedUpdates() error {
	transactions, err := m.store.ListUpdateTransactions()
	if err != nil {
		return fmt.Errorf("load Plugin update transactions: %w", err)
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
			return fmt.Errorf("lock Plugin update transaction %s: %w", transaction.Name, lockErr)
		}
		recoverErr := m.recoverUpdateLocked(transaction.Name)
		release()
		if recoverErr != nil {
			return fmt.Errorf("recover Plugin update %s: %w", transaction.Name, recoverErr)
		}
	}
	return nil
}

// recoverUpdateLocked 只能在持有 Plugin writer lease 时调用。能拿到 lease
// 就证明没有活 activation owner；pending 可以安全回滚，committed 则向前完成。
func (m *Manager) recoverUpdateLocked(name string) error {
	transaction, err := m.store.LoadUpdateTransaction(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	switch transaction.Phase {
	case "pending":
		return m.rollbackUpdateTransaction(transaction)
	case "committed":
		return m.finalizeUpdateTransaction(transaction)
	default:
		return fmt.Errorf("Plugin update %s has invalid transaction phase %q", transaction.Name, transaction.Phase)
	}
}

func (m *Manager) rollbackUpdateTransaction(transaction UpdateTransaction) error {
	previousPath, err := m.store.PackagePath(transaction.Previous.Name, transaction.Previous.Version)
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
	installed := Installed{State: transaction.Previous, Root: previousPath}
	if err := verifyInstalledPackage(installed); err != nil {
		return fmt.Errorf("verify previous Plugin package during recovery: %w", err)
	}
	if err := m.store.Save(transaction.Previous); err != nil {
		return fmt.Errorf("restore previous Plugin state: %w", err)
	}
	return m.store.DeleteUpdateTransaction(transaction.Name)
}

func (m *Manager) finalizeUpdateTransaction(transaction UpdateTransaction) error {
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
	return m.store.DeleteUpdateTransaction(transaction.Name)
}

func (m *Manager) cleanupStartupObsoleteVersions() {
	transactions, err := m.store.ListUpdateTransactions()
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
	if transaction, err := m.store.LoadUpdateTransaction(name); err == nil {
		if transaction.Previous.Version != "" {
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

func copyPluginTree(source, destination string) error {
	return snapshotPluginTree(source, destination, maxPluginExtractedBytes, maxPluginArchiveFiles)
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
