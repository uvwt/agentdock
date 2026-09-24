package startupdiag

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogWritesStructuredStartupStage(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	Log(logger, "core", "runtime_init", time.Now().Add(-10*time.Millisecond), slog.String("result", "ok"))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("parse startup log: %v", err)
	}
	if entry["msg"] != "startup stage" || entry["component"] != "core" || entry["stage"] != "runtime_init" || entry["result"] != "ok" {
		t.Fatalf("unexpected startup entry: %#v", entry)
	}
	if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("duration_ms = %#v, want non-negative number", entry["duration_ms"])
	}
	if pid, ok := entry["pid"].(float64); !ok || int(pid) != os.Getpid() {
		t.Fatalf("pid = %#v, want %d", entry["pid"], os.Getpid())
	}
}

func TestAppendWritesBoundedStartupLog(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, "task_core_host", "core_spawn", time.Now(), slog.Int("core_pid", 1234)); err != nil {
		t.Fatalf("append startup log: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "logs", "startup.log"))
	if err != nil {
		t.Fatalf("read startup log: %v", err)
	}
	if !bytes.Contains(data, []byte(`"component":"task_core_host"`)) ||
		!bytes.Contains(data, []byte(`"stage":"core_spawn"`)) ||
		!bytes.Contains(data, []byte(`"core_pid":1234`)) {
		t.Fatalf("unexpected startup log: %s", data)
	}
}
