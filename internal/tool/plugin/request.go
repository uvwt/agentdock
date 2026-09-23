package plugin

type ManageRequest struct {
	Action                string `json:"action"`
	Name                  string `json:"name,omitempty"`
	Source                string `json:"source,omitempty"`
	Enabled               *bool  `json:"enabled,omitempty"`
	Confirmed             bool   `json:"confirmed,omitempty"`
	ConfirmedSourceChange bool   `json:"confirmed_source_change,omitempty"`
	DataPolicy            string `json:"data_policy,omitempty"`
}
