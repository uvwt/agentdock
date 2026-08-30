package command

// RuntimeOptions 描述命令实际执行环境。非 Windows 主机只接受零值。
type RuntimeOptions struct {
	Runtime         string `json:"runtime,omitempty"`
	WSLDistribution string `json:"wsl_distribution,omitempty"`
}

// ExecRequest 是 exec_command 进入命令核心后的稳定输入契约。
// 可选整数使用指针保留“未提供”和“显式提供 0”的区别，例如 yield_time_ms=0。
type ExecRequest struct {
	RuntimeOptions
	Cmd            string            `json:"cmd"`
	Workdir        string            `json:"workdir,omitempty"`
	Skill          string            `json:"skill,omitempty"`
	SkillEnv       string            `json:"skill_env,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMS      *int              `json:"timeout_ms,omitempty"`
	ExecutionMode  string            `json:"execution_mode,omitempty"`
	YieldTimeMS    *int              `json:"yield_time_ms,omitempty"`
	MaxOutputBytes *int              `json:"max_output_bytes,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	TTY            bool              `json:"tty,omitempty"`
}

// SessionObserveRequest 是 session_observe 的强类型输入。
type SessionObserveRequest struct {
	Action         string `json:"action,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	MaxOutputBytes *int   `json:"max_output_bytes,omitempty"`
}

// SessionActRequest 是 session_act 的强类型输入。
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
