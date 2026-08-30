package file

// RuntimeOptions 描述文件工具的实际文件系统运行环境。非 Windows 主机只接受零值。
type RuntimeOptions struct {
	Runtime         string `json:"runtime,omitempty"`
	WSLDistribution string `json:"wsl_distribution,omitempty"`
}

// ReadRequest 是 read_file 进入文件核心后的稳定输入契约。
type ReadRequest struct {
	RuntimeOptions
	Path      string `json:"path"`
	StartLine *int   `json:"start_line,omitempty"`
	EndLine   *int   `json:"end_line,omitempty"`
	MaxBytes  *int   `json:"max_bytes,omitempty"`
}

// ListRequest 是 list_dir 进入文件核心后的稳定输入契约。
type ListRequest struct {
	RuntimeOptions
	Path            string   `json:"path,omitempty"`
	MaxDepth        *int     `json:"max_depth,omitempty"`
	MaxEntries      *int     `json:"max_entries,omitempty"`
	Patterns        []string `json:"patterns,omitempty"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	EntryType       string   `json:"entry_type,omitempty"`
	IncludeHidden   bool     `json:"include_hidden,omitempty"`
	IncludeIgnored  bool     `json:"include_ignored,omitempty"`
}

// SearchRequest 是 search_text 进入文件核心后的稳定输入契约。
type SearchRequest struct {
	RuntimeOptions
	Path           string   `json:"path,omitempty"`
	Query          string   `json:"query"`
	Regex          bool     `json:"regex,omitempty"`
	CaseSensitive  bool     `json:"case_sensitive,omitempty"`
	IncludeHidden  bool     `json:"include_hidden,omitempty"`
	IncludeIgnored bool     `json:"include_ignored,omitempty"`
	IncludeGlobs   []string `json:"include_globs,omitempty"`
	Glob           string   `json:"glob,omitempty"`
	ExcludeGlobs   []string `json:"exclude_globs,omitempty"`
	ContextLines   *int     `json:"context_lines,omitempty"`
	MaxResults     *int     `json:"max_results,omitempty"`
}

// EditRequest 统一承载 file_edit 五种 action 的输入，避免为每个 action 再制造一层类型。
// action 对字段组合的业务约束仍由文件服务在对应主流程中校验。
type EditRequest struct {
	RuntimeOptions
	Action          string `json:"action"`
	Path            string `json:"path,omitempty"`
	Old             string `json:"old,omitempty"`
	New             string `json:"new,omitempty"`
	ReplaceAll      bool   `json:"replace_all,omitempty"`
	ExpectedMatches *int   `json:"expected_matches,omitempty"`
	Content         string `json:"content,omitempty"`
	NewPath         string `json:"new_path,omitempty"`
	Overwrite       bool   `json:"overwrite,omitempty"`
	Recursive       bool   `json:"recursive,omitempty"`
	Patch           string `json:"patch,omitempty"`
	Workdir         string `json:"workdir,omitempty"`
	DryRun          bool   `json:"dry_run,omitempty"`
	MaxDiffBytes    *int   `json:"max_diff_bytes,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
