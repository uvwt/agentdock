package logx

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestSetupConfiguresExpectedMinimumLevel(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	ctx := context.Background()
	tests := []struct {
		name         string
		configured   string
		disabled     slog.Level
		minimumLevel slog.Level
	}{
		{name: "debug", configured: " DEBUG ", disabled: slog.Level(-8), minimumLevel: slog.LevelDebug},
		{name: "info default", configured: "unknown", disabled: slog.LevelDebug, minimumLevel: slog.LevelInfo},
		{name: "warn alias", configured: "warning", disabled: slog.LevelInfo, minimumLevel: slog.LevelWarn},
		{name: "error", configured: "error", disabled: slog.LevelWarn, minimumLevel: slog.LevelError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Setup(test.configured, &bytes.Buffer{})
			logger := slog.Default()
			if logger.Enabled(ctx, test.disabled) {
				t.Fatalf("level %s unexpectedly enabled for %q", test.disabled, test.configured)
			}
			if !logger.Enabled(ctx, test.minimumLevel) {
				t.Fatalf("minimum level %s disabled for %q", test.minimumLevel, test.configured)
			}
		})
	}
}

func TestSetupWritesJSONToProvidedOutput(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)

	var output bytes.Buffer
	Setup("info", &output)
	slog.Info("rotation-test", "component", "core")
	if !bytes.Contains(output.Bytes(), []byte(`"msg":"rotation-test"`)) ||
		!bytes.Contains(output.Bytes(), []byte(`"component":"core"`)) {
		t.Fatalf("unexpected log output: %s", output.String())
	}
}
