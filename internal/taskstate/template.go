package taskstate

import "time"

type TemplateStatus string

const (
	TemplateDraft   TemplateStatus = "draft"
	TemplateActive  TemplateStatus = "active"
	TemplateRetired TemplateStatus = "retired"
)

type MatchRule struct {
	Keywords []string `json:"keywords,omitempty"`
	Devices  []string `json:"devices,omitempty"`
	Type     string   `json:"type,omitempty"`
}

type TemplateStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Phase Phase  `json:"phase"`
}

type Template struct {
	ID                   string         `json:"id"`
	Version              string         `json:"version"`
	Title                string         `json:"title"`
	Description          string         `json:"description,omitempty"`
	Status               TemplateStatus `json:"status"`
	Match                MatchRule      `json:"match,omitempty"`
	CompletionConditions []string       `json:"completion_conditions"`
	Steps                []TemplateStep `json:"steps"`
	AllowLongTemplate    bool           `json:"allow_long_template,omitempty"`
	LongTemplateReason   string         `json:"long_template_reason,omitempty"`
	Hash                 string         `json:"hash,omitempty"`
	PublishedAt          *time.Time     `json:"published_at,omitempty"`
	RetiredAt            *time.Time     `json:"retired_at,omitempty"`
}

type TemplateCandidate struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Score   int    `json:"score"`
	Reason  string `json:"reason"`
}

type TemplateSelection struct {
	ID             string              `json:"id"`
	Version        string              `json:"version"`
	Hash           string              `json:"hash"`
	SelectedReason string              `json:"selected_reason"`
	Candidates     []TemplateCandidate `json:"candidates,omitempty"`
}

type TaskStep struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Phase     Phase     `json:"phase"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}
