package taskstate

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

func (s *Store) Get(id string) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()
	return s.loadLocked(id)
}

func (s *Store) Delete(id string) (Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return Task{}, err
	}
	defer release()

	task, err := s.loadLocked(id)
	if err != nil {
		return Task{}, err
	}
	if err := os.Remove(filepath.Join(s.root, id+".json")); err != nil {
		return Task{}, fmt.Errorf("delete task %s: %w", id, err)
	}
	return task, nil
}

func (s *Store) List(status Status, limit int) ([]Task, error) {
	release, err := s.acquireStoreLock()
	if err != nil {
		return nil, err
	}
	defer release()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "tsk_") || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := readTaskStateFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			slog.Warn("skip unreadable task state", "file", entry.Name(), "error", err)
			continue
		}
		task, err := decodeTask(data, entry.Name())
		if err != nil {
			slog.Warn("skip invalid task state", "file", entry.Name(), "error", err)
			continue
		}
		if status == "" || task.Status == status {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks, nil
}

func (s *Store) loadLocked(id string) (Task, error) {
	if err := validateID(id); err != nil {
		return Task{}, err
	}
	data, err := readTaskStateFile(filepath.Join(s.root, id+".json"))
	if os.IsNotExist(err) {
		return Task{}, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	if err != nil {
		return Task{}, err
	}
	task, err := decodeTask(data, id)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) saveLocked(task Task) error {
	if err := validateID(task.ID); err != nil {
		return err
	}
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxTaskStateFileBytes {
		return fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	target := filepath.Join(s.root, task.ID+".json")
	return atomicfile.Write(target, data, 0o600)
}

func readTaskStateFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxTaskStateFileBytes {
		return nil, fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTaskStateFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTaskStateFileBytes {
		return nil, fmt.Errorf("task state exceeds %d bytes", maxTaskStateFileBytes)
	}
	return data, nil
}

func decodeTask(data []byte, label string) (Task, error) {
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, fmt.Errorf("decode task %s: %w", label, err)
	}
	if task.SchemaVersion != SchemaVersion {
		return Task{}, fmt.Errorf("unsupported task schema version %d", task.SchemaVersion)
	}
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
	return task, nil
}
