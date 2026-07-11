//go:build !windows

package skillruntime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRunKeepsPrimaryResultWhenReporterAndEventSinkFail(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	reporter := &failingReporter{err: errors.New("report unavailable")}
	sink := &failingEventSink{err: errors.New("event unavailable")}
	runtime := newTestRuntime(t, "#!/bin/sh\nprintf '{\"ok\":true}\\n'\n")
	runtime.Reporter = reporter
	runtime.Events = sink

	result, err := runtime.Run(context.Background(), RunRequest{Skill: "output-test", Operation: "run", Input: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("Run() result = %#v", result)
	}
	if reporter.runCalls != 1 || sink.calls == 0 {
		t.Fatalf("reporter calls=%d event calls=%d", reporter.runCalls, sink.calls)
	}
	for _, expected := range []string{"skill run report failed", "skill runtime event delivery failed", "report unavailable", "event unavailable"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("log missing %q: %s", expected, logs.String())
		}
	}
}

func TestFinishInstallKeepsPrimaryResultWhenObserversFail(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	reporter := &failingReporter{err: errors.New("report unavailable")}
	sink := &failingEventSink{err: errors.New("event unavailable")}
	runtime := &Runtime{Reporter: reporter, Events: sink}
	manifest := Manifest{Metadata: Metadata{Name: "demo-skill", Version: "1.0.0"}}
	result, err := runtime.finishInstall(context.Background(), InstallRequest{}, manifest, "/tmp/demo-skill", "sha256:digest")
	if err != nil {
		t.Fatalf("finishInstall() error = %v", err)
	}
	if result.Skill != "demo-skill" || reporter.installCalls != 1 || sink.calls != 1 {
		t.Fatalf("result=%#v reporter calls=%d event calls=%d", result, reporter.installCalls, sink.calls)
	}
	for _, expected := range []string{"skill installation report failed", "skill runtime event delivery failed"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("log missing %q: %s", expected, logs.String())
		}
	}
}

type failingReporter struct {
	err          error
	installCalls int
	runCalls     int
}

func (r *failingReporter) InstallationChanged(context.Context, InstallResult) error {
	r.installCalls++
	return r.err
}

func (r *failingReporter) RunCompleted(context.Context, RunResult) error {
	r.runCalls++
	return r.err
}

type failingEventSink struct {
	err   error
	calls int
}

func (s *failingEventSink) Emit(context.Context, Event) error {
	s.calls++
	return s.err
}
