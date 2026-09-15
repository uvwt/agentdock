package plugin

// Definition groups related document Skills and dynamic MCP servers behind one
// lightweight capability description. Member configuration remains owned by the
// existing Skill and MCP stores; this registry only records ownership and the
// plugin-level availability overlay.
type Definition struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Skills      []string `json:"skills,omitempty"`
	MCPServers  []string `json:"mcp_servers,omitempty"`
}

type Membership struct {
	Plugin  string `json:"plugin"`
	Enabled bool   `json:"enabled"`
}

type Error struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func newError(code, message string, details map[string]any, cause error) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: code, Message: message, Details: details, Cause: cause}
}
