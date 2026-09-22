package command

// RuntimeOptions describes the actual command runtime. Non-Windows hosts only accept zero values.
type RuntimeOptions struct {
	Runtime         string `json:"runtime,omitempty"`
	WSLDistribution string `json:"wsl_distribution,omitempty"`
}

// ExecRequest is the stable exec_command input contract.
type ExecRequest struct {
	RuntimeOptions
	Cmd            string            `json:"cmd"`
	Workdir        string            `json:"workdir,omitempty"`
	SkillRef       string            `json:"skill_ref,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMS      *int              `json:"timeout_ms,omitempty"`
	ExecutionMode  string            `json:"execution_mode,omitempty"`
	YieldTimeMS    *int              `json:"yield_time_ms,omitempty"`
	MaxOutputBytes *int              `json:"max_output_bytes,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	TTY            bool              `json:"tty,omitempty"`
}

type SessionObserveRequest struct {
	Action         string `json:"action,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	MaxOutputBytes *int   `json:"max_output_bytes,omitempty"`
}

type SessionActRequest struct {
	Action         string `json:"action,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Chars          string `json:"chars,omitempty"`
	MaxOutputBytes *int   `json:"max_output_bytes,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
