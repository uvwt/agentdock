package main

import (
	"testing"
	"time"
)

func TestArbiterRunTimeoutPreservesWindowsTrialHeadroom(t *testing.T) {
	// Windows trial 各阶段的有界等待上限约 185s；Arbiter 总预算必须高于该值，
	// 否则慢停止 + 60s Core 冷启动可能在验证阶段被总 context 提前取消。
	const windowsTrialWorstCase = 185 * time.Second
	if arbiterRunTimeout <= windowsTrialWorstCase {
		t.Fatalf("arbiterRunTimeout = %s, must exceed Windows trial worst case %s", arbiterRunTimeout, windowsTrialWorstCase)
	}
}
