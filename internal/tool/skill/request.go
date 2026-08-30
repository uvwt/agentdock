package skill

// PackageRequest 是 skill_package 进入 Skill capability 后的稳定输入契约。
type PackageRequest struct {
	Action   string  `json:"action"`
	Skill    string  `json:"skill,omitempty"`
	Version  string  `json:"version,omitempty"`
	Key      string  `json:"key,omitempty"`
	Value    *string `json:"value,omitempty"`
	Source   string  `json:"source,omitempty"`
	Digest   string  `json:"digest,omitempty"`
	Activate *bool   `json:"activate,omitempty"`
	MaxBytes *int    `json:"max_bytes,omitempty"`
}

type InspectRequest struct {
	Skill   string `json:"skill"`
	Version string `json:"version,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
