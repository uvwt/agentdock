package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// rollbackJournal 同时记录文件快照和服务运行态。
// 回滚顺序必须是：停掉新进程 → 还原文件 → 重载 unit → 按安装前状态拉起旧服务。
// 只有 Restore 成功才允许把事务写成 rolled_back；还原失败必须是 failed/rollback_failed。
type rollbackJournal struct {
	Dir      string           `json:"dir"`
	Backups  []journalBackup  `json:"backups"`
	Created  []string         `json:"created"`
	Services []journalService `json:"services,omitempty"`
}

// journalService 记住安装前服务是否在跑，以及本次事务有没有动过它。
type journalService struct {
	Manager     string `json:"manager"`
	Name        string `json:"name"`
	Domain      string `json:"domain,omitempty"`
	Plist       string `json:"plist,omitempty"`
	WasActive   bool   `json:"was_active"`
	WasEnabled  bool   `json:"was_enabled"`
	StoppedByUs bool   `json:"stopped_by_us"`
	LoadedByUs  bool   `json:"loaded_by_us"`
	StartedByUs bool   `json:"started_by_us"`
}

type journalBackup struct {
	Original string `json:"original"`
	Backup   string `json:"backup,omitempty"`
	Existed  bool   `json:"existed"`
}

func newJournal(stateRoot, transactionID string) *rollbackJournal {
	return &rollbackJournal{
		Dir: filepath.Join(stateRoot, "install", "rollback", transactionID),
	}
}

func loadJournal(stateRoot, transactionID string) (*rollbackJournal, error) {
	dir := filepath.Join(stateRoot, "install", "rollback", transactionID)
	data, err := os.ReadFile(filepath.Join(dir, "journal.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var journal rollbackJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("parse rollback journal: %w", err)
	}
	journal.Dir = dir
	return &journal, nil
}

func (journal *rollbackJournal) Snapshot(path string) error {
	if journal == nil {
		return errors.New("rollback journal is required")
	}
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			journal.Backups = append(journal.Backups, journalBackup{Original: path, Existed: false})
			return journal.persist()
		}
		return err
	}
	if err := os.MkdirAll(journal.Dir, 0o700); err != nil {
		return err
	}
	backup := filepath.Join(journal.Dir, strconv.Itoa(len(journal.Backups))+filepath.Ext(path))
	if info.IsDir() {
		backup = filepath.Join(journal.Dir, strconv.Itoa(len(journal.Backups)))
	}
	if err := copyTree(path, backup, info.Mode()); err != nil {
		return fmt.Errorf("snapshot %s: %w", path, err)
	}
	journal.Backups = append(journal.Backups, journalBackup{Original: path, Backup: backup, Existed: true})
	return journal.persist()
}

func (journal *rollbackJournal) NoteCreated(path string) error {
	if path == "" {
		return nil
	}
	journal.Created = append(journal.Created, path)
	return journal.persist()
}

func (journal *rollbackJournal) NoteService(service journalService) error {
	if journal == nil {
		return errors.New("rollback journal is required")
	}
	journal.Services = append(journal.Services, service)
	return journal.persist()
}

func (journal *rollbackJournal) updateService(name string, mutate func(*journalService)) error {
	for i := range journal.Services {
		if journal.Services[i].Name == name {
			mutate(&journal.Services[i])
			return journal.persist()
		}
	}
	return fmt.Errorf("journal missing service %s", name)
}

func (journal *rollbackJournal) Restore(ctx context.Context, request Request) error {
	var failures []error
	for i := len(journal.Services) - 1; i >= 0; i-- {
		service := journal.Services[i]
		// 只要记进 journal 就停：enable --now 之后、StartedByUs 落盘之前崩溃，
		// 也必须把新进程停掉，不能因为标志没写上就宣称 rolled_back。
		if err := stopJournalService(ctx, request, service); err != nil {
			failures = append(failures, fmt.Errorf("stop %s: %w", service.Name, err))
		}
	}
	for i := len(journal.Created) - 1; i >= 0; i-- {
		if err := os.RemoveAll(journal.Created[i]); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Errorf("remove %s: %w", journal.Created[i], err))
		}
	}
	for i := len(journal.Backups) - 1; i >= 0; i-- {
		item := journal.Backups[i]
		if !item.Existed {
			if err := os.RemoveAll(item.Original); err != nil && !os.IsNotExist(err) {
				failures = append(failures, fmt.Errorf("remove new %s: %w", item.Original, err))
			}
			continue
		}
		if err := os.RemoveAll(item.Original); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Errorf("clear %s: %w", item.Original, err))
			continue
		}
		if err := copyTree(item.Backup, item.Original, 0); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", item.Original, err))
		}
	}
	if err := reloadJournalServices(ctx, request, journal.Services); err != nil {
		failures = append(failures, err)
	}
	for _, service := range journal.Services {
		// 安装前未启用的服务更需要走 restore：linuxAutostartRestore 会 disable/del，
		// 才能撤回本次安装加上的 enable / rc-update add。
		if err := restoreJournalService(ctx, request, service); err != nil {
			failures = append(failures, fmt.Errorf("restore runtime %s: %w", service.Name, err))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

func (journal *rollbackJournal) persist() error {
	if journal.Dir == "" {
		return nil
	}
	if err := os.MkdirAll(journal.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(journal.Dir, "journal.json"), data, 0o600)
}
