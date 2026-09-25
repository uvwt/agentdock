package main

import (
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

func TestArbiterRunTimeoutPreservesWindowsTrialHeadroom(t *testing.T) {
	// 保守按各阶段有界等待相加：Tunnel 停止 30s、Core 停止 20s、Tray 停止 15s、
	// Core 启动健康预算 60s、版本确认同一预算 60s、Tray 存活确认 15s。
	// 成功路径不会把两段健康预算都耗满，但总事务预算仍应覆盖这个悲观上界。
	const windowsTrialWorstCase = 30*time.Second +
		20*time.Second +
		15*time.Second +
		2*desktopruntime.WindowsCoreStartTimeout +
		15*time.Second
	if arbiterRunTimeout <= windowsTrialWorstCase {
		t.Fatalf("arbiterRunTimeout = %s, must exceed Windows trial worst case %s", arbiterRunTimeout, windowsTrialWorstCase)
	}
}
