package selfupdate

import (
	"encoding/json"
	"io"
)

const updateProgressSchemaVersion = 1

type UpdateProgressStage string

const (
	UpdateStageChecking      UpdateProgressStage = "checking"
	UpdateStageDownloading   UpdateProgressStage = "downloading"
	UpdateStageVerifying     UpdateProgressStage = "verifying"
	UpdateStageExtracting    UpdateProgressStage = "extracting"
	UpdateStageInstalling    UpdateProgressStage = "installing"
	UpdateStageUpdatingSkill UpdateProgressStage = "updating_skills"
	UpdateStageRestarting    UpdateProgressStage = "restarting"
)

type UpdateProgressEvent struct {
	SchemaVersion  int                 `json:"schema_version"`
	Type           string              `json:"type"`
	Stage          UpdateProgressStage `json:"stage,omitempty"`
	CurrentVersion string              `json:"current_version,omitempty"`
	TargetVersion  string              `json:"target_version,omitempty"`
	Asset          string              `json:"asset,omitempty"`
	Bytes          int64               `json:"bytes,omitempty"`
	TotalBytes     int64               `json:"total_bytes,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type updateProgressReporter func(UpdateProgressEvent)

func newJSONProgressReporter(output io.Writer) updateProgressReporter {
	encoder := json.NewEncoder(output)
	return func(event UpdateProgressEvent) {
		event.SchemaVersion = updateProgressSchemaVersion
		// 进度通道只是 UI/客户端观察面，写入失败不能让正在进行的更新事务中断。
		_ = encoder.Encode(event)
	}
}

func reportUpdateStage(
	reporter updateProgressReporter,
	stage UpdateProgressStage,
	currentVersion string,
	targetVersion string,
	asset string,
) {
	if reporter == nil {
		return
	}
	reporter(UpdateProgressEvent{
		Type:           "stage",
		Stage:          stage,
		CurrentVersion: normalizeVersion(currentVersion),
		TargetVersion:  normalizeVersion(targetVersion),
		Asset:          asset,
	})
}

func reportDownloadProgress(
	reporter updateProgressReporter,
	currentVersion string,
	targetVersion string,
	asset string,
	bytesRead int64,
	totalBytes int64,
) {
	if reporter == nil {
		return
	}
	reporter(UpdateProgressEvent{
		Type:           "progress",
		Stage:          UpdateStageDownloading,
		CurrentVersion: normalizeVersion(currentVersion),
		TargetVersion:  normalizeVersion(targetVersion),
		Asset:          asset,
		Bytes:          bytesRead,
		TotalBytes:     totalBytes,
	})
}
