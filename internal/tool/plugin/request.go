package plugin

type ManageRequest struct {
	Action      string   `json:"action"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	MCPServers  []string `json:"mcp_servers,omitempty"`
}

type LoadRequest struct {
	Name string `json:"name"`
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
