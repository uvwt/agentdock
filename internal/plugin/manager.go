package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	previous State
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
	release, err := m.store.AcquireWrite(ctx, pkg.Manifest.Name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()

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

func (m *Manager) updatePreparedCandidate(ctx context.Context, stage string, pkg Package, src Source, confirmSourceChange bool, beforeSwitch func(State) error) (ChangeResult, error) {
	if len(pkg.Unsupported) > 0 {
		return ChangeResult{}, pluginError("PLUGIN_UNSUPPORTED_COMPONENT", "update.validate", fmt.Errorf("unsupported components: %s", strings.Join(pkg.Unsupported, ", ")))
	}
	release, err := m.store.AcquireWrite(ctx, pkg.Manifest.Name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()

	current, err := m.store.Load(pkg.Manifest.Name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "update", fmt.Errorf("Plugin %q is not installed", pkg.Manifest.Name))
		}
		return ChangeResult{}, err
	}
	if m.hasPendingUpdate(pkg.Manifest.Name) {
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_PENDING", "update", fmt.Errorf("Plugin %q still has an unfinalized update transaction", pkg.Manifest.Name))
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
	if current.Version == VersionLocal && pkg.Manifest.Version == VersionLocal {
		if err := m.store.WaitVersionIdle(ctx, current.Name, current.Version); err != nil {
			return ChangeResult{}, err
		}
	}
	if beforeSwitch != nil {
		if err := beforeSwitch(current); err != nil {
			return ChangeResult{}, err
		}
	}

	previous := current
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
		Name:          candidate.Name, Phase: "pending", Previous: previous, Candidate: candidate,
		LocalReplacement: previous.Version == VersionLocal && candidate.Version == VersionLocal,
		CreatedAt:        time.Now().UTC(),
	}
	// Journal first: after this point every package/state mutation is recoverable
	// across process termination before runtime activation is finalized.
	if err := m.store.SaveUpdateTransaction(transaction); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_FAILED", "update.journal", err)
	}
	if _, err := m.commitCandidate(stage, pkg); err != nil {
		_ = m.store.DeleteUpdateTransaction(transaction.Name)
		return ChangeResult{}, err
	}
	if err := m.store.Save(candidate); err != nil {
		rollbackErr := m.rollbackUpdateTransaction(transaction)
		if rollbackErr == nil {
			m.cleanupObsoleteVersions(previous.Name, previous.Version)
		}
		return ChangeResult{}, pluginError("PLUGIN_UPDATE_FAILED", "update.state", errors.Join(err, rollbackErr))
	}
	m.pendingMu.Lock()
	m.pending[candidate.Name] = pendingUpdate{previous: previous}
	m.pendingMu.Unlock()
	return ChangeResult{
		Action: "update", Name: candidate.Name, Version: candidate.Version, PreviousVersion: previous.Version,
		PackageDigest: candidate.PackageDigest, Enabled: candidate.Enabled, Changed: true,
	}, nil
}

func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool) (ChangeResult, error) {
	name = strings.TrimSpace(name)
	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	state, err := m.store.Load(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "enable", fmt.Errorf("Plugin %q is not installed", name))
		}
		return ChangeResult{}, err
	}
	changed := state.Enabled != enabled
	state.Enabled = enabled
	if changed {
		if err := m.store.Save(state); err != nil {
			return ChangeResult{}, err
		}
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	return ChangeResult{Action: action, Name: state.Name, Version: state.Version, PackageDigest: state.PackageDigest, Enabled: state.Enabled, Changed: changed}, nil
}

func (m *Manager) Remove(ctx context.Context, name, dataPolicy string) (ChangeResult, error) {
	name = strings.TrimSpace(name)
	dataPolicy = strings.ToLower(strings.TrimSpace(dataPolicy))
	if dataPolicy != "keep" && dataPolicy != "purge" {
		return ChangeResult{}, pluginError("PLUGIN_DATA_POLICY_REQUIRED", "remove", errors.New("data_policy must be keep or purge"))
	}
	release, err := m.store.AcquireWrite(ctx, name)
	if err != nil {
		return ChangeResult{}, err
	}
	defer release()
	state, err := m.store.Load(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChangeResult{}, pluginError("PLUGIN_NOT_FOUND", "remove", fmt.Errorf("Plugin %q is not installed", name))
		}
		return ChangeResult{}, err
	}
	if err := m.store.Delete(name); err != nil {
		return ChangeResult{}, pluginError("PLUGIN_REMOVE_FAILED", "remove.state", err)
	}
	// Package versions with active readers remain until their last lease is
	// released. New bindings are already impossible because state is gone.
	m.cleanupObsoleteVersions(name, "")
	if dataPolicy == "purge" {
		if err := m.store.RemoveData(name); err != nil {
			return ChangeResult{}, pluginError("PLUGIN_PURGE_FAILED", "remove.data", err)
		}
	}
	return ChangeResult{Action: "remove", Name: state.Name, Version: state.Version, PackageDigest: state.PackageDigest, Enabled: false, Changed: true, DataPolicy: dataPolicy}, nil
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

func (m *Manager) hasPendingUpdate(name string) bool {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	_, ok := m.pending[name]
	return ok
}

// FinalizeUpdate durably decides the update after runtime activation succeeds.
// The committed phase is persisted before rollback material is destroyed so a
// crash during finalization resumes forward instead of attempting rollback.
func (m *Manager) FinalizeUpdate(name string) error {
	name = strings.TrimSpace(name)
	transaction, err := m.store.LoadUpdateTransaction(name)
	if errors.Is(err, os.ErrNotExist) {
		m.CleanupObsoleteVersions(name)
		return nil
	}
	if err != nil {
		return err
	}
	transaction.Phase = "committed"
	if err := m.store.SaveUpdateTransaction(transaction); err != nil {
		return err
	}
	if err := m.finalizeUpdateTransaction(transaction); err != nil {
		return err
	}
	m.pendingMu.Lock()
	delete(m.pending, name)
	m.pendingMu.Unlock()
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

// RestoreState rolls back an unfinalized update using the persisted journal.
// previous is still supplied by the runtime layer as an identity assertion.
func (m *Manager) RestoreState(ctx context.Context, previous State) error {
	release, err := m.store.AcquireWrite(ctx, previous.Name)
	if err != nil {
		return err
	}
	defer release()

	transaction, err := m.store.LoadUpdateTransaction(previous.Name)
	if err != nil {
		return err
	}
	if transaction.Phase != "pending" || transaction.Previous.Name != previous.Name ||
		transaction.Previous.Version != previous.Version || transaction.Previous.PackageDigest != previous.PackageDigest {
		return errors.New("pending Plugin update does not match requested restore state")
	}
	if err := m.rollbackUpdateTransaction(transaction); err != nil {
		return err
	}
	m.pendingMu.Lock()
	delete(m.pending, previous.Name)
	m.pendingMu.Unlock()
	// Non-local candidates are removed only through the version-lease-aware
	// cleanup path. An old candidate may still be serving an in-flight reader.
	m.CleanupObsoleteVersions(previous.Name)
	return nil
}

func (m *Manager) recoverInterruptedUpdates() error {
	transactions, err := m.store.ListUpdateTransactions()
	if err != nil {
		return fmt.Errorf("load Plugin update transactions: %w", err)
	}
	for _, transaction := range transactions {
		switch transaction.Phase {
		case "pending":
			if err := m.rollbackUpdateTransaction(transaction); err != nil {
				return fmt.Errorf("recover pending Plugin update %s: %w", transaction.Name, err)
			}
		case "committed":
			if err := m.finalizeUpdateTransaction(transaction); err != nil {
				return fmt.Errorf("finish committed Plugin update %s: %w", transaction.Name, err)
			}
		default:
			return fmt.Errorf("Plugin update %s has invalid transaction phase %q", transaction.Name, transaction.Phase)
		}
	}
	return nil
}

func (m *Manager) rollbackUpdateTransaction(transaction UpdateTransaction) error {
	previousPath, err := m.store.PackagePath(transaction.Previous.Name, transaction.Previous.Version)
	if err != nil {
		return err
	}
	if transaction.LocalReplacement {
		candidatePath, err := m.store.PackagePath(transaction.Candidate.Name, transaction.Candidate.Version)
		if err != nil {
			return err
		}
		backup, err := m.store.UpdateBackupPath(transaction.Name)
		if err != nil {
			return err
		}
		if _, backupErr := os.Lstat(backup); backupErr == nil {
			if err := os.RemoveAll(candidatePath); err != nil {
				return fmt.Errorf("remove interrupted local Plugin candidate: %w", err)
			}
			if err := os.Rename(backup, previousPath); err != nil {
				return fmt.Errorf("restore local Plugin backup: %w", err)
			}
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return backupErr
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
	entries, err := os.ReadDir(m.store.pluginRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || ValidateName(entry.Name()) != nil {
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
	m.pendingMu.Lock()
	if pending, ok := m.pending[name]; ok && pending.previous.Version != "" {
		protected[pending.previous.Version] = struct{}{}
	}
	m.pendingMu.Unlock()

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
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special file is not allowed: %s", path)
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := ioCopy(output, input)
		closeOut := output.Close()
		closeIn := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOut != nil {
			return closeOut
		}
		return closeIn
	})
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

func ioCopy(dst *os.File, src *os.File) (int64, error) {
	return dst.ReadFrom(src)
}

func sortInstalled(items []Installed) {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
}
