package plugin

type ManageRequest struct {
	Action      string `json:"action"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	ReviewToken string `json:"review_token,omitempty"`
	DataPolicy  string `json:"data_policy,omitempty"`
}
