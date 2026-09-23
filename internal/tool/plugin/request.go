package plugin

type ManageRequest struct {
	Action                string `json:"action"`
	Name                  string `json:"name,omitempty"`
	Source                string `json:"source,omitempty"`
	SourceType            string `json:"source_type,omitempty"`
	SourceAdapter         string `json:"source_adapter,omitempty"`
	SourceVersion         string `json:"source_version,omitempty"`
	GitRef                string `json:"git_ref,omitempty"`
	GitCommit             string `json:"git_commit,omitempty"`
	Subdir                string `json:"subdir,omitempty"`
	SHA256                string `json:"sha256,omitempty"`
	Catalog               string `json:"catalog,omitempty"`
	CatalogItem           string `json:"catalog_item,omitempty"`
	Enabled               *bool  `json:"enabled,omitempty"`
	ReviewToken           string `json:"review_token,omitempty"`
	ConfirmedSourceChange bool   `json:"confirmed_source_change,omitempty"`
	DataPolicy            string `json:"data_policy,omitempty"`
}
