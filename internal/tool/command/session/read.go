package session

import (
	"fmt"
	"unicode/utf8"
)

type ReadSnapshot struct {
	Snapshot
	StdoutOffset, StderrOffset           int
	StdoutNextOffset, StderrNextOffset   int
	StdoutMissedBytes, StderrMissedBytes int
}

// Read uses client-owned absolute offsets and leaves legacy observation cursors alone.
func (s *Session) Read(stdoutOffset, stderrOffset, maxBytes int) (ReadSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxBytes < utf8.UTFMax {
		return ReadSnapshot{}, fmt.Errorf("max_output_bytes must be at least %d for read", utf8.UTFMax)
	}
	if stdoutOffset < 0 || stdoutOffset > s.stdoutTotalBytes || stderrOffset < 0 || stderrOffset > s.stderrTotalBytes {
		return ReadSnapshot{}, fmt.Errorf("output offsets must be between zero and the stream's total bytes")
	}
	status := "running"
	if s.completed {
		status = "exited"
		if s.TimedOut {
			status = "timeout"
		}
	}
	result := ReadSnapshot{Snapshot: s.snapshotLocked(status, maxBytes, false)}
	result.Stdout, result.StdoutOffset, result.StdoutNextOffset, result.StdoutMissedBytes = readPage(s.stdout.Bytes(), s.stdoutDroppedBytes, stdoutOffset, maxBytes, s.completed)
	result.Stderr, result.StderrOffset, result.StderrNextOffset, result.StderrMissedBytes = readPage(s.stderr.Bytes(), s.stderrDroppedBytes, stderrOffset, maxBytes, s.completed)
	result.StdoutOutputBytes, result.StderrOutputBytes = len(result.Stdout), len(result.Stderr)
	result.StdoutOutputLines, result.StderrOutputLines = countLines(result.Stdout), countLines(result.Stderr)
	result.StdoutOmittedBytes = s.stdoutTotalBytes - result.StdoutNextOffset
	result.StderrOmittedBytes = s.stderrTotalBytes - result.StderrNextOffset
	result.StdoutTruncated, result.StderrTruncated = result.StdoutOmittedBytes > 0, result.StderrOmittedBytes > 0
	return result, nil
}

func readPage(data []byte, dropped, requested, maxBytes int, completed bool) (string, int, int, int) {
	start := max(requested, dropped) - dropped
	// Eviction or a caller-supplied offset can land inside a UTF-8 character.
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}
	end := min(start+maxBytes, len(data))
	for end > start && end < len(data) && !utf8.RuneStart(data[end]) {
		end--
	}
	if !completed && end == len(data) && end > start {
		last := end - 1
		for last > start && !utf8.RuneStart(data[last]) {
			last--
		}
		if !utf8.FullRune(data[last:end]) {
			end = last
		}
	}
	return string(data[start:end]), dropped + start, dropped + end, dropped + start - requested
}
