package skill

// ManageRequest is the stable skill_manage input contract.
type ManageRequest struct {
	Action   string  `json:"action"`
	Skill    string  `json:"skill,omitempty"`
	Key      string  `json:"key,omitempty"`
	Value    *string `json:"value,omitempty"`
	Source   string  `json:"source,omitempty"`
	Digest   string  `json:"digest,omitempty"`
	Purge    bool    `json:"purge,omitempty"`
	MaxBytes *int    `json:"max_bytes,omitempty"`
	MaxFiles *int    `json:"max_files,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
