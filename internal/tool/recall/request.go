package recall

// SearchRequest 是 recall_search 进入 Recall capability 后的稳定输入契约。
type SearchRequest struct {
	Query      string `json:"query"`
	Kind       string `json:"kind,omitempty"`
	MaxResults *int   `json:"max_results,omitempty"`
}

// ReadRequest 是 recall_read 的强类型输入。
type ReadRequest struct {
	Path       string `json:"path"`
	IncludeRaw bool   `json:"include_raw,omitempty"`
}

// WriteRequest 统一承载 recall_write 的公开字段及内部安全预览字段。
// Facts 是共享协议明确声明为开放对象的唯一动态叶子；它在 update_fact 核心入口一次性规范化为字符串值。
type WriteRequest struct {
	Target          string                 `json:"target,omitempty"`
	Action          string                 `json:"action,omitempty"`
	Confirmed       *bool                  `json:"confirmed,omitempty"`
	DryRun          *bool                  `json:"dry_run,omitempty"`
	Path            string                 `json:"path,omitempty"`
	Content         string                 `json:"content,omitempty"`
	Title           string                 `json:"title,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	Overwrite       *bool                  `json:"overwrite,omitempty"`
	AllowWarnings   bool                   `json:"allow_warnings,omitempty"`
	Old             string                 `json:"old,omitempty"`
	New             string                 `json:"new,omitempty"`
	Append          string                 `json:"append,omitempty"`
	Prepend         string                 `json:"prepend,omitempty"`
	Pattern         string                 `json:"pattern,omitempty"`
	Replacement     string                 `json:"replacement,omitempty"`
	All             *bool                  `json:"all,omitempty"`
	Operations      []memoryPatchOperation `json:"operations,omitempty"`
	Section         string                 `json:"section,omitempty"`
	SectionContent  string                 `json:"section_content,omitempty"`
	Key             string                 `json:"key,omitempty"`
	Value           *string                `json:"value,omitempty"`
	Facts           map[string]any         `json:"facts,omitempty"`
	AppendIfMissing bool                   `json:"append_if_missing,omitempty"`
	MaxBytes        *int                   `json:"max_bytes,omitempty"`
	Type            string                 `json:"type,omitempty"`
	Scope           string                 `json:"scope,omitempty"`
	Project         string                 `json:"project,omitempty"`
	Source          string                 `json:"source,omitempty"`
	Confidence      string                 `json:"confidence,omitempty"`
	Evidence        string                 `json:"evidence,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	Boundary        string                 `json:"boundary,omitempty"`
	Status          string                 `json:"status,omitempty"`
	MaxResults      *int                   `json:"max_results,omitempty"`
}

// MaintainRequest 是 recall_maintain 的强类型输入。
type MaintainRequest struct {
	Action      string   `json:"action,omitempty"`
	Prefix      string   `json:"prefix,omitempty"`
	Terms       []string `json:"terms,omitempty"`
	Regex       *bool    `json:"regex,omitempty"`
	MaxEntries  *int     `json:"max_entries,omitempty"`
	MaxFindings *int     `json:"max_findings,omitempty"`
	MaxResults  *int     `json:"max_results,omitempty"`
}

// PrivateNoteRequest 是 private_note_manage 的强类型输入。
type PrivateNoteRequest struct {
	Action            string   `json:"action"`
	Query             string   `json:"query,omitempty"`
	MaxResults        *int     `json:"max_results,omitempty"`
	Path              string   `json:"path,omitempty"`
	Category          string   `json:"category,omitempty"`
	Title             string   `json:"title,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Content           string   `json:"content,omitempty"`
	Confirmed         *bool    `json:"confirmed,omitempty"`
	Overwrite         *bool    `json:"overwrite,omitempty"`
	MaxBytes          *int     `json:"max_bytes,omitempty"`
	StatusAction      string   `json:"status_action,omitempty"`
	MaintenanceAction string   `json:"maintenance_action,omitempty"`
}

// memorySearchRequest / memoryListRequest 是 Recall HTTP adapter 前的内部强类型参数。
type memorySearchRequest struct {
	Query         string
	Prefix        string
	ExcludePrefix string
	MaxResults    int
}

type memoryListRequest struct {
	Prefix     string
	MaxEntries int
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func boolPtrValue(value bool) *bool { return &value }
