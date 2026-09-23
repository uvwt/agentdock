package skill

type SkillDocument struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	Body          string            `json:"body,omitempty"`
}

type SkillMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type InstallRequest struct {
	Source       string
	DigestSHA256 string
	MaxBytes     int64
	MaxFiles     int
}

type InstallResult struct {
	Skill         string `json:"skill"`
	ContentDigest string `json:"content_digest"`
	Path          string `json:"path"`
	Changed       bool   `json:"changed"`
}

type RemoveResult struct {
	Skill                string `json:"skill"`
	Removed              bool   `json:"removed"`
	PreservedEnvironment bool   `json:"preserved_environment"`
	PreservedData        bool   `json:"preserved_data"`
}

type ValidateRequest struct {
	Source       string
	DigestSHA256 string
	MaxBytes     int64
	MaxFiles     int
}

type ValidateIssue struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type ValidateResult struct {
	Valid         bool            `json:"valid"`
	Source        string          `json:"source"`
	SourceDigest  string          `json:"source_digest,omitempty"`
	ContentDigest string          `json:"content_digest,omitempty"`
	Document      SkillDocument   `json:"document,omitempty"`
	Issues        []ValidateIssue `json:"issues"`
}
