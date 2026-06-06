package nexusclient

import (
	"encoding/json"
	"runtime"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
)

type HeartbeatProvider interface {
	Heartbeat(deviceID string) contracts.DeviceHeartbeat
}

type DeviceMetrics struct {
	CPUCount          int    `json:"cpu_count"`
	ProcessAllocBytes uint64 `json:"process_alloc_bytes"`
	Goroutines        int    `json:"goroutines"`
}

type SystemHeartbeatProvider struct {
	StartedAt     time.Time
	Version       string
	Capabilities  func() []contracts.DeviceCapability
	SkillSummary  func() any
	MemorySummary func() any
	Now           func() time.Time
}

func (p SystemHeartbeatProvider) Heartbeat(deviceID string) contracts.DeviceHeartbeat {
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	startedAt := p.StartedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	var memoryStats runtime.MemStats
	runtime.ReadMemStats(&memoryStats)
	metrics, _ := json.Marshal(DeviceMetrics{
		CPUCount:          runtime.NumCPU(),
		ProcessAllocBytes: memoryStats.Alloc,
		Goroutines:        runtime.NumGoroutine(),
	})
	heartbeat := contracts.DeviceHeartbeat{
		DeviceId:         deviceID,
		SentAt:           now.Format(time.RFC3339Nano),
		UptimeSeconds:    int64(now.Sub(startedAt).Seconds()),
		AgentdockVersion: p.Version,
		Metrics:          metrics,
		Capabilities:     []contracts.DeviceCapability{},
	}
	if p.Capabilities != nil {
		heartbeat.Capabilities = p.Capabilities()
	}
	if p.SkillSummary != nil {
		heartbeat.SkillSummary, _ = json.Marshal(p.SkillSummary())
	}
	if p.MemorySummary != nil {
		heartbeat.MemorySyncSummary, _ = json.Marshal(p.MemorySummary())
	}
	return heartbeat
}
