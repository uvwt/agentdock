//go:build linux

package wslfilehelper

// Request 是 Windows Host 通过 stdin 发送给 Linux helper 的内部协议。
// 这是内部实现协议，不直接暴露给 MCP 调用方。
type Request struct {
	Action string `json:"action"`

	Path    string `json:"path"`
	NewPath string `json:"new_path"`
	Workdir string `json:"workdir"`

	RejectSymlink bool `json:"reject_symlink"`
	AllowMissing  bool `json:"allow_missing"`

	Content   *string `json:"content"`
	Overwrite bool    `json:"overwrite"`
	MustExist bool    `json:"must_exist"`
	Mode      *int    `json:"mode"`
	OwnerUID  *int    `json:"owner_uid"`
	OwnerGID  *int    `json:"owner_gid"`

	MaxDepth        int      `json:"max_depth"`
	MaxEntries      int      `json:"max_entries"`
	Patterns        []string `json:"patterns"`
	ExcludePatterns []string `json:"exclude_patterns"`
	EntryType       string   `json:"entry_type"`
	IncludeHidden   bool     `json:"include_hidden"`
	IncludeIgnored  bool     `json:"include_ignored"`

	Query         string   `json:"query"`
	Regex         bool     `json:"regex"`
	CaseSensitive bool     `json:"case_sensitive"`
	IncludeGlobs  []string `json:"include_globs"`
	ExcludeGlobs  []string `json:"exclude_globs"`
	ContextLines  int      `json:"context_lines"`
	MaxResults    int      `json:"max_results"`

	Changes []ChangeRequest `json:"changes"`
}

type ChangeRequest struct {
	Path           string  `json:"path"`
	ExpectedExists *bool   `json:"expected_exists"`
	NewExists      *bool   `json:"new_exists"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	ExpectedMode   *int    `json:"expected_mode"`
	ExpectedUID    *int    `json:"expected_uid"`
	ExpectedGID    *int    `json:"expected_gid"`
	Content        *string `json:"content"`
	SHA256         string  `json:"sha256"`
	Mode           *int    `json:"mode"`
	OwnerUID       *int    `json:"owner_uid"`
	OwnerGID       *int    `json:"owner_gid"`
}

type Response struct {
	OK      bool           `json:"ok"`
	Code    string         `json:"code,omitempty"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`

	Exists    *bool  `json:"exists,omitempty"`
	Path      string `json:"path,omitempty"`
	NewPath   string `json:"new_path,omitempty"`
	Content   string `json:"content,omitempty"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
	Mode      *int   `json:"mode,omitempty"`
	Modified  string `json:"modified,omitempty"`
	UID       *int   `json:"uid,omitempty"`
	GID       *int   `json:"gid,omitempty"`
	Symlink   *bool  `json:"symlink,omitempty"`

	Entries      *[]DirEntry `json:"entries,omitempty"`
	Truncated    *bool       `json:"truncated,omitempty"`
	Partial      *bool       `json:"partial,omitempty"`
	SkippedPaths *[]string   `json:"skipped_paths,omitempty"`

	Matches      *[]SearchMatch `json:"matches,omitempty"`
	TotalMatches *int           `json:"total_matches,omitempty"`
	Engine       string         `json:"engine,omitempty"`
	Query        string         `json:"query,omitempty"`

	SHA256 string `json:"sha256,omitempty"`

	TransactionID         string `json:"transaction_id,omitempty"`
	FilesChanged          *int   `json:"files_changed,omitempty"`
	RecoveredTransactions *int   `json:"recovered_transactions,omitempty"`
	CleanupPending        *bool  `json:"cleanup_pending,omitempty"`
}

type DirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
	Modified  string `json:"modified"`
	IsHidden  bool   `json:"is_hidden"`
}

type SearchMatch struct {
	Path             string   `json:"path"`
	RelativePath     string   `json:"relative_path"`
	Line             int      `json:"line"`
	Column           int      `json:"column"`
	Preview          string   `json:"preview"`
	MatchText        string   `json:"match_text"`
	Before           []string `json:"before"`
	After            []string `json:"after"`
	ContextStartLine int      `json:"context_start_line"`
	ContextEndLine   int      `json:"context_end_line"`
}

func boolPtr(value bool) *bool    { return &value }
func intPtr(value int) *int       { return &value }
func int64Ptr(value int64) *int64 { return &value }
