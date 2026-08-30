package acp

// SessionRequest 是 acp_session 进入 ACP capability 后的稳定输入契约。
// ConfigValue 只保留协议明确允许的 string/bool 动态叶子。
type SessionRequest struct {
	Action                string   `json:"action"`
	AuthMethodID          string   `json:"auth_method_id,omitempty"`
	SessionID             string   `json:"session_id,omitempty"`
	CWD                   string   `json:"cwd,omitempty"`
	AdditionalDirectories []string `json:"additional_directories,omitempty"`
	ModeID                string   `json:"mode_id,omitempty"`
	ConfigID              string   `json:"config_id,omitempty"`
	ConfigValue           any      `json:"config_value,omitempty"`
}

// PromptRequest 是 acp_prompt 的强类型输入。
type PromptRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Text      string `json:"text,omitempty"`
	AfterSeq  *int   `json:"after_seq,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
	WaitMS    *int   `json:"wait_ms,omitempty"`
}

// InteractionRequest 是 acp_interaction 的强类型输入。
type InteractionRequest struct {
	Action        string `json:"action"`
	SessionID     string `json:"session_id,omitempty"`
	InteractionID string `json:"interaction_id,omitempty"`
	OptionID      string `json:"option_id,omitempty"`
	PendingOnly   *bool  `json:"pending_only,omitempty"`
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
