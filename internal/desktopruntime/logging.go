package desktopruntime

import (
	"io"

	"github.com/uvwt/agentdock/internal/logfile"
)

type processLogs struct {
	stdout io.WriteCloser
	stderr io.WriteCloser
}

func OpenCoreLog(runtimeRoot string) (io.WriteCloser, error) {
	return platformOpenCoreLog(runtimeRoot)
}

func openProcessLogs(stdoutPath, stderrPath string) (*processLogs, error) {
	stdout, err := logfile.OpenDefault(stdoutPath)
	if err != nil {
		return nil, err
	}
	stderr, err := logfile.OpenDefault(stderrPath)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	return &processLogs{stdout: stdout, stderr: stderr}, nil
}

func (logs *processLogs) Close() error {
	if logs == nil {
		return nil
	}
	stdoutErr := logs.stdout.Close()
	stderrErr := logs.stderr.Close()
	if stdoutErr != nil {
		return stdoutErr
	}
	return stderrErr
}
